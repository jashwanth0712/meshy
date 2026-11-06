// Package provider implements the meshy-sync HTTP API server.
//
// Endpoints:
//
//	GET  /v1/health
//	POST /v1/agent/device-flow/start
//	POST /v1/agent/device-flow/poll
//	POST /v1/agent/attest-dev        (only when --dev-attest)
//	GET  /v1/map?org=<org.eth>
//	POST /v1/admit
package provider

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/jashwanth0712/meshy/meshy/enskit"
	"github.com/jashwanth0712/meshy/meshy/provider/netbird"
)

// Version is the meshy-sync version.
const Version = "0.1.0"

// Config holds all runtime configuration for the server.
type Config struct {
	Listen            string
	SepoliaRPC        string
	SignerKeyFile     string
	WorldIssuer       string
	WorldClientID     string
	WorldClientSecret string
	DevAttest         bool
	CORSOrigins       []string // allowed origins; default ["http://localhost:3000"]
	NetBirdAPI        string
	NetBirdToken      string
	NetBirdMgmtURL    string
	FromBlock         uint64
	Log               *slog.Logger
}

// Server is the meshy-sync HTTP server.
type Server struct {
	cfg        Config
	signerKey  *ecdsa.PrivateKey
	flows      *FlowManager
	mapBuilder *MapBuilder
	nb         *netbird.Client
	log        *slog.Logger
}

// NewServer creates a Server and connects to Sepolia.
func NewServer(ctx context.Context, cfg Config, signerKey *ecdsa.PrivateKey) (*Server, error) {
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}

	eth, err := ethclient.DialContext(ctx, cfg.SepoliaRPC)
	if err != nil {
		return nil, fmt.Errorf("dial Sepolia: %w", err)
	}

	ensClient := enskit.NewClient(eth, enskit.SepoliaUniversalHelper)
	fromBlock := cfg.FromBlock
	if fromBlock == 0 {
		fromBlock = 12072900
	}

	var flows *FlowManager
	if cfg.WorldClientID != "" {
		issuer := cfg.WorldIssuer
		if issuer == "" {
			issuer = "https://sandbox.auth.world.org"
		}
		flows = NewFlowManager(issuer, cfg.WorldClientID, cfg.WorldClientSecret, signerKey)
	}

	var nb *netbird.Client
	if cfg.NetBirdAPI != "" && cfg.NetBirdToken != "" {
		nb = netbird.NewClient(cfg.NetBirdAPI, cfg.NetBirdToken)
	}

	s := &Server{
		cfg:       cfg,
		signerKey: signerKey,
		flows:     flows,
		mapBuilder: &MapBuilder{
			ENS:       ensClient,
			ETH:       eth,
			FromBlock: fromBlock,
			Log:       log,
		},
		nb:  nb,
		log: log,
	}
	return s, nil
}

// Handler returns the HTTP handler for all /v1 routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", s.handleHealth)
	mux.HandleFunc("POST /v1/agent/device-flow/start", s.handleDeviceFlowStart)
	mux.HandleFunc("POST /v1/agent/device-flow/poll", s.handleDeviceFlowPoll)
	mux.HandleFunc("POST /v1/agent/attest-dev", s.handleAttestDev)
	mux.HandleFunc("GET /v1/map", s.handleMap)
	mux.HandleFunc("POST /v1/admit", s.handleAdmit)

	return s.corsMiddleware(mux)
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	origins := s.cfg.CORSOrigins
	if len(origins) == 0 {
		origins = []string{"http://localhost:3000"}
	}
	allowedSet := map[string]struct{}{}
	for _, o := range origins {
		allowedSet[o] = struct{}{}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if _, ok := allowedSet[origin]; ok {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- Handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	worldMode := "disabled"
	if s.flows != nil {
		if strings.Contains(s.flows.Issuer, "sandbox") {
			worldMode = "sandbox"
		} else {
			worldMode = "production"
		}
	}
	jsonOK(w, map[string]any{
		"ok":        true,
		"attestor":  crypto.PubkeyToAddress(s.signerKey.PublicKey).Hex(),
		"world":     worldMode,
		"devAttest": s.cfg.DevAttest,
		"version":   Version,
	})
}

func (s *Server) handleDeviceFlowStart(w http.ResponseWriter, r *http.Request) {
	if s.flows == nil {
		jsonError(w, http.StatusServiceUnavailable, "World ID not configured; set --world-client-id and --world-client-secret")
		return
	}
	var req StartFlowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Org == "" || req.Agent == "" || req.Addr == "" {
		jsonError(w, http.StatusBadRequest, "org, agent, addr are required")
		return
	}

	resp, err := s.flows.StartFlow(r.Context(), req)
	if err != nil {
		s.log.Warn("device-flow start", "err", err)
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonOK(w, resp)
}

func (s *Server) handleDeviceFlowPoll(w http.ResponseWriter, r *http.Request) {
	if s.flows == nil {
		jsonError(w, http.StatusServiceUnavailable, "World ID not configured")
		return
	}
	var req PollFlowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.PollID == "" {
		jsonError(w, http.StatusBadRequest, "pollId is required")
		return
	}

	resp, err := s.flows.PollFlow(r.Context(), req)
	if err != nil {
		s.log.Warn("device-flow poll", "err", err)
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, resp)
}

func (s *Server) handleAttestDev(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.DevAttest {
		http.NotFound(w, r)
		return
	}
	var req struct {
		Org   string `json:"org"`
		Agent string `json:"agent"`
		Addr  string `json:"addr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Org == "" || req.Agent == "" || req.Addr == "" {
		jsonError(w, http.StatusBadRequest, "org, agent, addr are required")
		return
	}

	record, err := DevAttestation(req.Org, req.Agent, req.Addr, s.signerKey)
	if err != nil {
		s.log.Warn("attest-dev", "err", err)
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{"attestation": record, "dev": true})
}

func (s *Server) handleMap(w http.ResponseWriter, r *http.Request) {
	org := r.URL.Query().Get("org")
	if org == "" {
		jsonError(w, http.StatusBadRequest, "org query parameter required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	m, err := s.mapBuilder.Build(ctx, org)
	if err != nil {
		s.log.Warn("map", "org", org, "err", err)
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonOK(w, m)
}

func (s *Server) handleAdmit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		WGPubKey string `json:"wgPubKey"`
		Sig      string `json:"sig"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Name == "" || req.WGPubKey == "" || req.Sig == "" {
		jsonError(w, http.StatusBadRequest, "name, wgPubKey, sig are required")
		return
	}

	if s.nb == nil {
		jsonError(w, http.StatusServiceUnavailable, "NetBird not configured; set --netbird-api and --netbird-token")
		return
	}

	// Recover signer from EIP-191 signature.
	signer, err := AdmitSigner(req.Name, req.WGPubKey, req.Sig)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "invalid signature: "+err.Error())
		return
	}

	// Verify that the signer owns the ENS name.
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	name := enskit.Name(req.Name)
	owner, err := s.mapBuilder.ENS.ExactOwner(ctx, name, nil)
	if err != nil {
		jsonError(w, http.StatusBadGateway, "ENS lookup: "+err.Error())
		return
	}
	if owner == (common.Address{}) {
		jsonError(w, http.StatusForbidden, "name not registered or expired")
		return
	}
	if !strings.EqualFold(owner.Hex(), signer.Hex()) {
		jsonError(w, http.StatusForbidden, "signature does not match owner of "+req.Name)
		return
	}

	// Optionally check meshy.wg record matches.
	rec, err := s.mapBuilder.ENS.Lookup(ctx, name)
	if err == nil && rec.Resolver != (common.Address{}) {
		if wgRec, err := rec.Text(ctx, s.mapBuilder.ENS, "meshy.wg"); err == nil && wgRec != "" {
			if wgRec != req.WGPubKey {
				jsonError(w, http.StatusForbidden, "meshy.wg record does not match provided wgPubKey")
				return
			}
		}
	}

	// Determine auto_groups: the network group for this device's network.
	// The name structure: <device>.<network>.<org>.eth or <resource>.<network>.<org>.eth
	autoGroups := []string{}
	parts := strings.Split(req.Name, ".")
	if len(parts) >= 4 {
		// Try to get the network group from NetBird.
		netLabel := parts[1]
		orgLabel := strings.Join(parts[2:], ".")
		netGroupName := netbird.PeerName("") // placeholder
		_ = netGroupName
		groups, err := s.nb.ListGroups(ctx)
		if err == nil {
			netTarget := "meshy:net:" + netLabel + "." + orgLabel
			for _, g := range groups {
				if g.Name == netTarget {
					autoGroups = append(autoGroups, g.ID)
					break
				}
			}
		}
	}

	// Create a one-off setup key.
	key, err := s.nb.CreateSetupKey(ctx, netbird.CreateSetupKeyRequest{
		Name:       "meshy-admit-" + req.Name + "-" + fmt.Sprintf("%d", time.Now().Unix()),
		Type:       "one-off",
		ExpiresIn:  3600, // 1 hour to use
		AutoGroups: autoGroups,
		UsageLimit: 1,
		Ephemeral:  false,
	})
	if err != nil {
		s.log.Warn("admit: create setup key", "name", req.Name, "err", err)
		jsonError(w, http.StatusInternalServerError, "setup key: "+err.Error())
		return
	}

	mgmtURL := s.cfg.NetBirdMgmtURL
	if mgmtURL == "" {
		mgmtURL = s.cfg.NetBirdAPI
	}

	jsonOK(w, map[string]any{
		"setupKey":      key.Key,
		"managementUrl": mgmtURL,
	})
}

// --- helpers ---

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
