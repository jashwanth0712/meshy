package provider

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/jashwanth0712/meshy/meshy/enskit"
	"github.com/jashwanth0712/meshy/meshy/provider/netbird"
	"github.com/jashwanth0712/meshy/meshy/worldid"
)

// OrgMap is the JSON response for GET /v1/map.
type OrgMap struct {
	Org       string        `json:"org"`
	OrgKey    string        `json:"orgKey,omitempty"`
	Attestors []string      `json:"attestors,omitempty"`
	Provider  string        `json:"provider,omitempty"`
	Networks  []NetworkInfo `json:"networks"`
	Agents    []AgentInfo   `json:"agents"`
	// Members are people.<org> names (orgs created with MeshyOrgFactory).
	Members     []string    `json:"members,omitempty"`
	Grants      []GrantInfo `json:"grants"`
	GeneratedAt time.Time   `json:"generatedAt"`
	Block       uint64      `json:"block"`
}

type NetworkInfo struct {
	Name      string         `json:"name"`
	FullName  string         `json:"fullName"`
	Resources []ResourceInfo `json:"resources"`
}

type ResourceInfo struct {
	Name     string `json:"name"`
	FullName string `json:"fullName"`
	Addr     string `json:"addr,omitempty"`
	EncKey   string `json:"encKey,omitempty"`
	SSHHost  string `json:"sshHost,omitempty"`
	Roots    string `json:"roots,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Meta     string `json:"meta,omitempty"`
	// Services are children of the device that carry a meshy.service record.
	Services []ServiceInfo `json:"services,omitempty"`
}

// ServiceInfo is one port on a device, e.g. camera.pi.home.acme.eth.
type ServiceInfo struct {
	Name     string `json:"name"`
	FullName string `json:"fullName"`
	Device   string `json:"device"`
	Spec     string `json:"spec"` // raw meshy.service JSON {proto,target,desc}
}

type AgentInfo struct {
	Name          string `json:"name"`
	FullName      string `json:"fullName"`
	Addr          string `json:"addr,omitempty"`
	SSH           string `json:"ssh,omitempty"`
	Owner         string `json:"owner,omitempty"`
	WorldIDValid  bool   `json:"worldIdValid"`
	WorldIDRecord string `json:"worldIdRecord,omitempty"`
}

type GrantInfo struct {
	Name     string `json:"name"`
	FullName string `json:"fullName"`
	Owner    string `json:"owner,omitempty"`
	Expiry   uint64 `json:"expiry"`
	Status   string `json:"status"` // "live", "expired", "revoked"
	// Kind is "device" (folders on a device, sealed meshy.policy) or
	// "service" (one port on a device).
	Kind string `json:"kind"`
	// Agent is the grantee's identity name (<label>.agents.<org>).
	Agent string `json:"agent"`
	// Target is what the grant opens: the device or the service name.
	Target string `json:"target"`
	// Device is the device the agent connects to (the service's parent for
	// service grants). The mesh policy is agent -> Device, tcp/22.
	Device string `json:"device"`
}

// mapCache holds a cached OrgMap and when it was built.
type mapCache struct {
	mu      sync.Mutex
	entries map[string]*mapCacheEntry
}

type mapCacheEntry struct {
	m       *OrgMap
	builtAt time.Time
}

var globalMapCache = &mapCache{entries: map[string]*mapCacheEntry{}}

func (c *mapCache) get(org string) *OrgMap {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[org]
	if !ok || time.Since(e.builtAt) > 10*time.Second {
		return nil
	}
	return e.m
}

func (c *mapCache) set(org string, m *OrgMap) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[org] = &mapCacheEntry{m: m, builtAt: time.Now()}
}

// MapBuilder builds the OrgMap for an org by reading ENSv2 state.
type MapBuilder struct {
	ENS       *enskit.Client
	ETH       *ethclient.Client // for log filtering + headers
	FromBlock uint64
	Log       *slog.Logger
}

func (b *MapBuilder) log() *slog.Logger {
	if b.Log == nil {
		return slog.Default()
	}
	return b.Log
}

// Build reads the ENS tree for org and returns an OrgMap.
func (b *MapBuilder) Build(ctx context.Context, org string) (*OrgMap, error) {
	if m := globalMapCache.get(org); m != nil {
		return m, nil
	}
	m, err := b.build(ctx, org)
	if err != nil {
		return nil, err
	}
	globalMapCache.set(org, m)
	return m, nil
}

func (b *MapBuilder) build(ctx context.Context, org string) (*OrgMap, error) {
	orgName := enskit.Name(org)

	// Get current block.
	head, err := b.ETH.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("get head: %w", err)
	}
	block := head.Number.Uint64()

	// Look up the org.
	orgRec, err := b.ENS.Lookup(ctx, orgName)
	if err != nil {
		return nil, fmt.Errorf("lookup org %s: %w", org, err)
	}

	m := &OrgMap{
		Org:         org,
		GeneratedAt: time.Now(),
		Block:       block,
	}

	// Read org-level records.
	if orgRec.Resolver != (common.Address{}) {
		if v, err := orgRec.Text(ctx, b.ENS, "meshy.orgkey"); err == nil {
			m.OrgKey = v
		}
		if v, err := orgRec.Text(ctx, b.ENS, "meshy.attestor"); err == nil {
			for _, a := range strings.Split(v, ",") {
				if a = strings.TrimSpace(a); a != "" {
					m.Attestors = append(m.Attestors, a)
				}
			}
		}
		if v, err := orgRec.Text(ctx, b.ENS, "meshy.provider"); err == nil {
			m.Provider = v
		}
	}

	// The org's own subregistry (holds networks and "agents").
	// We cannot use State.Resource from the ETH registrar because it uses a
	// different resource-field encoding from ENSv2 PermissionedRegistries.
	// Instead, discover the orgSubreg via findParentRegistry of "agents.<org>",
	// which is always registered during org initialisation.
	orgSubreg, err := b.ENS.ParentRegistry(ctx, enskit.Name("agents."+org), nil)
	if err != nil || orgSubreg == (common.Address{}) {
		// Try other canonical children in case "agents" isn't set up yet.
		for _, probe := range []string{"prod", "staging", "dev", "test", "main"} {
			orgSubreg, err = b.ENS.ParentRegistry(ctx, enskit.Name(probe+"."+org), nil)
			if err == nil && orgSubreg != (common.Address{}) {
				break
			}
		}
	}
	if orgSubreg == (common.Address{}) {
		b.log().Warn("cannot find org subregistry", "org", org)
		return m, nil
	}

	// Enumerate children of the org's subregistry (labels + their subregistries).
	orgChildren, err := enskit.ChildSubregs(ctx, b.ETH, orgSubreg, b.FromBlock)
	if err != nil {
		return nil, fmt.Errorf("enumerate org children: %w", err)
	}

	// Find "agents" and networks.
	for child, childSubreg := range orgChildren {
		childName := enskit.Name(child + "." + org)
		if child == "people" {
			if childSubreg != (common.Address{}) {
				if ms, err := enskit.ChildSubregs(ctx, b.ETH, childSubreg, b.FromBlock); err == nil {
					for l := range ms {
						m.Members = append(m.Members, l+".people."+org)
					}
				}
			}
			continue
		}
		if child == "agents" {
			agents, err := b.buildAgents(ctx, childName, childSubreg, m.Attestors)
			if err != nil {
				b.log().Warn("build agents", "err", err)
			} else {
				m.Agents = agents
			}
			continue
		}
		// Treat everything else as a network.
		netInfo, grants, err := b.buildNetwork(ctx, childName, childSubreg)
		if err != nil {
			b.log().Warn("build network", "name", child, "err", err)
			continue
		}
		m.Networks = append(m.Networks, netInfo)
		m.Grants = append(m.Grants, grants...)
	}

	return m, nil
}

// buildNetwork builds a NetworkInfo from a known network name and its subregistry.
// netSubreg is the registry that holds the network's resources (zero means no children).
func (b *MapBuilder) buildNetwork(ctx context.Context, netName enskit.Name, netSubreg common.Address) (NetworkInfo, []GrantInfo, error) {
	netLabel := netName.Label()

	info := NetworkInfo{
		Name:     netLabel,
		FullName: string(netName),
	}

	if netSubreg == (common.Address{}) {
		return info, nil, nil
	}

	// Enumerate resources (labels + their grant subregistries).
	resChildren, err := enskit.ChildSubregs(ctx, b.ETH, netSubreg, b.FromBlock)
	if err != nil {
		return info, nil, fmt.Errorf("enumerate resources: %w", err)
	}

	var grants []GrantInfo
	for resLabel, resSubreg := range resChildren {
		resName := enskit.Name(resLabel + "." + string(netName))

		resRec, err := b.ENS.Lookup(ctx, resName)
		if err != nil {
			b.log().Warn("resource lookup", "name", resName, "err", err)
			continue
		}

		ri := ResourceInfo{
			Name:     resLabel,
			FullName: string(resName),
		}
		if resRec.Resolver != (common.Address{}) {
			if v, err := resRec.Text(ctx, b.ENS, "meshy.enckey"); err == nil {
				ri.EncKey = v
			}
			if v, err := resRec.Text(ctx, b.ENS, "meshy.ssh-host"); err == nil {
				ri.SSHHost = v
			}
			if v, err := resRec.Text(ctx, b.ENS, "meshy.roots"); err == nil {
				ri.Roots = v
			}
			if v, err := resRec.Text(ctx, b.ENS, "meshy.endpoint"); err == nil {
				ri.Endpoint = v
			}
			if v, err := resRec.Text(ctx, b.ENS, "meshy.kind"); err == nil {
				ri.Kind = v
			}
			if v, err := resRec.Text(ctx, b.ENS, "meshy.meta"); err == nil {
				ri.Meta = v
			}
			if a, err := b.ENS.Addr(ctx, resRec.Resolver, resName); err == nil && a != (common.Address{}) {
				ri.Addr = a.Hex()
			}
		}
		info.Resources = append(info.Resources, ri)

		// Children of a device are either services (they carry meshy.service)
		// or device grants. Children of a service are service grants.
		if resSubreg != (common.Address{}) {
			children, err := enskit.ChildSubregs(ctx, b.ETH, resSubreg, b.FromBlock)
			if err != nil {
				b.log().Warn("enumerate device children", "resource", resName, "err", err)
				continue
			}
			for label, childSubreg := range children {
				childName := enskit.Name(label + "." + string(resName))
				if spec := b.serviceSpec(ctx, childName); spec != "" {
					svc := ServiceInfo{Name: label, FullName: string(childName), Device: string(resName), Spec: spec}
					ri := &info.Resources[len(info.Resources)-1]
					ri.Services = append(ri.Services, svc)
					if childSubreg == (common.Address{}) {
						continue
					}
					sg, err := enskit.ChildSubregs(ctx, b.ETH, childSubreg, b.FromBlock)
					if err != nil {
						b.log().Warn("enumerate service grants", "service", childName, "err", err)
						continue
					}
					for gl := range sg {
						gn := enskit.Name(gl + "." + string(childName))
						gi := b.buildGrant(ctx, gn, childSubreg, gl)
						gi.Kind, gi.Target, gi.Device = "service", string(childName), string(resName)
						gi.Agent = gl + ".agents." + orgOf(netName)
						grants = append(grants, gi)
					}
					continue
				}
				gi := b.buildGrant(ctx, childName, resSubreg, label)
				gi.Kind, gi.Target, gi.Device = "device", string(resName), string(resName)
				gi.Agent = label + ".agents." + orgOf(netName)
				grants = append(grants, gi)
			}
		}
	}

	return info, grants, nil
}

func (b *MapBuilder) buildGrant(ctx context.Context, grantName enskit.Name, resSubreg common.Address, grantLabel string) GrantInfo {
	gi := GrantInfo{
		Name:     grantLabel,
		FullName: string(grantName),
	}

	grantState, err := b.ENS.State(ctx, resSubreg, grantLabel, nil)
	if err != nil {
		gi.Status = "revoked"
		return gi
	}

	gi.Expiry = grantState.Expiry
	gi.Owner = grantState.LatestOwner.Hex()

	now := uint64(time.Now().Unix())
	switch {
	case grantState.Status != enskit.Registered:
		gi.Status = "revoked"
	case grantState.Expiry != 0 && now > grantState.Expiry:
		gi.Status = "expired"
	default:
		gi.Status = "live"
	}

	return gi
}

// buildAgents builds AgentInfo from the agents subregistry.
// agentsSubreg is the registry that holds agent identities (zero means no agents).
func (b *MapBuilder) buildAgents(ctx context.Context, agentsName enskit.Name, agentsSubreg common.Address, attestors []string) ([]AgentInfo, error) {
	if agentsSubreg == (common.Address{}) {
		return nil, nil
	}

	agentChildren, err := enskit.ChildSubregs(ctx, b.ETH, agentsSubreg, b.FromBlock)
	if err != nil {
		return nil, fmt.Errorf("enumerate agents: %w", err)
	}

	// Build trusted attestors set.
	trustedSet := map[common.Address]struct{}{}
	for _, a := range attestors {
		if common.IsHexAddress(a) {
			trustedSet[common.HexToAddress(a)] = struct{}{}
		}
	}

	var agents []AgentInfo
	for agentLabel := range agentChildren {
		agentName := enskit.Name(agentLabel + "." + string(agentsName))
		agentRec, err := b.ENS.Lookup(ctx, agentName)
		if err != nil {
			b.log().Warn("agent lookup", "name", agentName, "err", err)
			continue
		}

		ai := AgentInfo{
			Name:     agentLabel,
			FullName: string(agentName),
		}
		if agentRec.Resolver != (common.Address{}) {
			if v, err := agentRec.Text(ctx, b.ENS, "meshy.ssh"); err == nil {
				ai.SSH = v
			}
			if v, err := agentRec.Text(ctx, b.ENS, "meshy.owner"); err == nil {
				ai.Owner = v
			}
			if v, err := agentRec.Text(ctx, b.ENS, "meshy.worldid"); err == nil && v != "" {
				ai.WorldIDRecord = v
				// Verify the attestation.
				att, signer, err := worldid.Parse(v)
				if err == nil && att.Expires > time.Now().Unix() {
					if _, trusted := trustedSet[signer]; trusted {
						ai.WorldIDValid = true
					}
				}
			}
			if a, err := b.ENS.Addr(ctx, agentRec.Resolver, agentName); err == nil && a != (common.Address{}) {
				ai.Addr = a.Hex()
			}
		}
		agents = append(agents, ai)
	}
	return agents, nil
}

// serviceSpec returns the meshy.service record of name, or "" if it is not a
// service (no resolver or no record).
func (b *MapBuilder) serviceSpec(ctx context.Context, name enskit.Name) string {
	rec, err := b.ENS.Lookup(ctx, name)
	if err != nil || rec.Resolver == (common.Address{}) {
		return ""
	}
	v, err := rec.Text(ctx, b.ENS, "meshy.service")
	if err != nil {
		return ""
	}
	return v
}

// orgOf returns the org part of a network name ("prod.acme.eth" -> "acme.eth").
func orgOf(network enskit.Name) string {
	_, rest, _ := strings.Cut(string(network), ".")
	return rest
}

// Snapshot converts the map into the reconciler's input: live devices,
// agents and grants only.
func (m *OrgMap) Snapshot() netbird.OrgSnapshot {
	s := netbird.OrgSnapshot{Org: m.Org}
	for _, n := range m.Networks {
		for _, r := range n.Resources {
			s.Resources = append(s.Resources, r.FullName)
		}
	}
	for _, a := range m.Agents {
		s.Agents = append(s.Agents, a.FullName)
	}
	for _, g := range m.Grants {
		if g.Status == "live" {
			s.Grants = append(s.Grants, netbird.GrantSnap{Name: g.FullName, Agent: g.Agent, Resource: g.Device})
		}
	}
	return s
}
