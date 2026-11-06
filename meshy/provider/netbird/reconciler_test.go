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

// fakeNetBird is an in-memory NetBird management API stub.
type fakeNetBird struct {
	mu       sync.Mutex
	groups   map[string]netbird.Group
	policies map[string]netbird.Policy
	zones    map[string]netbird.Zone
	records  map[string]map[string]netbird.DNSRecord // zoneID → recordID → record
	peers    []netbird.Peer
	idSeq    int
}

func newFakeNetBird() *fakeNetBird {
	return &fakeNetBird{
		groups:   map[string]netbird.Group{},
		policies: map[string]netbird.Policy{},
		zones:    map[string]netbird.Zone{},
		records:  map[string]map[string]netbird.DNSRecord{},
	}
}

func (f *fakeNetBird) nextID() string {
	f.idSeq++
	return fmt.Sprintf("id%d", f.idSeq)
}

func (f *fakeNetBird) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/groups", f.handleGroups)
	mux.HandleFunc("/api/groups/", f.handleGroup)
	mux.HandleFunc("/api/policies", f.handlePolicies)
	mux.HandleFunc("/api/policies/", f.handlePolicy)
	mux.HandleFunc("/api/setup-keys", f.handleSetupKeys)
	mux.HandleFunc("/api/peers", f.handlePeers)
	mux.HandleFunc("/api/dns/zones", f.handleZones)
	mux.HandleFunc("/api/dns/zones/", f.handleZoneOrRecords)
	return mux
}

func jsonDecode(r *http.Request, out any) error {
	return json.NewDecoder(r.Body).Decode(out)
}

func jsonWrite(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (f *fakeNetBird) handleGroups(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method == "GET" {
		out := make([]netbird.Group, 0, len(f.groups))
		for _, g := range f.groups {
			out = append(out, g)
		}
		jsonWrite(w, out)
		return
	}
	if r.Method == "POST" {
		var req netbird.GroupRequest
		jsonDecode(r, &req)
		id := f.nextID()
		g := netbird.Group{ID: id, Name: req.Name}
		f.groups[id] = g
		jsonWrite(w, g)
		return
	}
	http.NotFound(w, r)
}

func (f *fakeNetBird) handleGroup(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/groups/")
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method == "DELETE" {
		delete(f.groups, id)
		w.WriteHeader(200)
		return
	}
	if r.Method == "PUT" {
		var req netbird.GroupRequest
		jsonDecode(r, &req)
		g := netbird.Group{ID: id, Name: req.Name}
		f.groups[id] = g
		jsonWrite(w, g)
		return
	}
	http.NotFound(w, r)
}

func (f *fakeNetBird) handlePolicies(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method == "GET" {
		out := make([]netbird.Policy, 0, len(f.policies))
		for _, p := range f.policies {
			out = append(out, p)
		}
		jsonWrite(w, out)
		return
	}
	if r.Method == "POST" {
		var req netbird.PolicyCreate
		jsonDecode(r, &req)
		id := f.nextID()
		p := netbird.Policy{ID: id, Name: req.Name, Enabled: req.Enabled}
		f.policies[id] = p
		jsonWrite(w, p)
		return
	}
	http.NotFound(w, r)
}

func (f *fakeNetBird) handlePolicy(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/policies/")
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method == "DELETE" {
		delete(f.policies, id)
		w.WriteHeader(200)
		return
	}
	http.NotFound(w, r)
}

func (f *fakeNetBird) handleSetupKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var req netbird.CreateSetupKeyRequest
		jsonDecode(r, &req)
		jsonWrite(w, netbird.SetupKeyClear{
			ID: "sk1", Key: "FAKE-SETUP-KEY", Name: req.Name,
			Type: req.Type, Valid: true, AutoGroups: req.AutoGroups,
		})
		return
	}
	http.NotFound(w, r)
}

func (f *fakeNetBird) handlePeers(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method == "GET" {
		jsonWrite(w, f.peers)
		return
	}
	http.NotFound(w, r)
}

func (f *fakeNetBird) handleZones(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method == "GET" {
		out := make([]netbird.Zone, 0, len(f.zones))
		for _, z := range f.zones {
			out = append(out, z)
		}
		jsonWrite(w, out)
		return
	}
	if r.Method == "POST" {
		var req netbird.ZoneRequest
		jsonDecode(r, &req)
		id := f.nextID()
		z := netbird.Zone{ID: id, Domain: req.Domain}
		f.zones[id] = z
		f.records[id] = map[string]netbird.DNSRecord{}
		jsonWrite(w, z)
		return
	}
	http.NotFound(w, r)
}

func (f *fakeNetBird) handleZoneOrRecords(w http.ResponseWriter, r *http.Request) {
	// /api/dns/zones/{id}  or  /api/dns/zones/{id}/records[/{rid}]
	path := strings.TrimPrefix(r.URL.Path, "/api/dns/zones/")
	parts := strings.SplitN(path, "/", 3)
	zoneID := parts[0]

	if len(parts) == 1 {
		// Zone CRUD.
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Method == "DELETE" {
			delete(f.zones, zoneID)
			delete(f.records, zoneID)
			w.WriteHeader(200)
			return
		}
		http.NotFound(w, r)
		return
	}

	if len(parts) >= 2 && parts[1] == "records" {
		if len(parts) == 2 {
			f.mu.Lock()
			defer f.mu.Unlock()
			if r.Method == "GET" {
				recs := f.records[zoneID]
				out := make([]netbird.DNSRecord, 0, len(recs))
				for _, rec := range recs {
					out = append(out, rec)
				}
				jsonWrite(w, out)
				return
			}
			if r.Method == "POST" {
				var req netbird.DNSRecordRequest
				jsonDecode(r, &req)
				rid := f.nextID()
				rec := netbird.DNSRecord{ID: rid, Name: req.Name, Type: req.Type, RData: req.RData, Enabled: req.Enabled}
				if f.records[zoneID] == nil {
					f.records[zoneID] = map[string]netbird.DNSRecord{}
				}
				f.records[zoneID][rid] = rec
				jsonWrite(w, rec)
				return
			}
		}
		if len(parts) == 3 {
			rid := parts[2]
			f.mu.Lock()
			defer f.mu.Unlock()
			if r.Method == "DELETE" {
				if recs, ok := f.records[zoneID]; ok {
					delete(recs, rid)
				}
				w.WriteHeader(200)
				return
			}
		}
	}
	http.NotFound(w, r)
}

func TestReconcilerCreateObjects(t *testing.T) {
	fake := newFakeNetBird()
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	client := netbird.NewClient(srv.URL, "test-token")
	rec := &netbird.Reconciler{Client: client}

	snap := netbird.OrgSnapshot{
		Org:      "acme.eth",
		Networks: []netbird.NetworkSnap{{Name: "prod"}},
		Agents:   []netbird.AgentSnap{{Label: "claude-1"}},
		Grants: []netbird.GrantSnap{{
			AgentLabel: "claude-1", ResourceLabel: "db1", NetworkLabel: "prod",
		}},
	}

	ctx := context.Background()
	if err := rec.Sync(ctx, snap); err != nil {
		t.Fatal("sync:", err)
	}

	// Verify groups were created.
	fake.mu.Lock()
	defer fake.mu.Unlock()

	var groupNames []string
	for _, g := range fake.groups {
		groupNames = append(groupNames, g.Name)
	}
	t.Logf("groups: %v", groupNames)

	hasGroup := func(name string) bool {
		for _, g := range fake.groups {
			if g.Name == name {
				return true
			}
		}
		return false
	}
	if !hasGroup("meshy:net:prod.acme.eth") {
		t.Error("missing network group")
	}
	if !hasGroup("meshy:agent:claude-1.agents.acme.eth") {
		t.Error("missing agent group")
	}
	if !hasGroup("meshy:res:db1.prod.acme.eth") {
		t.Error("missing resource group")
	}

	// Verify policy was created.
	if len(fake.policies) != 1 {
		t.Fatalf("expected 1 policy, got %d", len(fake.policies))
	}
	for _, p := range fake.policies {
		if !strings.HasPrefix(p.Name, "meshy:grant:claude-1.db1.prod.acme.eth") {
			t.Errorf("unexpected policy name: %s", p.Name)
		}
	}
}

// TestReconcilerIdempotent verifies syncing twice doesn't create duplicates.
func TestReconcilerIdempotent(t *testing.T) {
	fake := newFakeNetBird()
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	client := netbird.NewClient(srv.URL, "test-token")
	rec := &netbird.Reconciler{Client: client}

	snap := netbird.OrgSnapshot{
		Org:      "acme.eth",
		Networks: []netbird.NetworkSnap{{Name: "prod"}},
		Agents:   []netbird.AgentSnap{{Label: "claude-1"}},
		Grants:   []netbird.GrantSnap{{AgentLabel: "claude-1", ResourceLabel: "db1", NetworkLabel: "prod"}},
	}

	ctx := context.Background()
	if err := rec.Sync(ctx, snap); err != nil {
		t.Fatal("sync 1:", err)
	}
	groupCount1 := len(fake.groups)
	policyCount1 := len(fake.policies)

	if err := rec.Sync(ctx, snap); err != nil {
		t.Fatal("sync 2:", err)
	}
	if len(fake.groups) != groupCount1 {
		t.Errorf("group count changed on second sync: %d → %d", groupCount1, len(fake.groups))
	}
	if len(fake.policies) != policyCount1 {
		t.Errorf("policy count changed on second sync: %d → %d", policyCount1, len(fake.policies))
	}
}

// TestReconcilerRemoveStalePolicy verifies that removing a grant removes its policy.
func TestReconcilerRemoveStalePolicy(t *testing.T) {
	fake := newFakeNetBird()
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	client := netbird.NewClient(srv.URL, "test-token")
	rec := &netbird.Reconciler{Client: client}

	ctx := context.Background()

	// First sync with a grant.
	snapWith := netbird.OrgSnapshot{
		Org:      "acme.eth",
		Networks: []netbird.NetworkSnap{{Name: "prod"}},
		Agents:   []netbird.AgentSnap{{Label: "claude-1"}},
		Grants:   []netbird.GrantSnap{{AgentLabel: "claude-1", ResourceLabel: "db1", NetworkLabel: "prod"}},
	}
	if err := rec.Sync(ctx, snapWith); err != nil {
		t.Fatal(err)
	}
	if len(fake.policies) != 1 {
		t.Fatalf("expected 1 policy after first sync, got %d", len(fake.policies))
	}

	// Second sync without the grant (revoked).
	snapWithout := netbird.OrgSnapshot{
		Org:      "acme.eth",
		Networks: []netbird.NetworkSnap{{Name: "prod"}},
		Agents:   []netbird.AgentSnap{{Label: "claude-1"}},
		Grants:   []netbird.GrantSnap{}, // no grants
	}
	if err := rec.Sync(ctx, snapWithout); err != nil {
		t.Fatal(err)
	}
	if len(fake.policies) != 0 {
		t.Fatalf("expected 0 policies after revocation sync, got %d", len(fake.policies))
	}
}
