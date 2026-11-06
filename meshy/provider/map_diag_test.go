package provider_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/jashwanth0712/meshy/meshy/enskit"
)

// TestDiagnoseSepolia is a quick diagnostic test to inspect raw ENS state.
// Run manually with: go test -v -run TestDiagnoseSepolia ./provider/ -timeout 120s
func TestDiagnoseSepolia(t *testing.T) {
	key := loadAlchemyKey(t)
	rpc := "https://eth-sepolia.g.alchemy.com/v2/" + key

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	eth, err := ethclient.DialContext(ctx, rpc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer eth.Close()

	head, _ := eth.HeaderByNumber(ctx, nil)
	t.Logf("current block: %d", head.Number.Uint64())

	ens := enskit.NewClient(eth, enskit.SepoliaUniversalHelper)

	// Look up meshydemo.eth
	orgName := enskit.Name("meshydemo.eth")
	orgRec, err := ens.Lookup(ctx, orgName)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	t.Logf("org status: %s", orgRec.State.Status)
	t.Logf("org expiry: %d", orgRec.State.Expiry)
	t.Logf("org owner: %s", orgRec.State.LatestOwner.Hex())
	t.Logf("org registry: %s", orgRec.Registry.Hex())
	t.Logf("org resolver: %s", orgRec.Resolver.Hex())

	// Find the org subregistry via findParentRegistry of known children.
	for _, child := range []string{"agents.meshydemo.eth", "prod.meshydemo.eth", "db1.prod.meshydemo.eth"} {
		reg, err := ens.ParentRegistry(ctx, enskit.Name(child), nil)
		if err != nil {
			t.Logf("ParentRegistry(%s): %v", child, err)
		} else {
			t.Logf("ParentRegistry(%s): %s", child, reg.Hex())
		}
	}
}
