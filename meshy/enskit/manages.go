package enskit

import (
	"context"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

var eacABI = func() abi.ABI {
	a, err := abi.JSON(strings.NewReader(`[{"type":"function","name":"hasRootRoles","stateMutability":"view","inputs":[{"name":"roleBitmap","type":"uint256"},{"name":"account","type":"address"}],"outputs":[{"type":"bool"}]}]`))
	if err != nil {
		panic(err)
	}
	return a
}()

// HasRootRoles reports whether account holds all of roles on an EAC registry.
func (c *Client) HasRootRoles(ctx context.Context, registry common.Address, roles *big.Int, account common.Address) (bool, error) {
	data, _ := eacABI.Pack("hasRootRoles", roles, account)
	out, err := c.eth.CallContract(ctx, ethereum.CallMsg{To: &registry, Data: data}, nil)
	if err != nil || len(out) == 0 {
		return false, err
	}
	v, err := eacABI.Unpack("hasRootRoles", out)
	if err != nil || len(v) == 0 {
		return false, err
	}
	return v[0].(bool), nil
}

// Manages reports whether wallet may oversee device (<device>.<network>.<org>):
// it owns the org, or it is a delegated admin of the device's network (the
// admin roles on the network registry, as the dashboard grants them).
func (c *Client) Manages(ctx context.Context, wallet common.Address, device Name) (bool, error) {
	if wallet == (common.Address{}) {
		return false, nil
	}
	if owner, err := c.ExactOwner(ctx, device.Org(), nil); err == nil && owner == wallet {
		return true, nil
	}
	reg, err := c.ParentRegistry(ctx, device, nil) // the network's registry holds the device label
	if err != nil || reg == (common.Address{}) {
		return false, err
	}
	return c.HasRootRoles(ctx, reg, AdminRoles, wallet)
}
