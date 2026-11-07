package provider

// Requests from members to the admins of their org, without links to copy:
//
//	POST /v1/requests            {org, from, kind, target, path, mode, dur, joinCode, at, sig}
//	GET  /v1/requests?org=o.eth  pending requests, newest first
//	POST /v1/requests/withdraw   {id, from, at, sig}
//
// kind "access": from wants a grant on target (device or service), for path
// (devices) in mode ro|rw, for dur seconds. kind "device": from wants target
// (<device>.<network>.<org>) added with joinCode (from `meshyd init`).
//
// A request is signed (EIP-191) by the addr record or the ENS owner of from
// (a member or agent name in org), the same rule as /v1/admit, so nobody can
// ask in someone else's name. A request grants nothing: an owner or admin
// still signs the grant in the dashboard. Requests expire after a day.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/jashwanth0712/meshy/meshy/enskit"
)

const requestTTL = 24 * time.Hour

// Request is one pending ask from a member to the admins of an org.
type Request struct {
	ID       string `json:"id"`
	Org      string `json:"org"`
	From     string `json:"from"`          // roshan.people.acme.eth or claude-1.agents.acme.eth
	Kind     string `json:"kind"`          // access | device
	For      string `json:"for,omitempty"` // access: who gets the grant (a label in org); default: from
	Target   string `json:"target"`
	Path     string `json:"path,omitempty"`
	Mode     string `json:"mode,omitempty"`
	Dur      int    `json:"dur,omitempty"`
	JoinCode string `json:"joinCode,omitempty"`
	// kind "roots": the folders the device wants to share from now on (meshy.roots).
	Roots []string `json:"roots,omitempty"`
	At       int64  `json:"at"`
	Sig      string `json:"sig,omitempty"`
}

// RequestMessage is what the requester signs (EIP-191 personal_sign).
func RequestMessage(r Request) string {
	parts := []string{"meshy-request", r.Org, r.From, r.Kind, r.For, r.Target, r.Path, r.Mode,
		strconv.Itoa(r.Dur), r.JoinCode, strconv.FormatInt(r.At, 10)}
	if len(r.Roots) > 0 { // only "roots" requests carry it, so older signers keep working
		parts = append(parts, strings.Join(r.Roots, ","))
	}
	return strings.Join(parts, "\n")
}

// WithdrawMessage is what the requester signs to take a request back.
func WithdrawMessage(id, from string, at int64) string {
	return strings.Join([]string{"meshy-request-withdraw", id, from, strconv.FormatInt(at, 10)}, "\n")
}

// SignText signs msg with EIP-191, as the CLI does for requests.
func SignText(msg string, key interface{ Sign([]byte) ([]byte, error) }) (string, error) {
	sig, err := key.Sign(accounts.TextHash([]byte(msg)))
	if err != nil {
		return "", err
	}
	return "0x" + common.Bytes2Hex(sig), nil
}

func textSigner(msg, sigHex string) (common.Address, error) {
	sig := common.FromHex(sigHex)
	if len(sig) != 65 {
		return common.Address{}, errors.New("signature must be 65 bytes")
	}
	sig = append([]byte{}, sig...)
	if sig[64] >= 27 {
		sig[64] -= 27
	}
	pub, err := crypto.SigToPub(accounts.TextHash([]byte(msg)), sig)
	if err != nil {
		return common.Address{}, err
	}
	return crypto.PubkeyToAddress(*pub), nil
}

func requestID(r Request) string {
	return common.Bytes2Hex(crypto.Keccak256([]byte(RequestMessage(r))))[:16]
}

// Validate checks the shape of a request (not the signature).
func (r *Request) Validate(now time.Time) error {
	r.Org = strings.ToLower(strings.TrimSpace(r.Org))
	r.From = strings.ToLower(strings.TrimSpace(r.From))
	r.Target = strings.ToLower(strings.TrimSpace(r.Target))
	if r.Org == "" || orgFromName(r.From) != r.Org || orgFromName(r.Target) != r.Org {
		return errors.New("from and target must be names in org")
	}
	if !strings.HasSuffix(r.From, ".people."+r.Org) && !strings.HasSuffix(r.From, ".agents."+r.Org) {
		return errors.New("from must be a member (<you>.people.<org>) or an agent (<agent>.agents.<org>)")
	}
	if d := now.Sub(time.Unix(r.At, 0)); d > 10*time.Minute || d < -10*time.Minute {
		return errors.New("at must be within 10 minutes of now")
	}
	switch r.Kind {
	case "access":
		r.For = strings.ToLower(strings.TrimSpace(r.For))
		if r.For == "" {
			r.For = enskit.Name(r.From).Label()
		}
		if strings.Contains(r.For, ".") {
			return errors.New("for is a label, like claude-1")
		}
		if r.Mode != "" && r.Mode != "ro" && r.Mode != "rw" {
			return errors.New("mode must be ro or rw")
		}
		if r.Path != "" && (!strings.HasPrefix(r.Path, "/") || r.Path == "/") {
			return errors.New("path must be an absolute folder")
		}
		if r.Dur < 60 || r.Dur > 30*86400 {
			return errors.New("dur must be between a minute and 30 days")
		}
	case "device":
		if enskit.Name(r.Target).Depth() != 2 {
			return errors.New("a device is <device>.<network>.<org>")
		}
		if !strings.HasPrefix(r.JoinCode, "meshy-join:") || len(r.JoinCode) > 4096 {
			return errors.New("joinCode must be a meshyd join code")
		}
	case "roots":
		if enskit.Name(r.Target).Depth() != 2 {
			return errors.New("a device is <device>.<network>.<org>")
		}
		if len(r.Roots) == 0 || len(r.Roots) > 32 {
			return errors.New("roots: 1 to 32 folders")
		}
		for _, p := range r.Roots {
			if !strings.HasPrefix(p, "/") || p == "/" || strings.Contains(p, ",") {
				return errors.New("roots must be absolute folders")
			}
		}
	default:
		return errors.New("kind must be access, device or roots")
	}
	return nil
}

type requestBook struct {
	mu   sync.Mutex
	path string
	all  []Request
}

func newRequestBook(dir string) *requestBook {
	b := &requestBook{}
	if dir != "" {
		b.path = filepath.Join(dir, "requests.json")
		if raw, err := os.ReadFile(b.path); err == nil {
			json.Unmarshal(raw, &b.all)
		}
	}
	return b
}

func (b *requestBook) saveLocked() {
	if b.path == "" {
		return
	}
	raw, _ := json.Marshal(b.all)
	tmp := b.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		os.Rename(tmp, b.path)
	}
}

func (b *requestBook) pruneLocked(now time.Time) {
	keep := b.all[:0]
	for _, r := range b.all {
		if now.Sub(time.Unix(r.At, 0)) < requestTTL {
			keep = append(keep, r)
		}
	}
	b.all = keep
}

func (b *requestBook) add(r Request, now time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked(now)
	n := 0
	for i, x := range b.all {
		if x.From == r.From {
			n++
		}
		// asking again for the same thing replaces the older ask
		if x.From == r.From && x.Kind == r.Kind && x.For == r.For && x.Target == r.Target && x.Path == r.Path {
			b.all = append(b.all[:i], b.all[i+1:]...)
			n--
			break
		}
	}
	if n >= 20 {
		return errors.New("MESHY_TOO_MANY_REQUESTS withdraw some first")
	}
	b.all = append(b.all, r)
	b.saveLocked()
	return nil
}

func (b *requestBook) list(org string, now time.Time) []Request {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked(now)
	out := []Request{}
	for _, r := range b.all {
		if r.Org == org {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At > out[j].At })
	return out
}

func (b *requestBook) withdraw(id, from string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, r := range b.all {
		if r.ID == id && r.From == from {
			b.all = append(b.all[:i], b.all[i+1:]...)
			b.saveLocked()
			return true
		}
	}
	return false
}

// mayActFor reports whether signer is the addr record or ENS owner of name.
func (s *Server) mayActFor(ctx context.Context, name string, signer common.Address) (bool, error) {
	if s.actFor != nil {
		return s.actFor(ctx, name, signer)
	}
	n := enskit.Name(name)
	if owner, err := s.mapBuilder.ENS.ExactOwner(ctx, n, nil); err == nil && owner == signer && owner != (common.Address{}) {
		return true, nil
	}
	rec, err := s.mapBuilder.ENS.Lookup(ctx, n)
	if err != nil {
		return false, fmt.Errorf("%s is not on ENS", name)
	}
	if rec.State.Status != enskit.Registered {
		return false, fmt.Errorf("%s is not registered", name)
	}
	if rec.Resolver != (common.Address{}) {
		if a, err := s.mapBuilder.ENS.Addr(ctx, rec.Resolver, n); err == nil && a == signer {
			return true, nil
		}
	}
	return false, nil
}

func (s *Server) handleRequestsList(w http.ResponseWriter, r *http.Request) {
	org := strings.ToLower(r.URL.Query().Get("org"))
	if org == "" {
		jsonError(w, http.StatusBadRequest, "org is required")
		return
	}
	jsonOK(w, map[string]any{"org": org, "requests": s.requests.list(org, time.Now())})
}

func (s *Server) handleRequestsCreate(w http.ResponseWriter, r *http.Request) {
	var req Request
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	now := time.Now()
	// The signature covers the request exactly as sent; Validate then normalizes it.
	signer, err := textSigner(RequestMessage(req), req.Sig)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "invalid signature: "+err.Error())
		return
	}
	if err := req.Validate(now); err != nil {
		jsonError(w, http.StatusBadRequest, "MESHY_BAD_REQUEST "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	ok, err := s.mayActFor(ctx, req.From, signer)
	if err != nil {
		jsonError(w, http.StatusForbidden, "MESHY_NOT_ON_ENS "+err.Error())
		return
	}
	if !ok {
		jsonError(w, http.StatusForbidden, "MESHY_NOT_AUTHORIZED signer "+signer.Hex()+" is neither the addr record nor the owner of "+req.From)
		return
	}
	req.ID = requestID(req)
	if err := s.requests.add(req, now); err != nil {
		jsonError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	s.log.Info("request", "org", req.Org, "from", req.From, "kind", req.Kind, "target", req.Target)
	jsonOK(w, req)
}

func (s *Server) handleRequestsWithdraw(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID   string `json:"id"`
		From string `json:"from"`
		At   int64  `json:"at"`
		Sig  string `json:"sig"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	req.From = strings.ToLower(req.From)
	if d := time.Since(time.Unix(req.At, 0)); d > 10*time.Minute || d < -10*time.Minute {
		jsonError(w, http.StatusBadRequest, "at must be within 10 minutes of now")
		return
	}
	signer, err := textSigner(WithdrawMessage(req.ID, req.From, req.At), req.Sig)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "invalid signature: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if ok, err := s.mayActFor(ctx, req.From, signer); err != nil || !ok {
		jsonError(w, http.StatusForbidden, "MESHY_NOT_AUTHORIZED")
		return
	}
	jsonOK(w, map[string]bool{"withdrawn": s.requests.withdraw(req.ID, req.From)})
}
