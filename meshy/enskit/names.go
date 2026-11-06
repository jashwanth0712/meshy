// Package enskit reads Meshy's access state from ENSv2 and watches it for
// revocation. ENS is the source of truth: a grant is live only while its
// subname (and every ancestor) is registered, unexpired, owned by the agent,
// and still carries the token id it was issued with.
package enskit

import (
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Labelhash returns keccak256(label), the ENSv2 "anyId" for a label.
func Labelhash(label string) common.Hash { return crypto.Keccak256Hash([]byte(label)) }

// LabelID returns the labelhash as a uint256 anyId argument.
func LabelID(label string) *big.Int { return new(big.Int).SetBytes(Labelhash(label).Bytes()) }

// Namehash implements ENSIP-1 namehash.
func Namehash(name string) common.Hash {
	var node common.Hash
	if name == "" {
		return node
	}
	labels := strings.Split(name, ".")
	for i := len(labels) - 1; i >= 0; i-- {
		lh := Labelhash(labels[i])
		node = crypto.Keccak256Hash(node.Bytes(), lh.Bytes())
	}
	return node
}

// DNSEncode encodes a name in DNS wire format, as ENSv2 helpers expect.
func DNSEncode(name string) ([]byte, error) {
	if name == "" {
		return []byte{0}, nil
	}
	var out []byte
	for _, l := range strings.Split(name, ".") {
		if l == "" || len(l) > 255 {
			return nil, fmt.Errorf("enskit: bad label in %q", name)
		}
		out = append(out, byte(len(l)))
		out = append(out, l...)
	}
	return append(out, 0), nil
}

// Name is a parsed Meshy name. Meshy's namespace is
//
//	<agent>.<resource>.<network>.<org>.eth   grant
//	<resource>.<network>.<org>.eth           resource
//	<agent>.agents.<org>.eth                 agent identity
type Name string

// Label returns the first label.
func (n Name) Label() string { l, _, _ := strings.Cut(string(n), "."); return l }

// Parent returns the name without its first label.
func (n Name) Parent() Name { _, p, _ := strings.Cut(string(n), "."); return Name(p) }

// OrgParents are shared parent names under which orgs are free subnames
// (MeshyOrgFactory): an org under one of them has three labels, e.g.
// acme.meshy.eth. Extend with MESHY_ORG_PARENTS (comma-separated).
var OrgParents = func() []string {
	ps := []string{"meshy.eth"}
	for _, p := range strings.Split(os.Getenv("MESHY_ORG_PARENTS"), ",") {
		if p = strings.TrimSpace(strings.ToLower(p)); p != "" {
			ps = append(ps, p)
		}
	}
	return ps
}()

// Org returns the org a name belongs to: the 2LD ("acme.eth"), or the 3LD for
// orgs under a shared parent ("acme.meshy.eth"). A name that is itself a shared
// parent is returned as is.
func (n Name) Org() Name {
	parts := strings.Split(string(n), ".")
	if len(parts) < 2 {
		return n
	}
	for _, p := range OrgParents {
		if strings.HasSuffix(string(n), "."+p) && len(parts) >= 3 {
			return Name(strings.Join(parts[len(parts)-3:], "."))
		}
	}
	return Name(strings.Join(parts[len(parts)-2:], "."))
}

// Depth returns how many labels n has below its org (db1.prod.acme.eth -> 2).
func (n Name) Depth() int {
	org := string(n.Org())
	if string(n) == org {
		return 0
	}
	return strings.Count(strings.TrimSuffix(string(n), "."+org), ".") + 1
}

// ErrBadName is returned for names that do not fit Meshy's namespace.
var ErrBadName = errors.New("enskit: name does not fit the Meshy namespace")

// GrantName returns the grant name for agentLabel on resource.
func GrantName(agentLabel string, resource Name) (Name, error) {
	if err := validLabel(agentLabel); err != nil {
		return "", err
	}
	if resource.Depth() < 2 { // db1.prod.acme.eth
		return "", fmt.Errorf("%w: resource %q", ErrBadName, resource)
	}
	return Name(agentLabel + "." + string(resource)), nil
}

// AgentName returns the identity name of agentLabel in resource's org.
func AgentName(agentLabel string, resource Name) (Name, error) {
	if err := validLabel(agentLabel); err != nil {
		return "", err
	}
	return Name(agentLabel + ".agents." + string(resource.Org())), nil
}

func validLabel(l string) error {
	if l == "" || len(l) > 63 {
		return fmt.Errorf("%w: label %q", ErrBadName, l)
	}
	for _, r := range l {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return fmt.Errorf("%w: label %q (use a-z, 0-9, -)", ErrBadName, l)
		}
	}
	return nil
}
