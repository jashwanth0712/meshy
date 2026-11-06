package netbird_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jashwanth0712/meshy/meshy/provider/netbird"
)

// fakeNetBird is an in-memory stub of the NetBird management REST API.
type fakeNetBird struct {
	mu       sync.Mutex
	groups   map[string]netbird.Group
	policies map[string]netbird.Policy
	peers    map[string]netbird.Peer
	keys     []netbird.CreateSetupKeyRequest
	seq      int
}

func newFake() *fakeNetBird {
	return &fakeNetBird{groups: map[string]netbird.Group{}, policies: map[string]netbird.Policy{}, peers: map[string]netbird.Peer{}}
}

func (f *fakeNetBird) id() string { f.seq++; return fmt.Sprintf("id%d", f.seq) }

func (f *fakeNetBird) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/"), "/")
	res, id := parts[0], ""
	if len(parts) > 1 {
		id = parts[1]
	}
	enc := json.NewEncoder(w)
	switch {
	case res == "groups" && r.Method == "GET":
		out := []netbird.Group{}
		for _, g := range f.groups {
			out = append(out, g)
		}
		enc.Encode(out)
	case res == "groups" && (r.Method == "POST" || r.Method == "PUT"):
		var req netbird.GroupRequest
		json.NewDecoder(r.Body).Decode(&req)
		if id == "" {
			id = f.id()
		}
		g := netbird.Group{ID: id, Name: req.Name}
		for _, p := range req.Peers {
			g.Peers = append(g.Peers, netbird.PeerMinimum{ID: p})
		}
		f.groups[id] = g
		enc.Encode(g)
	case res == "groups" && r.Method == "DELETE":
		delete(f.groups, id)
	case res == "policies" && r.Method == "GET":
		out := []netbird.Policy{}
		for _, p := range f.policies {
			out = append(out, p)
		}
		enc.Encode(out)
	case res == "policies" && r.Method == "POST":
		var req netbird.PolicyCreate
		json.NewDecoder(r.Body).Decode(&req)
		p := netbird.Policy{ID: f.id(), Name: req.Name, Enabled: req.Enabled, Rules: req.Rules}
		f.policies[p.ID] = p
		enc.Encode(p)
	case res == "policies" && r.Method == "DELETE":
		delete(f.policies, id)
	case res == "peers" && r.Method == "GET":
		out := []netbird.Peer{}
		for _, p := range f.peers {
			out = append(out, p)
		}
		enc.Encode(out)
	case res == "peers" && r.Method == "DELETE":
		delete(f.peers, id)
	case res == "setup-keys" && r.Method == "POST":
		var req netbird.CreateSetupKeyRequest
		json.NewDecoder(r.Body).Decode(&req)
		f.keys = append(f.keys, req)
		enc.Encode(netbird.SetupKeyClear{ID: f.id(), Key: "KEY-" + req.Name, AutoGroups: req.AutoGroups})
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeNetBird) policyNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, p := range f.policies {
		out = append(out, p.Name)
	}
	return out
}

func (f *fakeNetBird) groupByName(n string) (netbird.Group, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, g := range f.groups {
		if g.Name == n {
			return g, true
		}
	}
	return netbird.Group{}, false
}

const org = "acme.eth"

func snap(grants ...netbird.GrantSnap) netbird.OrgSnapshot {
	return netbird.OrgSnapshot{
		Org:       org,
		Resources: []string{"db1.prod.acme.eth", "pi.home.acme.eth"},
		Agents:    []string{"claude-1.agents.acme.eth"},
		Grants:    grants,
	}
}

var g1 = netbird.GrantSnap{Name: "claude-1.db1.prod.acme.eth", Agent: "claude-1.agents.acme.eth", Resource: "db1.prod.acme.eth"}
var g2 = netbird.GrantSnap{Name: "claude-1.camera.pi.home.acme.eth", Agent: "claude-1.agents.acme.eth", Resource: "pi.home.acme.eth"}

func setup(t *testing.T) (*fakeNetBird, *netbird.Reconciler) {
	f := newFake()
	f.policies["d"] = netbird.Policy{ID: "d", Name: "Default", Enabled: true}
	f.peers["p1"] = netbird.Peer{ID: "p1", Name: "db1-prod-acme-eth", IP: "100.64.0.1"}
	f.peers["p2"] = netbird.Peer{ID: "p2", Name: "claude-1-agents-acme-eth", IP: "100.64.0.2"}
	f.peers["p3"] = netbird.Peer{ID: "p3", Name: "laptop", IP: "100.64.0.3"} // not meshy-managed
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, &netbird.Reconciler{Client: netbird.NewClient(srv.URL, "t")}
}

func TestSyncGroupsPoliciesDenyByDefault(t *testing.T) {
	f, r := setup(t)
	if err := r.Sync(context.Background(), snap(g1, g2)); err != nil {
		t.Fatal(err)
	}
	names := f.policyNames()
	for _, n := range names {
		if n == "Default" {
			t.Fatal("allow-all Default policy must be deleted")
		}
	}
	if len(names) != 2 {
		t.Fatalf("want 2 grant policies, got %v", names)
	}
	g, ok := f.groupByName(netbird.ResGroup("db1.prod.acme.eth"))
	if !ok || len(g.Peers) != 1 || g.Peers[0].ID != "p1" {
		t.Fatalf("res group should contain db1 peer: %+v", g)
	}
	a, _ := f.groupByName(netbird.AgentGroup("claude-1.agents.acme.eth"))
	if len(a.Peers) != 1 || a.Peers[0].ID != "p2" {
		t.Fatalf("agent group should contain agent peer: %+v", a)
	}
	// pi's group exists but is empty until its peer joins.
	pi, ok := f.groupByName(netbird.ResGroup("pi.home.acme.eth"))
	if !ok || len(pi.Peers) != 0 {
		t.Fatalf("pi group: %+v", pi)
	}
	// Rule shape: agent -> resource, tcp/22 only.
	for _, p := range f.policies {
		if p.Name == "meshy:grant:"+g1.Name {
			rl := p.Rules[0]
			if rl.Protocol != "tcp" || len(rl.Ports) != 1 || rl.Ports[0] != "22" || rl.Bidirectional {
				t.Fatalf("bad rule %+v", rl)
			}
			if rl.Sources[0] != a.ID || rl.Destinations[0] != g.ID {
				t.Fatalf("rule direction wrong %+v", rl)
			}
		}
	}
}

func TestSyncRevokeRemovesPolicyIdempotent(t *testing.T) {
	f, r := setup(t)
	ctx := context.Background()
	r.Sync(ctx, snap(g1, g2))
	r.Sync(ctx, snap(g1, g2)) // idempotent
	if n := len(f.policyNames()); n != 2 {
		t.Fatalf("idempotency: %d policies", n)
	}
	r.Sync(ctx, snap(g2)) // g1 revoked on ENS
	names := f.policyNames()
	if len(names) != 1 || names[0] != "meshy:grant:"+g2.Name {
		t.Fatalf("after revoke: %v", names)
	}
	if r.Last.PoliciesRemoved != 1 {
		t.Fatalf("stats: %+v", r.Last)
	}
}

func TestSyncEvictsPeerAfterTwoPasses(t *testing.T) {
	f, r := setup(t)
	ctx := context.Background()
	s := snap(g1)
	s.Agents = nil // agent identity unregistered on ENS
	r.Sync(ctx, s)
	if _, ok := f.peers["p2"]; !ok {
		t.Fatal("must not evict on the first missing pass")
	}
	r.Sync(ctx, s)
	if _, ok := f.peers["p2"]; ok {
		t.Fatal("agent peer should be evicted after two passes")
	}
	if _, ok := f.peers["p3"]; !ok {
		t.Fatal("non-meshy peer must never be touched")
	}
	if _, ok := f.peers["p1"]; !ok {
		t.Fatal("live device evicted")
	}
}

func TestGroupRefsDecodeBothShapes(t *testing.T) {
	var p netbird.Policy
	raw := `{"id":"x","name":"n","enabled":true,"rules":[{"name":"r","enabled":true,"bidirectional":false,"protocol":"tcp","action":"accept","sources":[{"id":"g1","name":"All"}],"destinations":["g2"]}]}`
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	if p.Rules[0].Sources[0] != "g1" || p.Rules[0].Destinations[0] != "g2" {
		t.Fatalf("%+v", p.Rules[0])
	}
}
