package provider

import (
	"context"
	"crypto/ecdsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/jashwanth0712/meshy/meshy/enskit"
	"github.com/jashwanth0712/meshy/meshy/provider/netbird"
)

// --- EIP-712 admission attestation (matches contracts/src/ProviderRegistry.sol) ---

var (
	attestationTypeHash = crypto.Keccak256Hash([]byte("Attestation(uint256 providerId,bytes dnsName,uint256 resource,uint64 issuedAt,uint64 validUntil,bytes32 peerKey)"))
	domainTypeHash      = crypto.Keccak256Hash([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"))
)

// Attestation is the provider's signed statement "I admitted peerKey as
// dnsName while it was live". If the name was already dead (expired,
// unregistered, re-registered) at issuedAt, anyone can submit it to
// ProviderRegistry.slash and take half the provider's stake.
type Attestation struct {
	ProviderID uint64 `json:"providerId"`
	Name       string `json:"name"`
	DNSName    string `json:"dnsName"`  // 0x DNS wire encoding
	Resource   string `json:"resource"` // decimal getResource(labelhash) at issuance
	IssuedAt   uint64 `json:"issuedAt"`
	ValidUntil uint64 `json:"validUntil"`
	PeerKey    string `json:"peerKey"` // 0x bytes32 (WireGuard public key)
	Sig        string `json:"sig"`     // 0x 65-byte r||s||v, v in {27,28}
	Registry   string `json:"verifyingContract"`
	ChainID    uint64 `json:"chainId"`
}

func u256(v *big.Int) []byte { return common.LeftPadBytes(v.Bytes(), 32) }

// AttestationDigest returns the EIP-712 digest ProviderRegistry verifies.
func AttestationDigest(chainID uint64, registry common.Address, providerID uint64, dnsName []byte, resource *big.Int, issuedAt, validUntil uint64, peerKey [32]byte) common.Hash {
	domain := crypto.Keccak256Hash(
		domainTypeHash.Bytes(),
		crypto.Keccak256([]byte("ProviderRegistry")),
		crypto.Keccak256([]byte("1")),
		u256(new(big.Int).SetUint64(chainID)),
		common.LeftPadBytes(registry.Bytes(), 32),
	)
	structHash := crypto.Keccak256Hash(
		attestationTypeHash.Bytes(),
		u256(new(big.Int).SetUint64(providerID)),
		crypto.Keccak256(dnsName),
		u256(resource),
		u256(new(big.Int).SetUint64(issuedAt)),
		u256(new(big.Int).SetUint64(validUntil)),
		peerKey[:],
	)
	return crypto.Keccak256Hash([]byte("\x19\x01"), domain.Bytes(), structHash.Bytes())
}

// SignAttestation fills Sig on a.
func SignAttestation(a *Attestation, key *ecdsa.PrivateKey) error {
	dns := common.FromHex(a.DNSName)
	res, ok := new(big.Int).SetString(a.Resource, 10)
	if !ok {
		return errors.New("attestation: bad resource")
	}
	var pk [32]byte
	copy(pk[:], common.FromHex(a.PeerKey))
	d := AttestationDigest(a.ChainID, common.HexToAddress(a.Registry), a.ProviderID, dns, res, a.IssuedAt, a.ValidUntil, pk)
	sig, err := crypto.Sign(d.Bytes(), key)
	if err != nil {
		return err
	}
	sig[64] += 27
	a.Sig = "0x" + common.Bytes2Hex(sig)
	return nil
}

// --- on-chain reads ---

var (
	registryABI = mustABI(`[{"type":"function","name":"getResource","stateMutability":"view","inputs":[{"name":"anyId","type":"uint256"}],"outputs":[{"type":"uint256"}]},
{"type":"function","name":"getExpiry","stateMutability":"view","inputs":[{"name":"anyId","type":"uint256"}],"outputs":[{"type":"uint64"}]}]`)
	subscriptionABI = mustABI(`[{"type":"function","name":"seatsOf","stateMutability":"view","inputs":[{"name":"orgNode","type":"bytes32"}],"outputs":[{"name":"seats","type":"uint32"},{"name":"paidUntil","type":"uint64"},{"name":"providerId","type":"uint256"}]}]`)
)

func mustABI(s string) abi.ABI {
	a, err := abi.JSON(strings.NewReader(s))
	if err != nil {
		panic(err)
	}
	return a
}

func (s *Server) ethCall(ctx context.Context, to common.Address, a abi.ABI, method string, args ...any) ([]any, error) {
	data, err := a.Pack(method, args...)
	if err != nil {
		return nil, err
	}
	out, err := s.mapBuilder.ETH.CallContract(ctx, ethereum.CallMsg{To: &to, Data: data}, nil)
	if err != nil {
		return nil, err
	}
	return a.Unpack(method, out)
}

// SeatStatus is the paid-vs-used seat count for one org.
type SeatStatus struct {
	Org        string `json:"org"`
	Seats      uint32 `json:"seats"`
	Used       int    `json:"used"` // devices + agents on ENS
	PaidUntil  uint64 `json:"paidUntil"`
	ProviderID uint64 `json:"providerId"`
	Trial      bool   `json:"trial,omitempty"`
}

// TrialSeats is what a new MeshyOrgFactory org gets before it subscribes.
const TrialSeats = 3

// isFactoryOrg reports whether org was created by MeshyOrgFactory (it writes
// meshy.created-by on the org's own resolver, which the org owner controls).
func (s *Server) isFactoryOrg(ctx context.Context, org string) bool {
	rec, err := s.mapBuilder.ENS.Lookup(ctx, enskit.Name(org))
	if err != nil || rec.Resolver == (common.Address{}) {
		return false
	}
	v, err := rec.Text(ctx, s.mapBuilder.ENS, "meshy.created-by")
	return err == nil && v == "MeshyOrgFactory"
}

// orgCreatedTopic is keccak("OrgCreated(string,string,string,address,address,address,address,address)").
var orgCreatedTopic = crypto.Keccak256Hash([]byte("OrgCreated(string,string,string,address,address,address,address,address)"))

var orgCreatedArgs = func() abi.Arguments {
	str, _ := abi.NewType("string", "", nil)
	ad, _ := abi.NewType("address", "", nil)
	return abi.Arguments{{Type: str}, {Type: str}, {Type: ad}, {Type: ad}, {Type: ad}, {Type: ad}}
}()

// discoverOrgs returns every org name created by the factory (OrgCreated logs).
func (s *Server) discoverOrgs(ctx context.Context) ([]string, error) {
	if s.cfg.OrgFactory == (common.Address{}) {
		return nil, nil
	}
	logs, err := s.mapBuilder.ETH.FilterLogs(ctx, ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(s.cfg.OrgFactoryFrom),
		Addresses: []common.Address{s.cfg.OrgFactory},
		Topics:    [][]common.Hash{{orgCreatedTopic}},
	})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, l := range logs {
		v, err := orgCreatedArgs.Unpack(l.Data)
		if err == nil && len(v) >= 2 {
			out = append(out, v[1].(string))
		}
	}
	return out, nil
}

// orgsToReconcile is the configured orgs plus those discovered from the factory.
func (s *Server) orgsToReconcile(ctx context.Context) []string {
	seen := map[string]bool{}
	var out []string
	add := func(o string) {
		if o != "" && !seen[o] {
			seen[o] = true
			out = append(out, o)
		}
	}
	for _, o := range s.cfg.Orgs {
		add(o)
	}
	if found, err := s.discoverOrgs(ctx); err != nil {
		s.log.Warn("discover orgs", "err", err)
	} else {
		for _, o := range found {
			add(o)
		}
	}
	return out
}

func (s *Server) seats(ctx context.Context, org string) (SeatStatus, error) {
	st := SeatStatus{Org: org}
	res, err := s.ethCall(ctx, s.cfg.Subscription, subscriptionABI, "seatsOf", [32]byte(enskit.Namehash(org)))
	if err != nil {
		return st, fmt.Errorf("seatsOf: %w", err)
	}
	st.Seats = res[0].(uint32)
	st.PaidUntil = res[1].(uint64)
	st.ProviderID = res[2].(*big.Int).Uint64()
	// Orgs created with MeshyOrgFactory get a free trial until they subscribe.
	if st.Seats == 0 && s.isFactoryOrg(ctx, org) {
		st.Seats, st.Trial = TrialSeats, true
		st.PaidUntil = uint64(time.Now().Add(365 * 24 * time.Hour).Unix())
		st.ProviderID = s.cfg.ProviderID
	}
	m, err := s.mapBuilder.Build(ctx, org)
	if err != nil {
		return st, err
	}
	st.Used = len(m.Agents)
	for _, n := range m.Networks {
		st.Used += len(n.Resources)
	}
	return st, nil
}

// orgFromName returns the 2LD of a name: claude-1.agents.acme.eth -> acme.eth.
func orgFromName(name string) string { return string(enskit.Name(name).Org()) }

// --- attestation log (evidence for challengers) ---

type attestLog struct {
	mu   sync.Mutex
	path string
	all  []Attestation
}

func newAttestLog(dir string) *attestLog {
	l := &attestLog{path: filepath.Join(dir, "attestations.jsonl")}
	if raw, err := os.ReadFile(l.path); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			var a Attestation
			if json.Unmarshal([]byte(line), &a) == nil && a.Sig != "" {
				l.all = append(l.all, a)
			}
		}
	}
	return l
}

func (l *attestLog) add(a Attestation) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.all = append(l.all, a)
	if f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		b, _ := json.Marshal(a)
		f.Write(append(b, '\n'))
		f.Close()
	}
}

func (l *attestLog) list(name string) []Attestation {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []Attestation{}
	for i := len(l.all) - 1; i >= 0; i-- {
		if name == "" || l.all[i].Name == name || strings.HasSuffix(l.all[i].Name, "."+name) {
			out = append(out, l.all[i])
		}
		if len(out) >= 200 {
			break
		}
	}
	return out
}

// --- handlers ---

// AdmitRequest is the body of POST /v1/admit. Sig is EIP-191 over
// "meshy-admit:<name>:<wgPubKey>" by the name's identity key (its ENS addr
// record, i.e. the meshyd daemon key or the agent key) or by its ENS owner.
type AdmitRequest struct {
	Name     string `json:"name"`
	WGPubKey string `json:"wgPubKey"` // base64 WireGuard public key
	Sig      string `json:"sig"`
}

// AdmitResponse carries the one-off NetBird setup key and the signed attestation.
type AdmitResponse struct {
	SetupKey      string      `json:"setupKey"`
	ManagementURL string      `json:"managementUrl"`
	PeerName      string      `json:"peerName"`
	Group         string      `json:"group"`
	Attestation   Attestation `json:"attestation"`
	Seats         *SeatStatus `json:"seats,omitempty"`
}

func (s *Server) handleAdmit(w http.ResponseWriter, r *http.Request) {
	var req AdmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	req.Name = strings.ToLower(strings.TrimSpace(req.Name))
	if req.Name == "" || req.WGPubKey == "" || req.Sig == "" {
		jsonError(w, http.StatusBadRequest, "name, wgPubKey, sig are required")
		return
	}
	if s.nb == nil {
		jsonError(w, http.StatusServiceUnavailable, "NetBird not configured; set --netbird-api and --netbird-token")
		return
	}
	wg, err := base64.StdEncoding.DecodeString(req.WGPubKey)
	if err != nil || len(wg) != 32 {
		jsonError(w, http.StatusBadRequest, "wgPubKey must be a base64 32-byte WireGuard public key")
		return
	}
	signer, err := AdmitSigner(req.Name, req.WGPubKey, req.Sig)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "invalid signature: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// 1. The name must be live, and the signer must be its identity key or owner.
	name := enskit.Name(req.Name)
	parent, err := s.mapBuilder.ENS.ParentRegistry(ctx, name, nil)
	if err != nil || parent == (common.Address{}) {
		jsonError(w, http.StatusForbidden, "MESHY_NOT_ON_ENS "+req.Name+" is not registered (or an ancestor is gone)")
		return
	}
	labelID := enskit.LabelID(name.Label())
	expRes, err := s.ethCall(ctx, parent, registryABI, "getExpiry", labelID)
	if err != nil {
		jsonError(w, http.StatusBadGateway, "getExpiry: "+err.Error())
		return
	}
	now := uint64(time.Now().Unix())
	if exp := expRes[0].(uint64); exp <= now {
		jsonError(w, http.StatusForbidden, "MESHY_EXPIRED "+req.Name)
		return
	}
	resRes, err := s.ethCall(ctx, parent, registryABI, "getResource", labelID)
	if err != nil {
		jsonError(w, http.StatusBadGateway, "getResource: "+err.Error())
		return
	}
	resource := resRes[0].(*big.Int)

	allowed := map[common.Address]bool{}
	if owner, err := s.mapBuilder.ENS.ExactOwner(ctx, name, nil); err == nil && owner != (common.Address{}) {
		allowed[owner] = true
	}
	if rec, err := s.mapBuilder.ENS.Lookup(ctx, name); err == nil && rec.Resolver != (common.Address{}) {
		if a, err := s.mapBuilder.ENS.Addr(ctx, rec.Resolver, name); err == nil && a != (common.Address{}) {
			allowed[a] = true
		}
		if v, err := rec.Text(ctx, s.mapBuilder.ENS, "meshy.wg"); err == nil && v != "" && v != req.WGPubKey {
			jsonError(w, http.StatusForbidden, "MESHY_WG_MISMATCH meshy.wg on ENS does not match wgPubKey")
			return
		}
	}
	if !allowed[signer] {
		jsonError(w, http.StatusForbidden, "MESHY_NOT_AUTHORIZED signer "+signer.Hex()+" is neither the addr record nor the owner of "+req.Name)
		return
	}

	// 2. Kind: agent identity or device. Grants are not peers.
	org := orgFromName(req.Name)
	var group string
	switch {
	case strings.HasSuffix(req.Name, ".agents."+org):
		group = netbird.AgentGroup(req.Name)
	case name.Depth() == 2: // device.network.<org>
		group = netbird.ResGroup(req.Name)
	default:
		jsonError(w, http.StatusBadRequest, "only devices (<device>.<network>.<org>) and agents (<agent>.agents.<org>) join the mesh")
		return
	}

	// 3. Seats: devices + agents on ENS must fit in what the org paid this provider.
	var seat *SeatStatus
	if s.cfg.Subscription != (common.Address{}) {
		st, err := s.seats(ctx, org)
		if err != nil {
			jsonError(w, http.StatusBadGateway, "seat check: "+err.Error())
			return
		}
		seat = &st
		switch {
		case st.Seats == 0 || st.PaidUntil <= now:
			jsonError(w, http.StatusPaymentRequired, fmt.Sprintf("MESHY_NO_SUBSCRIPTION %s has no active subscription (paidUntil=%d)", org, st.PaidUntil))
			return
		case s.cfg.ProviderID != 0 && st.ProviderID != s.cfg.ProviderID:
			jsonError(w, http.StatusPaymentRequired, fmt.Sprintf("MESHY_WRONG_PROVIDER %s pays provider #%d, this is #%d", org, st.ProviderID, s.cfg.ProviderID))
			return
		case st.Used > int(st.Seats):
			jsonError(w, http.StatusPaymentRequired, fmt.Sprintf("MESHY_SEATS_EXCEEDED %s uses %d seats, paid for %d", org, st.Used, st.Seats))
			return
		}
	}

	// 4. NetBird: make sure the group exists, mint a one-off key into it.
	gid, err := s.ensureGroup(ctx, group)
	if err != nil {
		jsonError(w, http.StatusBadGateway, "netbird group: "+err.Error())
		return
	}
	key, err := s.nb.CreateSetupKey(ctx, netbird.CreateSetupKeyRequest{
		Name:       "meshy-admit:" + req.Name,
		Type:       "one-off",
		ExpiresIn:  3600,
		AutoGroups: []string{gid},
		UsageLimit: 1,
	})
	if err != nil {
		jsonError(w, http.StatusBadGateway, "setup key: "+err.Error())
		return
	}

	// 5. Sign the admission so it can be challenged on-chain.
	dns, _ := enskit.DNSEncode(req.Name)
	att := Attestation{
		ProviderID: s.cfg.ProviderID,
		Name:       req.Name,
		DNSName:    "0x" + common.Bytes2Hex(dns),
		Resource:   resource.String(),
		IssuedAt:   now,
		ValidUntil: now + 3600,
		PeerKey:    "0x" + common.Bytes2Hex(wg),
		Registry:   s.cfg.ProviderRegistry.Hex(),
		ChainID:    s.cfg.ChainID,
	}
	if err := SignAttestation(&att, s.signerKey); err != nil {
		jsonError(w, http.StatusInternalServerError, "sign attestation: "+err.Error())
		return
	}
	s.attest.add(att)
	s.log.Info("admitted", "name", req.Name, "signer", signer.Hex(), "group", group)

	mgmt := s.cfg.NetBirdMgmtURL
	if mgmt == "" {
		mgmt = s.cfg.NetBirdAPI
	}
	jsonOK(w, AdmitResponse{SetupKey: key.Key, ManagementURL: mgmt, PeerName: netbird.PeerName(req.Name), Group: group, Attestation: att, Seats: seat})
}

func (s *Server) ensureGroup(ctx context.Context, name string) (string, error) {
	groups, err := s.nb.ListGroups(ctx)
	if err != nil {
		return "", err
	}
	for _, g := range groups {
		if g.Name == name {
			return g.ID, nil
		}
	}
	g, err := s.nb.CreateGroup(ctx, netbird.GroupRequest{Name: name})
	return g.ID, err
}

func (s *Server) handleAttestations(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]any{"attestations": s.attest.list(r.URL.Query().Get("name"))})
}

func (s *Server) handleSeats(w http.ResponseWriter, r *http.Request) {
	org := r.URL.Query().Get("org")
	if org == "" || s.cfg.Subscription == (common.Address{}) {
		jsonError(w, http.StatusBadRequest, "org required and --subscription must be set")
		return
	}
	st, err := s.seats(r.Context(), org)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonOK(w, st)
}

// --- reconcile loop ---

// RunReconciler syncs every configured org into NetBird every interval.
func (s *Server) RunReconciler(ctx context.Context, every time.Duration) {
	if s.nb == nil || (len(s.cfg.Orgs) == 0 && s.cfg.OrgFactory == (common.Address{})) {
		return
	}
	for {
		dctx, dcancel := context.WithTimeout(ctx, 30*time.Second)
		orgs := s.orgsToReconcile(dctx)
		dcancel()
		for _, org := range orgs {
			cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
			m, err := s.mapBuilder.Build(cctx, org)
			if err == nil {
				err = s.rec.Sync(cctx, m.Snapshot())
			}
			cancel()
			if err != nil {
				s.log.Warn("reconcile", "org", org, "err", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}
