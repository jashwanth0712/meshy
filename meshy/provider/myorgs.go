package provider

import (
	"context"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/jashwanth0712/meshy/meshy/enskit"
)

// MyOrg is one org a wallet belongs to, with its strongest role.
type MyOrg struct {
	Org  string `json:"org"`
	Role string `json:"role"`           // owner | admin | member | agent
	Name string `json:"name,omitempty"` // the wallet's own name in the org, if any
}

var (
	rolesABI  = mustABI(`[{"type":"function","name":"hasRootRoles","stateMutability":"view","inputs":[{"name":"roleBitmap","type":"uint256"},{"name":"account","type":"address"}],"outputs":[{"type":"bool"}]}]`)
	orgsOfABI = mustABI(`[{"type":"function","name":"orgsOf","stateMutability":"view","inputs":[{"name":"owner","type":"address"}],"outputs":[{"type":"string[]"}]}]`)
	// Claimed(address indexed issuer, uint256 indexed nonce, address indexed claimer, string name, address resolver)
	claimedTopic = crypto.Keccak256Hash([]byte("Claimed(address,uint256,address,string,address)"))
	claimedArgs  = func() abi.Arguments {
		str, _ := abi.NewType("string", "", nil)
		ad, _ := abi.NewType("address", "", nil)
		return abi.Arguments{{Type: str}, {Type: ad}}
	}()
	rank = map[string]int{"owner": 4, "admin": 3, "member": 2, "agent": 1}
)

type myOrgsCache struct {
	mu sync.Mutex
	m  map[common.Address]myOrgsEntry
}
type myOrgsEntry struct {
	at   time.Time
	orgs []MyOrg
}

var myOrgs = &myOrgsCache{m: map[common.Address]myOrgsEntry{}}

// OrgsFor lists the orgs addr belongs to, from chain state only:
//   - created with MeshyOrgFactory (orgsOf)            -> owner
//   - claimed a people/agents name via MeshyInvites    -> member / agent
//   - owner or REGISTRAR on a known org's registries   -> owner / admin
func (s *Server) OrgsFor(ctx context.Context, addr common.Address) []MyOrg {
	myOrgs.mu.Lock()
	if e, ok := myOrgs.m[addr]; ok && time.Since(e.at) < 30*time.Second {
		myOrgs.mu.Unlock()
		return e.orgs
	}
	myOrgs.mu.Unlock()

	best := map[string]MyOrg{}
	put := func(o MyOrg) {
		if cur, ok := best[o.Org]; !ok || rank[o.Role] > rank[cur.Role] || (cur.Name == "" && o.Name != "" && rank[o.Role] >= rank[cur.Role]) {
			if ok && cur.Name != "" && o.Name == "" {
				o.Name = cur.Name
			}
			best[o.Org] = o
		}
	}

	// 1. Orgs this wallet created.
	if s.cfg.OrgFactory != (common.Address{}) {
		if res, err := s.ethCall(ctx, s.cfg.OrgFactory, orgsOfABI, "orgsOf", addr); err == nil && len(res) > 0 {
			for _, o := range res[0].([]string) {
				put(MyOrg{Org: o, Role: "owner"})
			}
		}
	}
	// 2. Names this wallet claimed through invites.
	if s.cfg.Invites != (common.Address{}) {
		logs, err := s.mapBuilder.ETH.FilterLogs(ctx, ethereum.FilterQuery{
			FromBlock: new(big.Int).SetUint64(s.cfg.OrgFactoryFrom),
			Addresses: []common.Address{s.cfg.Invites},
			Topics:    [][]common.Hash{{claimedTopic}, nil, nil, {common.BytesToHash(addr.Bytes())}},
		})
		if err == nil {
			for _, l := range logs {
				v, err := claimedArgs.Unpack(l.Data)
				if err != nil || len(v) < 1 {
					continue
				}
				name := v[0].(string)
				role := "member"
				if strings.Contains(name, ".agents.") {
					role = "agent"
				}
				put(MyOrg{Org: string(enskit.Name(name).Org()), Role: role, Name: name})
			}
		}
	}
	// 3. Ownership / admin roles on the orgs this provider knows (configured + discovered).
	for _, org := range s.orgsToReconcile(ctx) {
		if _, done := best[org]; done && best[org].Role == "owner" {
			continue
		}
		if owner, err := s.mapBuilder.ENS.ExactOwner(ctx, enskit.Name(org), nil); err == nil && owner == addr {
			put(MyOrg{Org: org, Role: "owner"})
			continue
		}
		if s.isAdminOf(ctx, org, addr) {
			put(MyOrg{Org: org, Role: "admin"})
		}
	}

	out := make([]MyOrg, 0, len(best))
	for _, o := range best {
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool {
		if rank[out[i].Role] != rank[out[j].Role] {
			return rank[out[i].Role] > rank[out[j].Role]
		}
		return out[i].Org < out[j].Org
	})
	myOrgs.mu.Lock()
	myOrgs.m[addr] = myOrgsEntry{at: time.Now(), orgs: out}
	myOrgs.mu.Unlock()
	return out
}

// isAdminOf: REGISTRAR on the org registry, or on any network registry of org.
func (s *Server) isAdminOf(ctx context.Context, org string, addr common.Address) bool {
	has := func(reg common.Address) bool {
		if reg == (common.Address{}) {
			return false
		}
		res, err := s.ethCall(ctx, reg, rolesABI, "hasRootRoles", enskit.RoleRegistrar, addr)
		return err == nil && len(res) > 0 && res[0].(bool)
	}
	if reg, err := s.mapBuilder.ENS.ParentRegistry(ctx, enskit.Name("agents."+org), nil); err == nil && has(reg) {
		return true
	}
	m, err := s.mapBuilder.Build(ctx, org)
	if err != nil {
		return false
	}
	for _, n := range m.Networks {
		for _, r := range n.Resources {
			if reg, err := s.mapBuilder.ENS.ParentRegistry(ctx, enskit.Name(r.FullName), nil); err == nil && has(reg) {
				return true
			}
			break // one device per network is enough to find its registry
		}
	}
	return false
}

func (s *Server) handleMyOrgs(w http.ResponseWriter, r *http.Request) {
	a := r.URL.Query().Get("addr")
	if !common.IsHexAddress(a) {
		jsonError(w, http.StatusBadRequest, "addr=0x… required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	jsonOK(w, map[string]any{"addr": common.HexToAddress(a).Hex(), "orgs": s.OrgsFor(ctx, common.HexToAddress(a))})
}
