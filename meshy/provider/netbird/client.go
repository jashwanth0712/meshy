// Package netbird provides a minimal REST client for the NetBird management API
// and a reconciler that maps ENS grant state to NetBird groups, policies and
// DNS records. We drive NetBird only via its REST API (no Go import of the
// NetBird module, which conflicts with go-ethereum's pion/stun dependency).
package netbird

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a thin REST client for the NetBird management API.
type Client struct {
	BaseURL    string
	Token      string // PAT; sent as "Token <PAT>"
	HTTPClient *http.Client
}

func NewClient(baseURL, token string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Token:      token,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Token "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("netbird %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("netbird %s %s: HTTP %d: %s", method, path, resp.StatusCode, raw)
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// --- Groups ---

type Group struct {
	ID         string        `json:"id,omitempty"`
	Name       string        `json:"name"`
	Peers      []PeerMinimum `json:"peers,omitempty"`
	PeersCount int           `json:"peers_count,omitempty"`
	Issued     string        `json:"issued,omitempty"`
}

type PeerMinimum struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	IP   string `json:"ip,omitempty"`
}

type GroupRequest struct {
	Name  string   `json:"name"`
	Peers []string `json:"peers,omitempty"`
}

func (c *Client) ListGroups(ctx context.Context) ([]Group, error) {
	var out []Group
	return out, c.do(ctx, "GET", "/api/groups", nil, &out)
}

func (c *Client) CreateGroup(ctx context.Context, req GroupRequest) (Group, error) {
	var out Group
	return out, c.do(ctx, "POST", "/api/groups", req, &out)
}

func (c *Client) UpdateGroup(ctx context.Context, id string, req GroupRequest) (Group, error) {
	var out Group
	return out, c.do(ctx, "PUT", "/api/groups/"+id, req, &out)
}

func (c *Client) DeleteGroup(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/api/groups/"+id, nil, nil)
}

// --- Policies ---

type Policy struct {
	ID      string       `json:"id,omitempty"`
	Name    string       `json:"name"`
	Enabled bool         `json:"enabled"`
	Rules   []PolicyRule `json:"rules,omitempty"`
}

type PolicyRule struct {
	ID            string   `json:"id,omitempty"`
	Name          string   `json:"name"`
	Enabled       bool     `json:"enabled"`
	Bidirectional bool     `json:"bidirectional"`
	Protocol      string   `json:"protocol"` // "tcp", "udp", "all"
	Action        string   `json:"action"`   // "accept"
	Sources       []string `json:"sources,omitempty"`
	Destinations  []string `json:"destinations,omitempty"`
	Ports         []string `json:"ports,omitempty"`
}

type PolicyCreate struct {
	Name    string       `json:"name"`
	Enabled bool         `json:"enabled"`
	Rules   []PolicyRule `json:"rules"`
}

func (c *Client) ListPolicies(ctx context.Context) ([]Policy, error) {
	var out []Policy
	return out, c.do(ctx, "GET", "/api/policies", nil, &out)
}

func (c *Client) CreatePolicy(ctx context.Context, req PolicyCreate) (Policy, error) {
	var out Policy
	return out, c.do(ctx, "POST", "/api/policies", req, &out)
}

func (c *Client) DeletePolicy(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/api/policies/"+id, nil, nil)
}

// --- Setup Keys ---

type SetupKeyClear struct {
	ID         string    `json:"id"`
	Key        string    `json:"key"`
	Name       string    `json:"name"`
	Expires    time.Time `json:"expires"`
	Type       string    `json:"type"`
	Valid      bool      `json:"valid"`
	Revoked    bool      `json:"revoked"`
	AutoGroups []string  `json:"auto_groups"`
}

type CreateSetupKeyRequest struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`       // "one-off" or "reusable"
	ExpiresIn  int      `json:"expires_in"` // seconds
	AutoGroups []string `json:"auto_groups"`
	UsageLimit int      `json:"usage_limit"` // 1 for one-off
	Ephemeral  bool     `json:"ephemeral"`
}

func (c *Client) CreateSetupKey(ctx context.Context, req CreateSetupKeyRequest) (SetupKeyClear, error) {
	var out SetupKeyClear
	return out, c.do(ctx, "POST", "/api/setup-keys", req, &out)
}

// --- Peers ---

type Peer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	IP   string `json:"ip"`
}

func (c *Client) ListPeers(ctx context.Context) ([]Peer, error) {
	var out []Peer
	return out, c.do(ctx, "GET", "/api/peers", nil, &out)
}

func (c *Client) DeletePeer(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/api/peers/"+id, nil, nil)
}

// --- DNS Zones ---

type Zone struct {
	ID          string `json:"id,omitempty"`
	Domain      string `json:"domain"`
	Description string `json:"description,omitempty"`
}

type ZoneRequest struct {
	Domain      string `json:"domain"`
	Description string `json:"description,omitempty"`
}

func (c *Client) ListZones(ctx context.Context) ([]Zone, error) {
	var out []Zone
	return out, c.do(ctx, "GET", "/api/dns/zones", nil, &out)
}

func (c *Client) CreateZone(ctx context.Context, req ZoneRequest) (Zone, error) {
	var out Zone
	return out, c.do(ctx, "POST", "/api/dns/zones", req, &out)
}

func (c *Client) DeleteZone(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/api/dns/zones/"+id, nil, nil)
}

// --- DNS Records ---

type DNSRecord struct {
	ID      string `json:"id,omitempty"`
	Name    string `json:"name"`
	Type    string `json:"type"` // "A"
	RData   string `json:"rdata"`
	TTL     int    `json:"ttl,omitempty"`
	Enabled bool   `json:"enabled"`
}

type DNSRecordRequest struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	RData   string `json:"rdata"`
	TTL     int    `json:"ttl,omitempty"`
	Enabled bool   `json:"enabled"`
}

func (c *Client) ListRecords(ctx context.Context, zoneID string) ([]DNSRecord, error) {
	var out []DNSRecord
	return out, c.do(ctx, "GET", "/api/dns/zones/"+zoneID+"/records", nil, &out)
}

func (c *Client) CreateRecord(ctx context.Context, zoneID string, req DNSRecordRequest) (DNSRecord, error) {
	var out DNSRecord
	return out, c.do(ctx, "POST", "/api/dns/zones/"+zoneID+"/records", req, &out)
}

func (c *Client) DeleteRecord(ctx context.Context, zoneID, recordID string) error {
	return c.do(ctx, "DELETE", "/api/dns/zones/"+zoneID+"/records/"+recordID, nil, nil)
}

// Bootstrap calls POST /api/setup on a fresh NetBird deployment and returns
// a management PAT. This is used by the initial docker-compose bootstrap script.
func (c *Client) Bootstrap(ctx context.Context) (string, error) {
	var out struct {
		Access struct {
			Token string `json:"token"`
		} `json:"access"`
	}
	if err := c.do(ctx, "POST", "/api/setup", map[string]any{}, &out); err != nil {
		return "", err
	}
	return out.Access.Token, nil
}
