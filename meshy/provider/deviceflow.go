package provider

import (
	"context"
	"crypto/ecdsa"
	gocrypto "crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/jashwanth0712/meshy/meshy/worldid"
)

// defaultAllowedACRs are the World ID ACR values we accept by default.
var defaultAllowedACRs = []string{
	"https://world.org/oidc/acr/orb",
	"https://world.org/oidc/acr/device",
	"orb",
	"device",
}

// oidcDiscovery is the .well-known/openid-configuration document.
type oidcDiscovery struct {
	Issuer              string `json:"issuer"`
	DeviceAuthEndpoint  string `json:"device_authorization_endpoint"`
	TokenEndpoint       string `json:"token_endpoint"`
	JWKSURI             string `json:"jwks_uri"`
}

// flowEntry tracks the state of one in-flight device authorization flow.
type flowEntry struct {
	org        string
	agent      string
	addr       string
	deviceCode string // NEVER logged
	interval   int    // polling interval in seconds
	expiresAt  time.Time
	startedAt  time.Time
	issuer     string
	tokenURL   string
	jwksURI    string
}

// FlowManager manages World ID device authorization flows.
type FlowManager struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	AllowedACRs  []string
	SignerKey     *ecdsa.PrivateKey

	mu     sync.Mutex
	flows  map[string]*flowEntry
	oidc   *oidcDiscovery
	oidcAt time.Time
	hc     *http.Client
}

// NewFlowManager constructs a FlowManager.
func NewFlowManager(issuer, clientID, clientSecret string, signerKey *ecdsa.PrivateKey) *FlowManager {
	acrs := make([]string, len(defaultAllowedACRs))
	copy(acrs, defaultAllowedACRs)
	return &FlowManager{
		Issuer:       issuer,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		AllowedACRs:  acrs,
		SignerKey:     signerKey,
		flows:        map[string]*flowEntry{},
		hc:           &http.Client{Timeout: 15 * time.Second},
	}
}

// StartFlowRequest is the payload for POST /v1/agent/device-flow/start.
type StartFlowRequest struct {
	Org   string `json:"org"`
	Agent string `json:"agent"`
	Addr  string `json:"addr"`
}

// StartFlowResponse is the response for POST /v1/agent/device-flow/start.
type StartFlowResponse struct {
	PollID                  string `json:"pollId"`
	UserCode                string `json:"userCode"`
	VerificationURI         string `json:"verificationUri"`
	VerificationURIComplete string `json:"verificationUriComplete"`
	ExpiresIn               int    `json:"expiresIn"`
	Interval                int    `json:"interval"`
}

// PollFlowRequest is the payload for POST /v1/agent/device-flow/poll.
type PollFlowRequest struct {
	PollID string `json:"pollId"`
}

// PollFlowResponse is the response for POST /v1/agent/device-flow/poll.
type PollFlowResponse struct {
	Status      string `json:"status"` // "pending", "denied", "expired", "ok"
	Attestation string `json:"attestation,omitempty"`
	Error       string `json:"error,omitempty"`
}

func (f *FlowManager) discover(ctx context.Context) (*oidcDiscovery, error) {
	f.mu.Lock()
	if f.oidc != nil && time.Since(f.oidcAt) < time.Hour {
		cfg := f.oidc
		f.mu.Unlock()
		return cfg, nil
	}
	f.mu.Unlock()

	discURL := strings.TrimRight(f.Issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, "GET", discURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery: %w", err)
	}
	defer resp.Body.Close()
	var cfg oidcDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("OIDC discovery decode: %w", err)
	}

	f.mu.Lock()
	f.oidc = &cfg
	f.oidcAt = time.Now()
	f.mu.Unlock()
	return &cfg, nil
}

// StartFlow initiates a device authorization flow.
func (f *FlowManager) StartFlow(ctx context.Context, req StartFlowRequest) (*StartFlowResponse, error) {
	cfg, err := f.discover(ctx)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC: %w", err)
	}
	if cfg.DeviceAuthEndpoint == "" {
		return nil, errors.New("OIDC provider does not support device authorization")
	}

	form := url.Values{}
	form.Set("client_id", f.ClientID)
	form.Set("scope", "openid")
	// Request essential acr claim if the provider supports it.
	form.Set("claims", `{"id_token":{"acr":{"essential":true}}}`)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", cfg.DeviceAuthEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.SetBasicAuth(f.ClientID, f.ClientSecret)

	resp, err := f.hc.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("device auth request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("device auth: HTTP %d: %s", resp.StatusCode, body)
	}

	var dar struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
	}
	if err := json.Unmarshal(body, &dar); err != nil {
		return nil, fmt.Errorf("device auth decode: %w", err)
	}

	// Derive a pollId from the device_code (never return or log the device_code itself).
	pollID := base64.RawURLEncoding.EncodeToString(
		crypto.Keccak256([]byte(dar.DeviceCode+fmt.Sprintf("%d", time.Now().UnixNano())))[:16],
	)

	interval := dar.Interval
	if interval == 0 {
		interval = 5
	}
	expiresIn := dar.ExpiresIn
	if expiresIn == 0 {
		expiresIn = 300
	}

	entry := &flowEntry{
		org:        req.Org,
		agent:      req.Agent,
		addr:       req.Addr,
		deviceCode: dar.DeviceCode,
		interval:   interval,
		expiresAt:  time.Now().Add(time.Duration(expiresIn) * time.Second),
		startedAt:  time.Now(),
		issuer:     cfg.Issuer,
		tokenURL:   cfg.TokenEndpoint,
		jwksURI:    cfg.JWKSURI,
	}

	f.mu.Lock()
	f.flows[pollID] = entry
	f.mu.Unlock()

	return &StartFlowResponse{
		PollID:                  pollID,
		UserCode:                dar.UserCode,
		VerificationURI:         dar.VerificationURI,
		VerificationURIComplete: dar.VerificationURIComplete,
		ExpiresIn:               expiresIn,
		Interval:                interval,
	}, nil
}

// PollFlow polls the token endpoint and, on success, validates and signs the attestation.
func (f *FlowManager) PollFlow(ctx context.Context, req PollFlowRequest) (*PollFlowResponse, error) {
	f.mu.Lock()
	entry, ok := f.flows[req.PollID]
	f.mu.Unlock()
	if !ok {
		return &PollFlowResponse{Status: "expired", Error: "unknown or expired poll id"}, nil
	}
	if time.Now().After(entry.expiresAt) {
		f.mu.Lock()
		delete(f.flows, req.PollID)
		f.mu.Unlock()
		return &PollFlowResponse{Status: "expired"}, nil
	}

	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	form.Set("device_code", entry.deviceCode)
	form.Set("client_id", f.ClientID)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", entry.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.SetBasicAuth(f.ClientID, f.ClientSecret)

	resp, err := f.hc.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("token poll: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var tokResp struct {
		Error   string `json:"error"`
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &tokResp); err != nil {
		return nil, fmt.Errorf("token decode: %w", err)
	}

	switch tokResp.Error {
	case "authorization_pending":
		return &PollFlowResponse{Status: "pending"}, nil
	case "slow_down":
		f.mu.Lock()
		if e, ok := f.flows[req.PollID]; ok {
			e.interval += 5
		}
		f.mu.Unlock()
		return &PollFlowResponse{Status: "pending"}, nil
	case "access_denied":
		f.mu.Lock()
		delete(f.flows, req.PollID)
		f.mu.Unlock()
		return &PollFlowResponse{Status: "denied"}, nil
	case "expired_token":
		f.mu.Lock()
		delete(f.flows, req.PollID)
		f.mu.Unlock()
		return &PollFlowResponse{Status: "expired"}, nil
	case "":
		// success
	default:
		return &PollFlowResponse{Status: "denied", Error: tokResp.Error}, nil
	}

	if tokResp.IDToken == "" {
		return nil, errors.New("no id_token in response")
	}

	att, err := f.validateIDToken(ctx, entry, tokResp.IDToken)
	if err != nil {
		f.mu.Lock()
		delete(f.flows, req.PollID)
		f.mu.Unlock()
		return &PollFlowResponse{Status: "denied", Error: "id_token validation: " + err.Error()}, nil
	}

	record, err := worldid.Sign(*att, f.SignerKey)
	if err != nil {
		return nil, fmt.Errorf("sign attestation: %w", err)
	}

	// Single-use: remove the flow.
	f.mu.Lock()
	delete(f.flows, req.PollID)
	f.mu.Unlock()

	return &PollFlowResponse{Status: "ok", Attestation: record}, nil
}

// validateIDToken validates an RS256 JWT and returns a worldid.Attestation.
func (f *FlowManager) validateIDToken(ctx context.Context, entry *flowEntry, idToken string) (*worldid.Attestation, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed JWT")
	}

	headerRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("JWT header: %w", err)
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerRaw, &header); err != nil {
		return nil, err
	}
	if header.Alg != "RS256" {
		return nil, fmt.Errorf("unsupported alg: %s", header.Alg)
	}

	payloadRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("JWT payload: %w", err)
	}
	var claims struct {
		Iss      string   `json:"iss"`
		Aud      any      `json:"aud"`
		Sub      string   `json:"sub"`
		Exp      int64    `json:"exp"`
		AuthTime int64    `json:"auth_time"`
		ACR      string   `json:"acr"`
		AMR      []string `json:"amr"`
	}
	if err := json.Unmarshal(payloadRaw, &claims); err != nil {
		return nil, fmt.Errorf("JWT claims: %w", err)
	}

	now := time.Now()

	if claims.Iss != entry.issuer {
		return nil, fmt.Errorf("iss mismatch: %s != %s", claims.Iss, entry.issuer)
	}

	audOK := false
	switch v := claims.Aud.(type) {
	case string:
		audOK = v == f.ClientID
	case []any:
		for _, a := range v {
			if s, ok := a.(string); ok && s == f.ClientID {
				audOK = true
			}
		}
	}
	if !audOK {
		return nil, fmt.Errorf("aud does not include client_id %s", f.ClientID)
	}

	if now.Unix() > claims.Exp {
		return nil, errors.New("id_token expired")
	}

	if claims.AuthTime < entry.startedAt.Unix() {
		return nil, fmt.Errorf("auth_time %d predates flow start %d", claims.AuthTime, entry.startedAt.Unix())
	}

	acr := claims.ACR
	acrOK := false
	for _, allowed := range f.AllowedACRs {
		if acr == allowed {
			acrOK = true
			break
		}
	}
	if !acrOK && len(claims.AMR) > 0 {
		for _, m := range claims.AMR {
			for _, allowed := range f.AllowedACRs {
				if m == allowed {
					acrOK = true
					acr = m
					break
				}
			}
			if acrOK {
				break
			}
		}
	}
	if !acrOK {
		return nil, fmt.Errorf("acr %q not in allowed set", claims.ACR)
	}

	if err := f.verifyRS256(ctx, entry.jwksURI, header.Kid, parts[0]+"."+parts[1], parts[2]); err != nil {
		return nil, fmt.Errorf("signature: %w", err)
	}

	att := &worldid.Attestation{
		Agent:    entry.agent + ".agents." + entry.org,
		Addr:     common.HexToAddress(entry.addr).Hex(),
		Human:    worldid.HumanHash(claims.Iss, claims.Sub, entry.org),
		ACR:      acr,
		AuthTime: claims.AuthTime,
		IssuedAt: now.Unix(),
		Expires:  now.Add(7 * 24 * time.Hour).Unix(),
	}
	return att, nil
}

func (f *FlowManager) verifyRS256(ctx context.Context, jwksURI, kid, sigInput, sigB64 string) error {
	keys, err := fetchJWKS(ctx, f.hc, jwksURI)
	if err != nil {
		return err
	}

	var pub *rsa.PublicKey
	for _, k := range keys.Keys {
		if (kid == "" || k.Kid == kid) && k.Kty == "RSA" {
			p, err := jwkToRSA(k)
			if err != nil {
				continue
			}
			pub = p
			break
		}
	}
	if pub == nil {
		return fmt.Errorf("no matching RSA key for kid=%q", kid)
	}

	sig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return fmt.Errorf("sig decode: %w", err)
	}
	h := sha256.Sum256([]byte(sigInput))
	return rsa.VerifyPKCS1v15(pub, gocrypto.SHA256, h[:], sig)
}

// jwksCache caches JWKS by URI.
var (
	jwksMu    sync.Mutex
	jwksCache = map[string]*jwksDoc{}
	jwksTime  = map[string]time.Time{}
)

type jwksDoc struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func fetchJWKS(ctx context.Context, hc *http.Client, uri string) (*jwksDoc, error) {
	jwksMu.Lock()
	if doc, ok := jwksCache[uri]; ok && time.Since(jwksTime[uri]) < 10*time.Minute {
		jwksMu.Unlock()
		return doc, nil
	}
	jwksMu.Unlock()

	req, err := http.NewRequestWithContext(ctx, "GET", uri, nil)
	if err != nil {
		return nil, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var doc jwksDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, err
	}

	jwksMu.Lock()
	jwksCache[uri] = &doc
	jwksTime[uri] = time.Now()
	jwksMu.Unlock()
	return &doc, nil
}

func jwkToRSA(k jwkKey) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, err
	}
	n := new(big.Int).SetBytes(nBytes)
	eInt := new(big.Int).SetBytes(eBytes)
	return &rsa.PublicKey{N: n, E: int(eInt.Int64())}, nil
}

// DevAttestation builds a dev-mode attestation (--dev-attest).
func DevAttestation(org, agent, addr string, signerKey *ecdsa.PrivateKey) (string, error) {
	now := time.Now()
	att := worldid.Attestation{
		Agent:    agent + ".agents." + org,
		Addr:     common.HexToAddress(addr).Hex(),
		Human:    worldid.HumanHash("dev", "dev-human", org),
		ACR:      "dev",
		AuthTime: now.Unix(),
		IssuedAt: now.Unix(),
		Expires:  now.Add(7 * 24 * time.Hour).Unix(),
	}
	return worldid.Sign(att, signerKey)
}

// AdmitPrefix is the EIP-191 message prefix for /v1/admit signature.
const AdmitPrefix = "meshy-admit:"

// AdmitSigner recovers the EIP-191 personal_sign signer from an admit message.
// msg = "meshy-admit:" + name + ":" + wgPubKey
func AdmitSigner(name, wgPubKey, sigHex string) (common.Address, error) {
	msg := AdmitPrefix + name + ":" + wgPubKey
	hash := accounts.TextHash([]byte(msg))
	sig := common.FromHex(sigHex)
	if len(sig) != 65 {
		return common.Address{}, errors.New("admit: invalid signature length")
	}
	sig = append([]byte{}, sig...)
	if sig[64] >= 27 {
		sig[64] -= 27
	}
	pub, err := crypto.SigToPub(hash, sig)
	if err != nil {
		return common.Address{}, fmt.Errorf("admit: recover signer: %w", err)
	}
	return crypto.PubkeyToAddress(*pub), nil
}

// CleanExpired removes flow entries that have timed out.
func (f *FlowManager) CleanExpired() {
	f.mu.Lock()
	defer f.mu.Unlock()
	cutoff := time.Now().Add(-20 * time.Minute)
	now := time.Now()
	for id, e := range f.flows {
		if e.startedAt.Before(cutoff) || now.After(e.expiresAt) {
			delete(f.flows, id)
		}
	}
}

// ActiveFlows returns the count of in-flight flows.
func (f *FlowManager) ActiveFlows() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.flows)
}
