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

	// Orgs whose ENS state is reconciled into NetBird (deny-by-default mesh).
	Orgs []string
	// On-chain economics. Zero Subscription disables seat enforcement.
	Subscription     common.Address
	ProviderRegistry common.Address
	ProviderID       uint64
	ChainID          uint64
	// StateDir holds attestations.jsonl (evidence for slashing challenges).
	StateDir string
	// OrgFactory: MeshyOrgFactory; orgs it creates are discovered and reconciled
	// automatically and get TrialSeats until they subscribe.
	OrgFactory     common.Address
	OrgFactoryFrom uint64
	Invites        common.Address // MeshyInvites (Claimed events: memberships)
}

// Server is the meshy-sync HTTP server.
type Server struct {
	cfg        Config
	signerKey  *ecdsa.PrivateKey
	flows      *FlowManager
	mapBuilder *MapBuilder
	nb         *netbird.Client
	rec        *netbird.Reconciler
	attest     *attestLog
	requests   *requestBook
	log        *slog.Logger
	// actFor overrides the ENS check in mayActFor (tests).
	actFor func(ctx context.Context, name string, signer common.Address) (bool, error)
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
		nb:       nb,
		attest:   newAttestLog(cfg.StateDir),
		requests: newRequestBook(cfg.StateDir),
		log:      log,
	}
	if nb != nil {
		s.rec = &netbird.Reconciler{Client: nb, Log: log}
	}
	if s.cfg.ChainID == 0 {
		s.cfg.ChainID = 11155111
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
	mux.HandleFunc("GET /v1/attestations", s.handleAttestations)
	mux.HandleFunc("GET /v1/seats", s.handleSeats)
	mux.HandleFunc("GET /v1/orgs", s.handleMyOrgs)
	mux.HandleFunc("GET /v1/requests", s.handleRequestsList)
	mux.HandleFunc("POST /v1/requests", s.handleRequestsCreate)
	mux.HandleFunc("POST /v1/requests/withdraw", s.handleRequestsWithdraw)

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
		"mesh":      s.meshHealth(),
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

func (s *Server) meshHealth() map[string]any {
	if s.nb == nil {
		return map[string]any{"enabled": false}
	}
	out := map[string]any{
		"enabled":       true,
		"management":    s.cfg.NetBirdMgmtURL,
		"orgs":          s.cfg.Orgs,
		"providerId":    s.cfg.ProviderID,
		"seatsEnforced": s.cfg.Subscription != (common.Address{}),
	}
	if s.rec != nil {
		out["lastSync"] = s.rec.Last
	}
	return out
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
