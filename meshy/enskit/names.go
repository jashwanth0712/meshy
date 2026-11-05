// Package enskit reads Meshy's access state from ENSv2 and watches it for
// revocation. ENS is the source of truth: a grant is live only while its
// subname (and every ancestor) is registered, unexpired, owned by the agent,
// and still carries the token id it was issued with.
package enskit

import (
	"errors"
	"fmt"
	"math/big"
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

// Org returns the org 2LD (last two labels), e.g. "acme.eth".
func (n Name) Org() Name {
	parts := strings.Split(string(n), ".")
	if len(parts) < 2 {
		return n
	}
	return Name(strings.Join(parts[len(parts)-2:], "."))
}

// ErrBadName is returned for names that do not fit Meshy's namespace.
var ErrBadName = errors.New("enskit: name does not fit the Meshy namespace")

// GrantName returns the grant name for agentLabel on resource.
func GrantName(agentLabel string, resource Name) (Name, error) {
	if err := validLabel(agentLabel); err != nil {
		return "", err
	}
	if strings.Count(string(resource), ".") < 3 { // db1.prod.acme.eth
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
