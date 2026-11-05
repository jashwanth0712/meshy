package meshyd

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jashwanth0712/meshy/meshy/enskit"
	"github.com/jashwanth0712/meshy/meshy/policy"
)

// Authz is the result of a successful authorization.
type Authz struct {
	Agent     string       // agent label (the SSH username), e.g. "claude-1"
	AgentName enskit.Name  // claude-1.agents.acme.eth
	Grant     enskit.Grant // what the watcher re-checks every block
	Expiry    time.Time
	Policy    *policy.Policy
}

// Authorizer decides whether an SSH login is allowed.
type Authorizer interface {
	Authorize(ctx context.Context, user string, key gossh.PublicKey) (*Authz, error)
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

// ENSAuthorizer authorizes logins from ENSv2 state.
//
// For user U connecting to resource R it requires:
//  1. grant U.R is REGISTERED, unexpired, and (with every ancestor) owned;
//  2. agent identity U.agents.<org> has `meshy.ssh` equal to the presented key
//     and an addr record equal to the grant owner;
//  3. (if Human != nil) a valid `meshy.worldid` attestation on the agent;
//  4. the grant's `meshy.policy` decrypts with the daemon key, names this
//     grant, and carries the grant's current token id.
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

func (a *ENSAuthorizer) Authorize(ctx context.Context, user string, key gossh.PublicKey) (*Authz, error) {
	grantName, err := enskit.GrantName(user, a.Resource)
	if err != nil {
		return nil, deny("MESHY_BAD_USER", "user=%q: %v", user, err)
	}
	agentName, _ := enskit.AgentName(user, a.Resource)

	// 1. Grant must be live with all ancestors.
	owner, err := a.ENS.ExactOwner(ctx, grantName, nil)
	if err != nil {
		return nil, fmt.Errorf("meshyd: ens: %w", err)
	}
	if owner == (common.Address{}) {
		return nil, deny("MESHY_NO_GRANT", "resource=%s grant=%s", a.Resource, grantName)
	}
	g, err := a.ENS.Lookup(ctx, grantName)
	if err != nil {
		return nil, fmt.Errorf("meshyd: ens: %w", err)
	}
	if g.State.Status != enskit.Registered {
		return nil, deny("MESHY_NO_GRANT", "grant=%s status=%s", grantName, g.State.Status)
	}
	expiry := time.Unix(int64(g.State.Expiry), 0)
	if !expiry.After(a.now()) {
		return nil, deny("MESHY_EXPIRED", "grant=%s", grantName)
	}

	// 2. Agent identity binds the SSH key and the grant owner.
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
	if agentAddr != owner {
		return nil, deny("MESHY_NO_GRANT", "grant=%s is owned by %s, not agent %s", grantName, owner.Hex(), agentAddr.Hex())
	}

	// 3. Human-backed (World ID Agent Pass).
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

	// 4. Policy.
	if g.Resolver == (common.Address{}) {
		return nil, deny("MESHY_NO_POLICY", "grant=%s has no resolver", grantName)
	}
	rec, err := a.ENS.Text(ctx, g.Resolver, grantName, "meshy.policy")
	if err != nil {
		return nil, fmt.Errorf("meshyd: ens: %w", err)
	}
	if rec == "" {
		return nil, deny("MESHY_NO_POLICY", "grant=%s", grantName)
	}
	pol, err := policy.Open(rec, string(grantName), a.Key)
	if err != nil {
		return nil, deny("MESHY_NO_POLICY", "grant=%s: %v", grantName, err)
	}
	if pol.GrantTokenID != "" && pol.GrantTokenID != g.State.TokenID.String() {
		return nil, deny("MESHY_STALE_POLICY", "grant=%s was re-issued; ask an admin to re-grant", grantName)
	}

	return &Authz{
		Agent:     user,
		AgentName: agentName,
		Grant:     enskit.Grant{Name: grantName, Owner: owner, TokenID: g.State.TokenID},
		Expiry:    expiry,
		Policy:    pol,
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

func (s StaticAuthorizer) Authorize(_ context.Context, user string, _ gossh.PublicKey) (*Authz, error) {
	if az, ok := s[user]; ok {
		return az, nil
	}
	return nil, deny("MESHY_NO_GRANT", "user=%s", user)
}
