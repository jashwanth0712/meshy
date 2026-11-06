package meshyd

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jashwanth0712/meshy/meshy/enskit"
	"github.com/jashwanth0712/meshy/meshy/policy"
)

// Identity is an authenticated agent (key + ENS identity + World ID checked).
type Identity struct {
	Agent     string      // agent label (SSH username), e.g. "claude-1"
	AgentName enskit.Name // claude-1.agents.acme.eth
	Addr      common.Address
}

// Authz authorizes a shell/exec session on this resource (device grant).
type Authz struct {
	Agent     string       // agent label (the SSH username), e.g. "claude-1"
	AgentName enskit.Name  // claude-1.agents.acme.eth
	Grant     enskit.Grant // what the watcher re-checks every block
	Expiry    time.Time
	Policy    *policy.Policy
	// Services is used only by StaticAuthorizer (tests/demos): service -> target.
	Services map[enskit.Name]string
}

// Service is a network service published under a device in ENS, e.g.
// camera.pi.home.acme.eth -> {"proto":"http","target":"127.0.0.1:8001"}.
type Service struct {
	Proto  string `json:"proto"`
	Target string `json:"target"`
	Desc   string `json:"desc,omitempty"`
}

// ServiceAuthz authorizes one port-forward to a service (service grant).
type ServiceAuthz struct {
	Service enskit.Name
	Grant   enskit.Grant
	Expiry  time.Time
	Spec    Service
}

// Authorizer decides who may log in and what they may do.
type Authorizer interface {
	// Authenticate checks the SSH key against the agent's ENS identity (and
	// World ID attestation). It does not require any grant.
	Authenticate(ctx context.Context, user string, key gossh.PublicKey) (*Identity, error)
	// AuthorizeSession requires a live device grant <agent>.<resource> with a policy.
	AuthorizeSession(ctx context.Context, id *Identity) (*Authz, error)
	// AuthorizeService requires a live service grant <agent>.<service>, where
	// service is a child of this resource; returns the ENS-declared target.
	AuthorizeService(ctx context.Context, id *Identity, service enskit.Name) (*ServiceAuthz, error)
}

// DenyError is an authorization failure with an agent-facing code.
type DenyError struct {
	Code   string // MESHY_NO_GRANT, MESHY_EXPIRED, MESHY_NOT_HUMAN_BACKED, MESHY_BAD_KEY, ...
	Detail string
}

func (e *DenyError) Error() string { return e.Code + " " + e.Detail }

func deny(code, format string, a ...any) error {
	return &DenyError{Code: code, Detail: fmt.Sprintf(format, a...)}
}

// HumanVerifier checks that an agent is backed by a verified human (World ID
// Agent Pass attestation stored in the agent's `meshy.worldid` record).
type HumanVerifier interface {
	VerifyAgent(ctx context.Context, agent enskit.Name, agentAddr common.Address, attestation string) error
}

// ENSAuthorizer authorizes from ENSv2 state.
//
//   - Authenticate: agent identity U.agents.<org> has `meshy.ssh` == key, an
//     addr record, and (if Human != nil) a valid `meshy.worldid` attestation.
//   - AuthorizeSession: grant U.<resource> is REGISTERED, unexpired, owned (with
//     every ancestor) by the agent address, and its `meshy.policy` decrypts
//     with the daemon key, names this grant and carries its current token id.
//   - AuthorizeService: S is a child of <resource> with a `meshy.service`
//     record, and grant U.S is live and owned by the agent.
type ENSAuthorizer struct {
	ENS      *enskit.Client
	Resource enskit.Name
	Key      *ecdh.PrivateKey // daemon policy key
	Human    HumanVerifier    // nil disables the World ID check (dev only)
	Now      func() time.Time
}

func (a *ENSAuthorizer) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *ENSAuthorizer) Authenticate(ctx context.Context, user string, key gossh.PublicKey) (*Identity, error) {
	agentName, err := enskit.AgentName(user, a.Resource)
	if err != nil {
		return nil, deny("MESHY_BAD_USER", "user=%q: %v", user, err)
	}
	ag, err := a.ENS.Lookup(ctx, agentName)
	if err != nil || ag.State.Status != enskit.Registered {
		return nil, deny("MESHY_UNKNOWN_AGENT", "agent=%s", agentName)
	}
	if ag.Resolver == (common.Address{}) {
		return nil, deny("MESHY_UNKNOWN_AGENT", "agent=%s has no resolver", agentName)
	}
	sshRec, err := a.ENS.Text(ctx, ag.Resolver, agentName, "meshy.ssh")
	if err != nil {
		return nil, fmt.Errorf("meshyd: ens: %w", err)
	}
	if !sameKey(sshRec, key) {
		return nil, deny("MESHY_BAD_KEY", "key does not match %s meshy.ssh", agentName)
	}
	agentAddr, err := a.ENS.Addr(ctx, ag.Resolver, agentName)
	if err != nil {
		return nil, fmt.Errorf("meshyd: ens: %w", err)
	}
	if agentAddr == (common.Address{}) {
		return nil, deny("MESHY_UNKNOWN_AGENT", "agent=%s has no addr record", agentName)
	}
	if a.Human != nil {
		att, err := a.ENS.Text(ctx, ag.Resolver, agentName, "meshy.worldid")
		if err != nil {
			return nil, fmt.Errorf("meshyd: ens: %w", err)
		}
		if att == "" {
			return nil, deny("MESHY_NOT_HUMAN_BACKED:", "complete World Agent Pass via /meshy:join (agent=%s)", agentName)
		}
		if err := a.Human.VerifyAgent(ctx, agentName, agentAddr, att); err != nil {
			return nil, deny("MESHY_NOT_HUMAN_BACKED:", "%v (agent=%s)", err, agentName)
		}
	}
	return &Identity{Agent: user, AgentName: agentName, Addr: agentAddr}, nil
}

// liveGrant checks that grant is registered, unexpired (with ancestors) and
// owned by owner. It returns the grant's records.
func (a *ENSAuthorizer) liveGrant(ctx context.Context, grantName enskit.Name, owner common.Address) (*enskit.Records, time.Time, error) {
	exact, err := a.ENS.ExactOwner(ctx, grantName, nil)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("meshyd: ens: %w", err)
	}
	if exact == (common.Address{}) {
		return nil, time.Time{}, deny("MESHY_NO_GRANT", "grant=%s", grantName)
	}
	if exact != owner {
		return nil, time.Time{}, deny("MESHY_NO_GRANT", "grant=%s is owned by %s, not the agent %s", grantName, exact.Hex(), owner.Hex())
	}
	g, err := a.ENS.Lookup(ctx, grantName)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("meshyd: ens: %w", err)
	}
	if g.State.Status != enskit.Registered {
		return nil, time.Time{}, deny("MESHY_NO_GRANT", "grant=%s status=%s", grantName, g.State.Status)
	}
	expiry := time.Unix(int64(g.State.Expiry), 0)
	if !expiry.After(a.now()) {
		return nil, time.Time{}, deny("MESHY_EXPIRED", "grant=%s", grantName)
	}
	return g, expiry, nil
}

func (a *ENSAuthorizer) AuthorizeSession(ctx context.Context, id *Identity) (*Authz, error) {
	grantName, err := enskit.GrantName(id.Agent, a.Resource)
	if err != nil {
		return nil, deny("MESHY_BAD_USER", "%v", err)
	}
	g, expiry, err := a.liveGrant(ctx, grantName, id.Addr)
	if err != nil {
		return nil, err
	}
	if g.Resolver == (common.Address{}) {
		return nil, deny("MESHY_NO_POLICY", "grant=%s has no resolver", grantName)
	}
	rec, err := a.ENS.Text(ctx, g.Resolver, grantName, "meshy.policy")
	if err != nil {
		return nil, fmt.Errorf("meshyd: ens: %w", err)
	}
	if rec == "" {
		return nil, deny("MESHY_NO_POLICY", "grant=%s (a folder policy is required for shell access)", grantName)
	}
	pol, err := policy.Open(rec, string(grantName), a.Key)
	if err != nil {
		return nil, deny("MESHY_NO_POLICY", "grant=%s: %v", grantName, err)
	}
	if pol.GrantTokenID != "" && pol.GrantTokenID != g.State.TokenID.String() {
		return nil, deny("MESHY_STALE_POLICY", "grant=%s was re-issued; ask an admin to re-grant", grantName)
	}
	return &Authz{
		Agent: id.Agent, AgentName: id.AgentName,
		Grant:  enskit.Grant{Name: grantName, Owner: id.Addr, TokenID: g.State.TokenID},
		Expiry: expiry, Policy: pol,
	}, nil
}

func (a *ENSAuthorizer) AuthorizeService(ctx context.Context, id *Identity, service enskit.Name) (*ServiceAuthz, error) {
	service = enskit.Name(strings.ToLower(strings.TrimSuffix(string(service), ".")))
	if service.Parent() != a.Resource {
		return nil, deny("MESHY_NOT_HERE", "service=%s is not published under %s", service, a.Resource)
	}
	svc, err := a.ENS.Lookup(ctx, service)
	if err != nil || svc.State.Status != enskit.Registered {
		return nil, deny("MESHY_UNKNOWN_SERVICE", "service=%s", service)
	}
	raw, err := svc.Text(ctx, a.ENS, "meshy.service")
	if err != nil {
		return nil, fmt.Errorf("meshyd: ens: %w", err)
	}
	var spec Service
	if json.Unmarshal([]byte(raw), &spec) != nil || spec.Target == "" {
		return nil, deny("MESHY_UNKNOWN_SERVICE", "service=%s has no valid meshy.service record", service)
	}
	if _, _, err := net.SplitHostPort(spec.Target); err != nil {
		return nil, deny("MESHY_UNKNOWN_SERVICE", "service=%s target %q is not host:port", service, spec.Target)
	}
	grantName := enskit.Name(id.Agent + "." + string(service))
	g, expiry, err := a.liveGrant(ctx, grantName, id.Addr)
	if err != nil {
		return nil, err
	}
	return &ServiceAuthz{
		Service: service,
		Grant:   enskit.Grant{Name: grantName, Owner: id.Addr, TokenID: g.State.TokenID},
		Expiry:  expiry, Spec: spec,
	}, nil
}

// sameKey compares an authorized_keys-format record with a presented key.
func sameKey(record string, key gossh.PublicKey) bool {
	rec, _, _, _, err := gossh.ParseAuthorizedKey([]byte(strings.TrimSpace(record)))
	if err != nil {
		return false
	}
	return bytes.Equal(rec.Marshal(), key.Marshal())
}

// StaticAuthorizer is a fixed table, for tests and local demos without chain.
type StaticAuthorizer map[string]*Authz

var ErrDenied = errors.New("denied")

func (s StaticAuthorizer) Authenticate(_ context.Context, user string, _ gossh.PublicKey) (*Identity, error) {
	az, ok := s[user]
	if !ok {
		return nil, deny("MESHY_UNKNOWN_AGENT", "user=%s", user)
	}
	return &Identity{Agent: user, AgentName: az.AgentName, Addr: az.Grant.Owner}, nil
}

func (s StaticAuthorizer) AuthorizeSession(_ context.Context, id *Identity) (*Authz, error) {
	az, ok := s[id.Agent]
	if !ok || az.Policy == nil {
		return nil, deny("MESHY_NO_GRANT", "user=%s", id.Agent)
	}
	return az, nil
}

func (s StaticAuthorizer) AuthorizeService(_ context.Context, id *Identity, service enskit.Name) (*ServiceAuthz, error) {
	az, ok := s[id.Agent]
	if !ok {
		return nil, deny("MESHY_NO_GRANT", "user=%s", id.Agent)
	}
	t, ok := az.Services[service]
	if !ok {
		return nil, deny("MESHY_NO_GRANT", "grant=%s.%s", id.Agent, service)
	}
	return &ServiceAuthz{Service: service, Grant: enskit.Grant{Name: enskit.Name(id.Agent + "." + string(service))}, Expiry: az.Expiry, Spec: Service{Proto: "tcp", Target: t}}, nil
}
