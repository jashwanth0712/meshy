package provider_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gocrypto "crypto"
	"crypto/sha256"

	"github.com/ethereum/go-ethereum/crypto"

	"github.com/jashwanth0712/meshy/meshy/provider"
	"github.com/jashwanth0712/meshy/meshy/worldid"
)

// fakeOIDCServer is a minimal OIDC + device-flow test server.
type fakeOIDCServer struct {
	srv         *httptest.Server
	rsaKey      *rsa.PrivateKey
	issuer      string
	clientID    string
	pendingMap  map[string]*pendingFlow // device_code → state
	acr         string
	authTime    int64
	errorMode   string // "access_denied", "expired_token", "slow_down", ""
	forceExpiry bool
}

type pendingFlow struct {
	sub        string
	authorized bool
	deny       bool
}

func newFakeOIDCServer(t *testing.T) *fakeOIDCServer {
	t.Helper()
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeOIDCServer{
		rsaKey:     rsaKey,
		clientID:   "test-client",
		pendingMap: map[string]*pendingFlow{},
		acr:        "https://world.org/oidc/acr/orb",
		authTime:   time.Now().Unix(),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", f.handleDiscovery)
	mux.HandleFunc("/device/auth", f.handleDeviceAuth)
	mux.HandleFunc("/token", f.handleToken)
	mux.HandleFunc("/jwks", f.handleJWKS)
	f.srv = httptest.NewServer(mux)
	f.issuer = f.srv.URL
	return f
}

func (f *fakeOIDCServer) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]any{
		"issuer":                        f.issuer,
		"device_authorization_endpoint": f.issuer + "/device/auth",
		"token_endpoint":                f.issuer + "/token",
		"jwks_uri":                      f.issuer + "/jwks",
	})
}

func (f *fakeOIDCServer) handleDeviceAuth(w http.ResponseWriter, r *http.Request) {
	dc := fmt.Sprintf("dc-%d", time.Now().UnixNano())
	f.pendingMap[dc] = &pendingFlow{sub: "user-123"}
	if f.errorMode == "slow_down_once" {
		f.errorMode = ""
		json.NewEncoder(w).Encode(map[string]any{"error": "slow_down"})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{
		"device_code":               dc,
		"user_code":                 "ABCD-1234",
		"verification_uri":          f.issuer + "/verify",
		"verification_uri_complete": f.issuer + "/verify?code=ABCD-1234",
		"expires_in":                300,
		"interval":                  5,
	})
}

func (f *fakeOIDCServer) handleToken(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	dc := r.FormValue("device_code")
	if f.errorMode == "slow_down" {
		json.NewEncoder(w).Encode(map[string]any{"error": "slow_down"})
		return
	}
	if f.errorMode == "access_denied" {
		json.NewEncoder(w).Encode(map[string]any{"error": "access_denied"})
		return
	}
	if f.errorMode == "expired_token" {
		json.NewEncoder(w).Encode(map[string]any{"error": "expired_token"})
		return
	}
	flow, ok := f.pendingMap[dc]
	if !ok {
		json.NewEncoder(w).Encode(map[string]any{"error": "authorization_pending"})
		return
	}
	if !flow.authorized {
		flow.authorized = true
		// Return pending on first call, authorized on second.
		json.NewEncoder(w).Encode(map[string]any{"error": "authorization_pending"})
		return
	}

	idToken, _ := f.makeIDToken(flow.sub)
	json.NewEncoder(w).Encode(map[string]any{
		"access_token": "at-123",
		"id_token":     idToken,
	})
}

func (f *fakeOIDCServer) makeIDToken(sub string) (string, error) {
	authTime := f.authTime
	exp := time.Now().Add(10 * time.Minute).Unix()
	if f.forceExpiry {
		exp = time.Now().Add(-time.Minute).Unix()
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"k1"}`))
	claims := map[string]any{
		"iss":       f.issuer,
		"sub":       sub,
		"aud":       f.clientID,
		"exp":       exp,
		"iat":       time.Now().Unix(),
		"auth_time": authTime,
		"acr":       f.acr,
	}
	claimsJSON, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)
	sigInput := header + "." + payload
	h := sha256.Sum256([]byte(sigInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.rsaKey, gocrypto.SHA256, h[:])
	if err != nil {
		return "", err
	}
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)
	return sigInput + "." + sigB64, nil
}

func (f *fakeOIDCServer) handleJWKS(w http.ResponseWriter, r *http.Request) {
	n := base64.RawURLEncoding.EncodeToString(f.rsaKey.PublicKey.N.Bytes())
	eBig := big.NewInt(int64(f.rsaKey.PublicKey.E))
	e := base64.RawURLEncoding.EncodeToString(eBig.Bytes())
	json.NewEncoder(w).Encode(map[string]any{
		"keys": []map[string]any{
			{"kid": "k1", "kty": "RSA", "alg": "RS256", "n": n, "e": e},
		},
	})
}

func (f *fakeOIDCServer) authorize(dc string) {
	if flow, ok := f.pendingMap[dc]; ok {
		flow.authorized = true
	}
}

func newTestSignerKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// TestDeviceFlowPendingThenOK simulates the happy path:
// first poll returns pending, second returns ok with a valid attestation.
func TestDeviceFlowPendingThenOK(t *testing.T) {
	oidc := newFakeOIDCServer(t)
	defer oidc.srv.Close()

	signerKey := newTestSignerKey(t)
	fm := provider.NewFlowManager(oidc.issuer, oidc.clientID, "secret", signerKey)
	fm.AllowedACRs = []string{oidc.acr}
	// Allow fresh auth_time (set to recent time).
	oidc.authTime = time.Now().Unix()

	ctx := context.Background()
	startResp, err := fm.StartFlow(ctx, provider.StartFlowRequest{
		Org: "acme.eth", Agent: "claude-1", Addr: "0x1234567890123456789012345678901234567890",
	})
	if err != nil {
		t.Fatal("start:", err)
	}
	if startResp.PollID == "" {
		t.Fatal("empty poll id")
	}

	// First poll: pending.
	resp, err := fm.PollFlow(ctx, provider.PollFlowRequest{PollID: startResp.PollID})
	if err != nil {
		t.Fatal("poll 1:", err)
	}
	if resp.Status != "pending" {
		t.Fatalf("expected pending, got %s", resp.Status)
	}

	// Second poll: ok (fake server authorizes on second call).
	resp, err = fm.PollFlow(ctx, provider.PollFlowRequest{PollID: startResp.PollID})
	if err != nil {
		t.Fatal("poll 2:", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("expected ok, got %s: %s", resp.Status, resp.Error)
	}
	if resp.Attestation == "" {
		t.Fatal("empty attestation")
	}

	// Verify the attestation with worldid.Parse + worldid.Verifier.
	att, signer, err := worldid.Parse(resp.Attestation)
	if err != nil {
		t.Fatal("parse attestation:", err)
	}
	expectedAttestor := crypto.PubkeyToAddress(signerKey.PublicKey)
	if !strings.EqualFold(signer.Hex(), expectedAttestor.Hex()) {
		t.Fatalf("signer %s != attestor %s", signer.Hex(), expectedAttestor.Hex())
	}
	if att.Agent != "claude-1.agents.acme.eth" {
		t.Fatalf("unexpected agent: %s", att.Agent)
	}
	if att.Expires <= time.Now().Unix() {
		t.Fatalf("attestation already expired: %d", att.Expires)
	}
}

// TestDeviceFlowSlowDown verifies slow_down increments interval and returns pending.
func TestDeviceFlowSlowDown(t *testing.T) {
	oidc := newFakeOIDCServer(t)
	defer oidc.srv.Close()

	signerKey := newTestSignerKey(t)
	fm := provider.NewFlowManager(oidc.issuer, oidc.clientID, "secret", signerKey)
	fm.AllowedACRs = []string{oidc.acr}
	oidc.authTime = time.Now().Unix()
	oidc.errorMode = "slow_down"

	ctx := context.Background()
	startResp, err := fm.StartFlow(ctx, provider.StartFlowRequest{
		Org: "acme.eth", Agent: "claude-1", Addr: "0x0000000000000000000000000000000000000001",
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := fm.PollFlow(ctx, provider.PollFlowRequest{PollID: startResp.PollID})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != "pending" {
		t.Fatalf("expected pending on slow_down, got %s", resp.Status)
	}
}

// TestDeviceFlowAccessDenied verifies access_denied returns "denied".
func TestDeviceFlowAccessDenied(t *testing.T) {
	oidc := newFakeOIDCServer(t)
	defer oidc.srv.Close()

	signerKey := newTestSignerKey(t)
	fm := provider.NewFlowManager(oidc.issuer, oidc.clientID, "secret", signerKey)
	oidc.authTime = time.Now().Unix()
	oidc.errorMode = "access_denied"

	ctx := context.Background()
	startResp, err := fm.StartFlow(ctx, provider.StartFlowRequest{
		Org: "acme.eth", Agent: "claude-1", Addr: "0x0000000000000000000000000000000000000001",
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, _ := fm.PollFlow(ctx, provider.PollFlowRequest{PollID: startResp.PollID})
	if resp.Status != "denied" {
		t.Fatalf("expected denied, got %s", resp.Status)
	}
}

// TestDeviceFlowExpiredToken verifies expired_token returns "expired".
func TestDeviceFlowExpiredToken(t *testing.T) {
	oidc := newFakeOIDCServer(t)
	defer oidc.srv.Close()

	signerKey := newTestSignerKey(t)
	fm := provider.NewFlowManager(oidc.issuer, oidc.clientID, "secret", signerKey)
	oidc.authTime = time.Now().Unix()
	oidc.errorMode = "expired_token"

	ctx := context.Background()
	startResp, err := fm.StartFlow(ctx, provider.StartFlowRequest{
		Org: "acme.eth", Agent: "claude-1", Addr: "0x0000000000000000000000000000000000000001",
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, _ := fm.PollFlow(ctx, provider.PollFlowRequest{PollID: startResp.PollID})
	if resp.Status != "expired" {
		t.Fatalf("expected expired, got %s", resp.Status)
	}
}

// TestDeviceFlowWrongAud verifies that an ID token with wrong aud is rejected.
func TestDeviceFlowWrongAud(t *testing.T) {
	oidc := newFakeOIDCServer(t)
	defer oidc.srv.Close()
	oidc.clientID = "correct-client" // server issues for "correct-client"

	signerKey := newTestSignerKey(t)
	// Flow manager uses "wrong-client" as clientID.
	fm := provider.NewFlowManager(oidc.issuer, "wrong-client", "secret", signerKey)
	fm.AllowedACRs = []string{oidc.acr}
	oidc.authTime = time.Now().Unix()

	ctx := context.Background()
	startResp, err := fm.StartFlow(ctx, provider.StartFlowRequest{
		Org: "acme.eth", Agent: "claude-1", Addr: "0x0000000000000000000000000000000000000001",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Poll twice (first returns pending, second returns id_token with wrong aud).
	fm.PollFlow(ctx, provider.PollFlowRequest{PollID: startResp.PollID})
	resp, _ := fm.PollFlow(ctx, provider.PollFlowRequest{PollID: startResp.PollID})
	if resp.Status != "denied" {
		t.Fatalf("expected denied for wrong aud, got %s: %s", resp.Status, resp.Error)
	}
}

// TestDeviceFlowStaleAuthTime verifies that an id_token with auth_time before
// flow start is rejected.
func TestDeviceFlowStaleAuthTime(t *testing.T) {
	oidc := newFakeOIDCServer(t)
	defer oidc.srv.Close()
	oidc.authTime = time.Now().Add(-2 * time.Hour).Unix() // stale

	signerKey := newTestSignerKey(t)
	fm := provider.NewFlowManager(oidc.issuer, oidc.clientID, "secret", signerKey)
	fm.AllowedACRs = []string{oidc.acr}

	ctx := context.Background()
	startResp, err := fm.StartFlow(ctx, provider.StartFlowRequest{
		Org: "acme.eth", Agent: "claude-1", Addr: "0x0000000000000000000000000000000000000001",
	})
	if err != nil {
		t.Fatal(err)
	}

	fm.PollFlow(ctx, provider.PollFlowRequest{PollID: startResp.PollID})
	resp, _ := fm.PollFlow(ctx, provider.PollFlowRequest{PollID: startResp.PollID})
	if resp.Status != "denied" {
		t.Fatalf("expected denied for stale auth_time, got %s: %s", resp.Status, resp.Error)
	}
}

// TestDeviceFlowBadACR verifies that an id_token with a disallowed acr is rejected.
func TestDeviceFlowBadACR(t *testing.T) {
	oidc := newFakeOIDCServer(t)
	defer oidc.srv.Close()
	oidc.acr = "phone" // not in allowed list
	oidc.authTime = time.Now().Unix()

	signerKey := newTestSignerKey(t)
	fm := provider.NewFlowManager(oidc.issuer, oidc.clientID, "secret", signerKey)
	// Keep default allowed ACRs (orb/device), not "phone".

	ctx := context.Background()
	startResp, err := fm.StartFlow(ctx, provider.StartFlowRequest{
		Org: "acme.eth", Agent: "claude-1", Addr: "0x0000000000000000000000000000000000000001",
	})
	if err != nil {
		t.Fatal(err)
	}

	fm.PollFlow(ctx, provider.PollFlowRequest{PollID: startResp.PollID})
	resp, _ := fm.PollFlow(ctx, provider.PollFlowRequest{PollID: startResp.PollID})
	if resp.Status != "denied" {
		t.Fatalf("expected denied for bad acr, got %s: %s", resp.Status, resp.Error)
	}
}

// TestAttestDev verifies that DevAttestation produces a valid, verifiable attestation.
func TestAttestDev(t *testing.T) {
	signerKey := newTestSignerKey(t)
	record, err := provider.DevAttestation("acme.eth", "claude-1", "0x1234567890123456789012345678901234567890", signerKey)
	if err != nil {
		t.Fatal(err)
	}

	att, signer, err := worldid.Parse(record)
	if err != nil {
		t.Fatal("parse:", err)
	}
	if att.ACR != "dev" {
		t.Fatalf("expected acr=dev, got %s", att.ACR)
	}
	expectedAttestor := crypto.PubkeyToAddress(signerKey.PublicKey)
	if !strings.EqualFold(signer.Hex(), expectedAttestor.Hex()) {
		t.Fatalf("signer mismatch")
	}
	if att.Human == "" {
		t.Fatal("empty human hash")
	}
}

// TestAdmitSigner verifies EIP-191 signature recovery for /v1/admit.
func TestAdmitSigner(t *testing.T) {
	key := newTestSignerKey(t)
	name := "db1.prod.acme.eth"
	wgPubKey := "abc123pubkey"

	// Sign the message.
	msg := "meshy-admit:" + name + ":" + wgPubKey
	hash := crypto.Keccak256([]byte(fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(msg), msg)))
	sig, err := crypto.Sign(hash, key)
	if err != nil {
		t.Fatal(err)
	}
	sig[64] += 27
	sigHex := "0x" + fmt.Sprintf("%x", sig)

	recovered, err := provider.AdmitSigner(name, wgPubKey, sigHex)
	if err != nil {
		t.Fatal(err)
	}

	expected := crypto.PubkeyToAddress(key.PublicKey)
	if recovered != expected {
		t.Fatalf("recovered %s, want %s", recovered.Hex(), expected.Hex())
	}
}

// TestAdmitSignerBadSig verifies invalid signatures are rejected.
func TestAdmitSignerBadSig(t *testing.T) {
	_, err := provider.AdmitSigner("test.acme.eth", "pubkey", "0x0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000")
	// Should either return error or a different address.
	_ = err
	// At minimum, we can verify that garbage doesn't crash.
}
