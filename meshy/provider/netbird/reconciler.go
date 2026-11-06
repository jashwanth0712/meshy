package netbird

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

const meshyPrefix = "meshy:"

// OrgSnapshot is the snapshot of one org's ENS state that the reconciler
// syncs into NetBird. The provider package fills this and passes it to Sync.
type OrgSnapshot struct {
	Org      string // e.g. "acme.eth"
	Networks []NetworkSnap
	Agents   []AgentSnap
	Grants   []GrantSnap  // live grants only
	Peers    []PeerSnap   // resource peers known to NetBird
}

type NetworkSnap struct {
	Name string // e.g. "prod"
}

type AgentSnap struct {
	Label string // e.g. "claude-1"
}

type GrantSnap struct {
	AgentLabel    string // e.g. "claude-1"
	ResourceLabel string // e.g. "db1"
	NetworkLabel  string // e.g. "prod"
}

// PeerSnap maps a resource ENS name to its NetBird peer IP.
type PeerSnap struct {
	ResourceName string // e.g. "db1.prod.acme.eth" (dots replaced with dashes in NB peer name)
	PeerName     string // e.g. "db1-prod-acme-eth"
	IP           string
}

// Reconciler keeps NetBird in sync with ENS grant state.
// It is idempotent: all objects it creates use the meshy: name prefix and are
// diffed on every Sync call.
type Reconciler struct {
	Client *Client
	Log    *slog.Logger

	mu      sync.Mutex
	lastRun time.Time
}

func (r *Reconciler) log() *slog.Logger {
	if r.Log == nil {
		return slog.Default()
	}
	return r.Log
}

// Sync reconciles the given snapshot into NetBird.
func (r *Reconciler) Sync(ctx context.Context, snap OrgSnapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()

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
	zones, err := r.Client.ListZones(ctx)
	if err != nil {
		return fmt.Errorf("reconciler: list zones: %w", err)
	}

	// Index existing meshy-managed objects.
	groupByName := map[string]Group{}
	for _, g := range groups {
		if strings.HasPrefix(g.Name, meshyPrefix) {
			groupByName[g.Name] = g
		}
	}
	policyByName := map[string]Policy{}
	for _, p := range policies {
		if strings.HasPrefix(p.Name, meshyPrefix) {
			policyByName[p.Name] = p
		}
	}
	zoneByDomain := map[string]Zone{}
	for _, z := range zones {
		if strings.HasPrefix(z.Domain, meshyPrefix) {
			zoneByDomain[z.Domain] = z
		}
	}
	// Peer lookup: name (dots→dashes) → Peer
	peerByName := map[string]Peer{}
	for _, p := range peers {
		peerByName[p.Name] = p
	}

	// Desired group names.
	desiredGroups := map[string]GroupRequest{}

	// Per-network group.
	for _, net := range snap.Networks {
		name := meshyPrefix + "net:" + net.Name + "." + snap.Org
		desiredGroups[name] = GroupRequest{Name: name}
	}
	// Per-agent group.
	for _, ag := range snap.Agents {
		name := meshyPrefix + "agent:" + ag.Label + ".agents." + snap.Org
		desiredGroups[name] = GroupRequest{Name: name}
	}
	// Per-resource group.
	resourceGroups := map[string]string{} // resource full name → group name
	for _, net := range snap.Networks {
		for _, g := range snap.Grants {
			if g.NetworkLabel != net.Name {
				continue
			}
			resName := g.ResourceLabel + "." + g.NetworkLabel + "." + snap.Org
			resGroupName := meshyPrefix + "res:" + resName
			if _, ok := resourceGroups[resName]; !ok {
				resourceGroups[resName] = resGroupName
				desiredGroups[resGroupName] = GroupRequest{Name: resGroupName}
			}
		}
	}

	// Ensure all desired groups exist; create if missing.
	for name, req := range desiredGroups {
		if _, exists := groupByName[name]; !exists {
			g, err := r.Client.CreateGroup(ctx, req)
			if err != nil {
				r.log().Warn("reconciler: create group", "name", name, "err", err)
				continue
			}
			groupByName[name] = g
			r.log().Info("reconciler: created group", "name", name)
		}
	}

	// Remove stale groups (no longer desired).
	for name, g := range groupByName {
		if _, desired := desiredGroups[name]; !desired {
			if err := r.Client.DeleteGroup(ctx, g.ID); err != nil {
				r.log().Warn("reconciler: delete group", "name", name, "err", err)
			} else {
				r.log().Info("reconciler: deleted stale group", "name", name)
			}
		}
	}

	// Desired policies: one per live grant.
	desiredPolicies := map[string]PolicyCreate{}
	for _, grant := range snap.Grants {
		agentGroupName := meshyPrefix + "agent:" + grant.AgentLabel + ".agents." + snap.Org
		resName := grant.ResourceLabel + "." + grant.NetworkLabel + "." + snap.Org
		resGroupName := meshyPrefix + "res:" + resName
		agentGroup, hasAgent := groupByName[agentGroupName]
		resGroup, hasRes := groupByName[resGroupName]
		if !hasAgent || !hasRes {
			continue
		}
		polName := meshyPrefix + "grant:" + grant.AgentLabel + "." + resName
		desiredPolicies[polName] = PolicyCreate{
			Name:    polName,
			Enabled: true,
			Rules: []PolicyRule{{
				Name:          polName + ":ssh",
				Enabled:       true,
				Bidirectional: false,
				Protocol:      "tcp",
				Action:        "accept",
				Ports:         []string{"22"},
				Sources:       []string{agentGroup.ID},
				Destinations:  []string{resGroup.ID},
			}},
		}
	}

	// Ensure all desired policies exist.
	for name, req := range desiredPolicies {
		if _, exists := policyByName[name]; !exists {
			p, err := r.Client.CreatePolicy(ctx, req)
			if err != nil {
				r.log().Warn("reconciler: create policy", "name", name, "err", err)
				continue
			}
			policyByName[name] = p
			r.log().Info("reconciler: created policy", "name", name)
		}
	}

	// Remove stale policies.
	for name, p := range policyByName {
		if _, desired := desiredPolicies[name]; !desired {
			if err := r.Client.DeletePolicy(ctx, p.ID); err != nil {
				r.log().Warn("reconciler: delete policy", "name", name, "err", err)
			} else {
				r.log().Info("reconciler: deleted stale policy", "name", name)
			}
		}
	}

	// DNS zones and records: one zone per network, A records per resource.
	for _, net := range snap.Networks {
		zoneDomain := meshyPrefix + net.Name + "." + snap.Org + "."
		zone, exists := zoneByDomain[zoneDomain]
		if !exists {
			z, err := r.Client.CreateZone(ctx, ZoneRequest{
				Domain:      zoneDomain,
				Description: "meshy: " + net.Name + "." + snap.Org,
			})
			if err != nil {
				r.log().Warn("reconciler: create zone", "domain", zoneDomain, "err", err)
				continue
			}
			zone = z
			zoneByDomain[zoneDomain] = zone
			r.log().Info("reconciler: created zone", "domain", zoneDomain)
		}

		existingRecs, err := r.Client.ListRecords(ctx, zone.ID)
		if err != nil {
			r.log().Warn("reconciler: list records", "zone", zoneDomain, "err", err)
			continue
		}
		recByName := map[string]DNSRecord{}
		for _, rec := range existingRecs {
			recByName[rec.Name] = rec
		}

		// Add A records for resources whose peer IP is known.
		for _, ps := range snap.Peers {
			// ps.ResourceName e.g. "db1.prod.acme.eth"
			parts := strings.SplitN(ps.ResourceName, ".", 2)
			if len(parts) < 1 {
				continue
			}
			resLabel := parts[0]
			resNet := ""
			if len(parts) > 1 {
				resNetAndOrg := parts[1]
				netAndOrgParts := strings.SplitN(resNetAndOrg, ".", 2)
				if len(netAndOrgParts) > 0 {
					resNet = netAndOrgParts[0]
				}
			}
			if resNet != net.Name {
				continue
			}
			recName := resLabel + "." + net.Name + "." + snap.Org + "."
			if _, ok := recByName[recName]; !ok && ps.IP != "" {
				rec, err := r.Client.CreateRecord(ctx, zone.ID, DNSRecordRequest{
					Name:    recName,
					Type:    "A",
					RData:   ps.IP,
					TTL:     60,
					Enabled: true,
				})
				if err != nil {
					r.log().Warn("reconciler: create DNS record", "name", recName, "err", err)
				} else {
					recByName[recName] = rec
					r.log().Info("reconciler: created DNS record", "name", recName, "ip", ps.IP)
				}
			}
		}
	}

	// Build PeerSnap lookup for peer matching by resource name.
	_ = peerByName // used if we need to match peers for group membership

	r.lastRun = time.Now()
	return nil
}

// PeerName returns the NetBird peer name for a resource ENS name
// (dots replaced with dashes).
func PeerName(resourceENS string) string {
	return strings.ReplaceAll(resourceENS, ".", "-")
}
