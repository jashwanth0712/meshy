package netbird

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

const meshyPrefix = "meshy:"

// OrgSnapshot is the slice of one org's ENS state that the mesh needs.
// All names are full ENS names (e.g. "db1.prod.acme.eth").
type OrgSnapshot struct {
	Org       string
	Resources []string    // live devices (each runs meshyd + meshy-mesh serve)
	Agents    []string    // live agent identities (claude-1.agents.acme.eth)
	Grants    []GrantSnap // live grants only
}

// GrantSnap is one live grant. Resource is always the *device* the agent
// reaches: for a service grant (claude-1.camera.pi.home.acme.eth) it is the
// service's parent device, because services are reached through meshyd's
// SSH direct-tcpip on that device.
type GrantSnap struct {
	Name     string // claude-1.db1.prod.acme.eth
	Agent    string // claude-1.agents.acme.eth
	Resource string // db1.prod.acme.eth
}

// Reconciler keeps NetBird in sync with ENS. ENS is the source of truth:
//
//   - every resource and agent gets a group "meshy:res:<name>" / "meshy:agent:<name>"
//     whose only member is the peer named PeerName(<name>)
//   - every live grant becomes one policy "meshy:grant:<grant>" allowing
//     tcp/22 from the agent group to the resource group
//   - NetBird's allow-all "Default" policy is deleted (deny by default)
//   - peers whose ENS name no longer exists (revoked device or agent) are deleted
//
// It is idempotent and only touches objects it owns (meshy: prefix, *-eth peers).
type Reconciler struct {
	Client *Client
	Log    *slog.Logger
	// KeepDefaultPolicy leaves NetBird's allow-all policy in place (tests/dev).
	KeepDefaultPolicy bool

	mu      sync.Mutex
	lastRun time.Time
	missing map[string]int // peer name -> consecutive passes absent from ENS
	// Stats from the last run, for /v1/health.
	Last SyncStats
}

// SyncStats summarises one reconcile pass.
type SyncStats struct {
	At              time.Time `json:"at"`
	Org             string    `json:"org"`
	Groups          int       `json:"groups"`
	Policies        int       `json:"policies"`
	PeersMatched    int       `json:"peersMatched"`
	PeersEvicted    int       `json:"peersEvicted"`
	PoliciesAdded   int       `json:"policiesAdded"`
	PoliciesRemoved int       `json:"policiesRemoved"`
	Err             string    `json:"err,omitempty"`
}

func (r *Reconciler) log() *slog.Logger {
	if r.Log == nil {
		return slog.Default()
	}
	return r.Log
}

// ResGroup / AgentGroup are the NetBird group names for an ENS name.
func ResGroup(name string) string    { return meshyPrefix + "res:" + name }
func AgentGroup(name string) string  { return meshyPrefix + "agent:" + name }
func grantPolicy(name string) string { return meshyPrefix + "grant:" + name }

// Sync reconciles snap into NetBird.
func (r *Reconciler) Sync(ctx context.Context, snap OrgSnapshot) (err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := SyncStats{At: time.Now(), Org: snap.Org}
	defer func() {
		if err != nil {
			st.Err = err.Error()
		}
		r.Last = st
		r.lastRun = time.Now()
	}()

	groups, err := r.Client.ListGroups(ctx)
	if err != nil {
		return fmt.Errorf("reconciler: list groups: %w", err)
	}
	policies, err := r.Client.ListPolicies(ctx)
	if err != nil {
		return fmt.Errorf("reconciler: list policies: %w", err)
	}
	peers, err := r.Client.ListPeers(ctx)
	if err != nil {
		return fmt.Errorf("reconciler: list peers: %w", err)
	}

	orgSuffix := PeerName(snap.Org) // "acme-eth"
	peerByName := map[string]Peer{}
	for _, p := range peers {
		peerByName[p.Name] = p
	}

	// 1. Desired groups and their single member.
	desired := map[string]string{} // group name -> ENS name
	for _, n := range snap.Resources {
		desired[ResGroup(n)] = n
	}
	for _, n := range snap.Agents {
		desired[AgentGroup(n)] = n
	}
	byName := map[string]Group{}
	for _, g := range groups {
		if strings.HasPrefix(g.Name, meshyPrefix) {
			byName[g.Name] = g
		}
	}
	for gname, ens := range desired {
		var members []string
		if p, ok := peerByName[PeerName(ens)]; ok {
			members = []string{p.ID}
			st.PeersMatched++
		}
		g, exists := byName[gname]
		switch {
		case !exists:
			ng, err := r.Client.CreateGroup(ctx, GroupRequest{Name: gname, Peers: members})
			if err != nil {
				r.log().Warn("reconciler: create group", "name", gname, "err", err)
				continue
			}
			byName[gname] = ng
		case !sameMembers(g.Peers, members):
			ng, err := r.Client.UpdateGroup(ctx, g.ID, GroupRequest{Name: gname, Peers: members})
			if err != nil {
				r.log().Warn("reconciler: update group", "name", gname, "err", err)
				continue
			}
			ng.Peers = toMin(members)
			byName[gname] = ng
		}
	}

	// 2. Desired policies: one per live grant.
	want := map[string]PolicyCreate{}
	for _, g := range snap.Grants {
		ag, ok1 := byName[AgentGroup(g.Agent)]
		rg, ok2 := byName[ResGroup(g.Resource)]
		if !ok1 || !ok2 {
			continue
		}
		pn := grantPolicy(g.Name)
		want[pn] = PolicyCreate{
			Name:    pn,
			Enabled: true,
			Rules: []PolicyRule{{
				Name: pn, Enabled: true, Bidirectional: false,
				Protocol: "tcp", Action: "accept", Ports: []string{"22"},
				Sources: []string{ag.ID}, Destinations: []string{rg.ID},
			}},
		}
	}
	have := map[string]Policy{}
	for _, p := range policies {
		switch {
		case strings.HasPrefix(p.Name, meshyPrefix):
			have[p.Name] = p
		case p.Name == "Default" && !r.KeepDefaultPolicy:
			if err := r.Client.DeletePolicy(ctx, p.ID); err != nil {
				r.log().Warn("reconciler: delete Default policy", "err", err)
			} else {
				r.log().Info("reconciler: removed allow-all Default policy (deny by default)")
			}
		}
	}
	for pn, req := range want {
		if _, ok := have[pn]; ok {
			continue
		}
		if _, err := r.Client.CreatePolicy(ctx, req); err != nil {
			r.log().Warn("reconciler: create policy", "name", pn, "err", err)
			continue
		}
		st.PoliciesAdded++
		r.log().Info("reconciler: grant -> policy", "grant", strings.TrimPrefix(pn, meshyPrefix+"grant:"))
	}
	// Only remove policies belonging to this org.
	for pn, p := range have {
		if _, ok := want[pn]; ok || !strings.HasSuffix(pn, "."+snap.Org) {
			continue
		}
		if err := r.Client.DeletePolicy(ctx, p.ID); err != nil {
			r.log().Warn("reconciler: delete policy", "name", pn, "err", err)
			continue
		}
		st.PoliciesRemoved++
		r.log().Info("reconciler: grant gone -> policy removed", "grant", strings.TrimPrefix(pn, meshyPrefix+"grant:"))
	}

	// 3. Remove this org's groups that are no longer desired (after policies,
	// which reference them).
	for gname, g := range byName {
		if _, ok := desired[gname]; ok || !strings.HasSuffix(gname, "."+snap.Org) {
			continue
		}
		if err := r.Client.DeleteGroup(ctx, g.ID); err != nil {
			r.log().Warn("reconciler: delete group", "name", gname, "err", err)
		}
	}

	// 4. Evict peers of this org whose ENS name is gone. A name must be absent
	// in two consecutive passes, so one flaky RPC read never evicts a device.
	live := map[string]bool{}
	for _, n := range desired {
		live[PeerName(n)] = true
	}
	if r.missing == nil {
		r.missing = map[string]int{}
	}
	for _, p := range peers {
		if !strings.HasSuffix(p.Name, "-"+orgSuffix) || live[p.Name] {
			delete(r.missing, p.Name)
			continue
		}
		if r.missing[p.Name]++; r.missing[p.Name] < 2 {
			continue
		}
		delete(r.missing, p.Name)
		if err := r.Client.DeletePeer(ctx, p.ID); err != nil {
			r.log().Warn("reconciler: evict peer", "peer", p.Name, "err", err)
			continue
		}
		st.PeersEvicted++
		r.log().Info("reconciler: ENS name gone -> peer evicted", "peer", p.Name)
	}

	st.Groups = len(desired)
	st.Policies = len(want)
	return nil
}

func sameMembers(have []PeerMinimum, want []string) bool {
	if len(have) != len(want) {
		return false
	}
	a := make([]string, len(have))
	for i, p := range have {
		a[i] = p.ID
	}
	b := append([]string(nil), want...)
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func toMin(ids []string) []PeerMinimum {
	out := make([]PeerMinimum, len(ids))
	for i, id := range ids {
		out[i] = PeerMinimum{ID: id}
	}
	return out
}

// PeerName returns the NetBird peer name for an ENS name (dots -> dashes).
// meshy-mesh registers each device/agent with exactly this name, and
// resolves ENS names to mesh IPs through it.
func PeerName(ens string) string {
	return strings.ReplaceAll(strings.ToLower(ens), ".", "-")
}
