package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/jashwanth0712/meshy/meshy/policy"
)

// SepoliaOrgFactory is MeshyOrgFactory (contracts/deployments/orgs-sepolia.json).
const SepoliaOrgFactory = "0x0eB7E8DBD2947a5A7769faD210E87944DB513403"

var factoryABI = func() abi.ABI {
	a, err := abi.JSON(strings.NewReader(`[
 {"type":"function","name":"createOrg","inputs":[{"name":"label","type":"string"},{"name":"orgKey","type":"string"},{"name":"attestor","type":"string"}],"outputs":[]},
 {"type":"function","name":"isAvailable","stateMutability":"view","inputs":[{"name":"label","type":"string"}],"outputs":[{"type":"bool"}]},
 {"type":"function","name":"orgs","stateMutability":"view","inputs":[{"name":"","type":"bytes32"}],"outputs":[{"name":"owner","type":"address"},{"name":"orgRegistry","type":"address"},{"name":"resolver","type":"address"},{"name":"peopleRegistry","type":"address"},{"name":"agentsRegistry","type":"address"},{"name":"createdAt","type":"uint64"}]}]`))
	if err != nil {
		panic(err)
	}
	return a
}()

// orgCreate makes <label>.meshy.eth with MeshyOrgFactory (one transaction; the
// same contract the dashboard's /new page uses) and saves admin state so the
// network/resource/grant commands work on it.
func orgCreate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("org create", flag.ExitOnError)
	var c cli
	c.register(fs)
	label := fs.String("label", "", "org label (creates <label>.meshy.eth)")
	attestor := fs.String("attestor", "0x6Aafef03Fbbbeb56ff4422f042b25f754F6e0EC4", "trusted World ID attestor (provider signer)")
	factory := fs.String("factory", SepoliaOrgFactory, "MeshyOrgFactory address")
	fs.Parse(args)
	if *label == "" {
		return errors.New("--label required")
	}
	a, err := c.admin(ctx)
	if err != nil {
		return err
	}
	fac := common.HexToAddress(*factory)
	call := func(method string, out any, args ...any) error {
		data, err := factoryABI.Pack(method, args...)
		if err != nil {
			return err
		}
		res, err := a.Eth.CallContract(ctx, ethereum.CallMsg{To: &fac, Data: data}, nil)
		if err != nil {
			return err
		}
		return factoryABI.UnpackIntoInterface(out, method, res)
	}
	var avail bool
	if err := call("isAvailable", &avail, *label); err != nil {
		return fmt.Errorf("isAvailable: %w", err)
	}
	if !avail {
		return fmt.Errorf("%s.meshy.eth is taken or invalid", *label)
	}
	ok, _ := policy.GenerateKey()
	data, err := factoryABI.Pack("createOrg", *label, policy.EncodePublicKey(ok.PublicKey()), *attestor)
	if err != nil {
		return err
	}
	org := *label + ".meshy.eth"
	fmt.Printf("Creating %s (owner %s) with MeshyOrgFactory\n", org, a.From().Hex())
	if _, err := a.Send(ctx, fac, data); err != nil {
		return fmt.Errorf("createOrg: %w", err)
	}
	var o struct {
		Owner, OrgRegistry, Resolver, PeopleRegistry, AgentsRegistry common.Address
		CreatedAt                                                    uint64
	}
	if err := call("orgs", &o, crypto.Keccak256Hash([]byte(*label))); err != nil {
		return fmt.Errorf("orgs: %w", err)
	}
	step("org registry %s, resolver %s", o.OrgRegistry.Hex(), o.Resolver.Hex())
	step("people %s, agents %s", o.PeopleRegistry.Hex(), o.AgentsRegistry.Hex())
	s := &State{Org: org, Resolver: o.Resolver.Hex(), OrgReg: o.OrgRegistry.Hex(), AgentsReg: o.AgentsRegistry.Hex(),
		Networks: map[string]string{}, Resources: map[string]string{}, OrgKeyPriv: policy.EncodePrivateKey(ok)}
	if err := s.save(); err != nil {
		return err
	}
	fmt.Printf("Org ready: %s. Invite people from https://meshy.live/org/%s/people\n", org, org)
	return nil
}
