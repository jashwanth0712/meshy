package provider

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type ecdsaKey = ecdsa.PrivateKey

type keySigner struct{ k *ecdsa.PrivateKey }

func (s keySigner) Sign(h []byte) ([]byte, error) { return crypto.Sign(h, s.k) }

func TestRequests(t *testing.T) {
	member, _ := crypto.GenerateKey()
	stranger, _ := crypto.GenerateKey()
	memberAddr := crypto.PubkeyToAddress(member.PublicKey)
	s := &Server{requests: newRequestBook(t.TempDir()), log: slog.Default(),
		actFor: func(_ context.Context, name string, signer common.Address) (bool, error) {
			return name == "roshan.people.acme.eth" && signer == memberAddr, nil
		}}
	h := s.Handler()
	post := func(path string, v any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(v)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", path, bytes.NewReader(b)))
		return w
	}
	req := Request{Org: "acme.eth", From: "roshan.people.acme.eth", Kind: "access", Target: "jash.sample1.acme.eth", Path: "/Users/jash/oct26", Mode: "rw", Dur: 3600, At: time.Now().Unix()}

	signAs := func(r Request, k *ecdsaKey) Request {
		r.Sig, _ = SignText(RequestMessage(r), keySigner{k})
		return r
	}
	if w := post("/v1/requests", signAs(req, stranger)); w.Code != 403 {
		t.Fatalf("a stranger asked in roshan's name: %d %s", w.Code, w.Body)
	}
	w := post("/v1/requests", signAs(req, member))
	if w.Code != 200 {
		t.Fatalf("member request: %d %s", w.Code, w.Body)
	}
	var got Request
	json.Unmarshal(w.Body.Bytes(), &got)
	// asking again replaces, not duplicates
	post("/v1/requests", signAs(req, member))
	lw := httptest.NewRecorder()
	h.ServeHTTP(lw, httptest.NewRequest("GET", "/v1/requests?org=acme.eth", nil))
	var list struct{ Requests []Request }
	json.Unmarshal(lw.Body.Bytes(), &list)
	if len(list.Requests) != 1 || list.Requests[0].Path != "/Users/jash/oct26" {
		t.Fatalf("list = %+v", list)
	}
	bad := req
	bad.Target = "jash.sample1.other.eth"
	if w := post("/v1/requests", signAs(bad, member)); w.Code != 400 {
		t.Fatalf("cross-org target accepted: %d", w.Code)
	}
	at := time.Now().Unix()
	sig, _ := SignText(WithdrawMessage(got.ID, req.From, at), keySigner{member})
	if w := post("/v1/requests/withdraw", map[string]any{"id": got.ID, "from": req.From, "at": at, "sig": sig}); w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("true")) {
		t.Fatalf("withdraw: %d %s", w.Code, w.Body)
	}
	if n := len(s.requests.list("acme.eth", time.Now())); n != 0 {
		t.Fatalf("%d left after withdraw", n)
	}
	_ = http.StatusOK
}
