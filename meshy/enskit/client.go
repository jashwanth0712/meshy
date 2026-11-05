package enskit

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// Sepolia ENSv2 deployment (docs.ens.domains/learn/deployments, 2026-10-01).
var (
	SepoliaUniversalHelper = common.HexToAddress("0xd453e5bdb62cc3bea84341b1e306319c8ffd7dfe")
	SepoliaRootRegistry    = common.HexToAddress("0xb458d6a3a77919449d03e7a6903c26827c1ec43f")
)

// Status mirrors IPermissionedRegistry.Status.
type Status uint8

const (
	Available Status = iota
	Reserved
	Registered
)

func (s Status) String() string {
	return [...]string{"AVAILABLE", "RESERVED", "REGISTERED"}[min(int(s), 2)]
}

// State mirrors IPermissionedRegistry.State.
type State struct {
	Status      Status
	Expiry      uint64
	LatestOwner common.Address
	TokenID     *big.Int
	Resource    *big.Int
}

const abiJSON = `[
 {"type":"function","name":"findExactOwner","stateMutability":"view","inputs":[{"name":"name","type":"bytes"}],"outputs":[{"type":"address"}]},
 {"type":"function","name":"findParentRegistry","stateMutability":"view","inputs":[{"name":"name","type":"bytes"}],"outputs":[{"type":"address"}]},
 {"type":"function","name":"getState","stateMutability":"view","inputs":[{"name":"anyId","type":"uint256"}],"outputs":[{"type":"tuple","components":[
   {"name":"status","type":"uint8"},{"name":"expiry","type":"uint64"},{"name":"latestOwner","type":"address"},{"name":"tokenId","type":"uint256"},{"name":"resource","type":"uint256"}]}]},
 {"type":"function","name":"getResolver","stateMutability":"view","inputs":[{"name":"label","type":"string"}],"outputs":[{"type":"address"}]},
 {"type":"function","name":"text","stateMutability":"view","inputs":[{"name":"node","type":"bytes32"},{"name":"key","type":"string"}],"outputs":[{"type":"string"}]},
 {"type":"function","name":"addr","stateMutability":"view","inputs":[{"name":"node","type":"bytes32"}],"outputs":[{"type":"address"}]}
]`

var ensABI = func() abi.ABI {
	a, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		panic(err)
	}
	return a
}()

// Caller is the subset of ethclient.Client we need (eases testing).
type Caller interface {
	CallContract(ctx context.Context, msg ethereum.CallMsg, block *big.Int) ([]byte, error)
}

// Client performs ENSv2 reads against a chain.
type Client struct {
	eth    Caller
	helper common.Address
}

// NewClient returns a client using the given UniversalHelper address.
func NewClient(eth Caller, helper common.Address) *Client { return &Client{eth: eth, helper: helper} }

func (c *Client) call(ctx context.Context, to common.Address, block *big.Int, method string, args ...any) ([]any, error) {
	data, err := ensABI.Pack(method, args...)
	if err != nil {
		return nil, err
	}
	out, err := c.eth.CallContract(ctx, ethereum.CallMsg{To: &to, Data: data}, block)
	if err != nil {
		return nil, fmt.Errorf("enskit: %s: %w", method, err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("enskit: %s: empty result from %s (not a contract?)", method, to)
	}
	return ensABI.Unpack(method, out)
}

// ExactOwner returns the owner of name, or the zero address if name or any
// ancestor is expired, unregistered or unowned at block (nil = latest).
func (c *Client) ExactOwner(ctx context.Context, name Name, block *big.Int) (common.Address, error) {
	dns, err := DNSEncode(string(name))
	if err != nil {
		return common.Address{}, err
	}
	res, err := c.call(ctx, c.helper, block, "findExactOwner", dns)
	if err != nil {
		return common.Address{}, err
	}
	return res[0].(common.Address), nil
}

// ParentRegistry returns the registry that holds name's first label.
func (c *Client) ParentRegistry(ctx context.Context, name Name, block *big.Int) (common.Address, error) {
	dns, err := DNSEncode(string(name))
	if err != nil {
		return common.Address{}, err
	}
	res, err := c.call(ctx, c.helper, block, "findParentRegistry", dns)
	if err != nil {
		return common.Address{}, err
	}
	return res[0].(common.Address), nil
}

// State returns the registry state of label in registry.
func (c *Client) State(ctx context.Context, registry common.Address, label string, block *big.Int) (State, error) {
	res, err := c.call(ctx, registry, block, "getState", LabelID(label))
	if err != nil {
		return State{}, err
	}
	raw := res[0].(struct {
		Status      uint8          `json:"status"`
		Expiry      uint64         `json:"expiry"`
		LatestOwner common.Address `json:"latestOwner"`
		TokenId     *big.Int       `json:"tokenId"`
		Resource    *big.Int       `json:"resource"`
	})
	return State{Status(raw.Status), raw.Expiry, raw.LatestOwner, raw.TokenId, raw.Resource}, nil
}

// Resolver returns the resolver set directly on label in registry (no
// wildcard fallback to ancestors: a grant must carry its own records).
func (c *Client) Resolver(ctx context.Context, registry common.Address, label string, block *big.Int) (common.Address, error) {
	res, err := c.call(ctx, registry, block, "getResolver", label)
	if err != nil {
		return common.Address{}, err
	}
	return res[0].(common.Address), nil
}

// Text reads a text record from resolver for name.
func (c *Client) Text(ctx context.Context, resolver common.Address, name Name, key string) (string, error) {
	res, err := c.call(ctx, resolver, nil, "text", Namehash(string(name)), key)
	if err != nil {
		return "", err
	}
	return res[0].(string), nil
}

// Addr reads the ETH address record from resolver for name.
func (c *Client) Addr(ctx context.Context, resolver common.Address, name Name) (common.Address, error) {
	res, err := c.call(ctx, resolver, nil, "addr", Namehash(string(name)))
	if err != nil {
		return common.Address{}, err
	}
	return res[0].(common.Address), nil
}

// Records is a convenience bundle: a name's registry, state and resolver.
type Records struct {
	Name     Name
	Registry common.Address
	State    State
	Resolver common.Address
}

// Lookup resolves registry, state and resolver for name at latest.
func (c *Client) Lookup(ctx context.Context, name Name) (*Records, error) {
	reg, err := c.ParentRegistry(ctx, name, nil)
	if err != nil {
		return nil, err
	}
	if reg == (common.Address{}) {
		return nil, fmt.Errorf("enskit: %s: parent registry not found (ancestor expired or missing)", name)
	}
	st, err := c.State(ctx, reg, name.Label(), nil)
	if err != nil {
		return nil, err
	}
	res, err := c.Resolver(ctx, reg, name.Label(), nil)
	if err != nil {
		return nil, err
	}
	return &Records{Name: name, Registry: reg, State: st, Resolver: res}, nil
}

// Text reads a text record of this name, or "" if it has no resolver.
func (r *Records) Text(ctx context.Context, c *Client, key string) (string, error) {
	if r.Resolver == (common.Address{}) {
		return "", nil
	}
	return c.Text(ctx, r.Resolver, r.Name, key)
}
