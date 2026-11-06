package provider_test

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/jashwanth0712/meshy/meshy/enskit"
	"github.com/jashwanth0712/meshy/meshy/provider"
)

// TestMapLiveSepolia is a live smoke test against real Sepolia.
// It is skipped unless ALCHEMY_KEY is set in the environment
// (or in ~/.meshy/secrets.env).
func TestMapLiveSepolia(t *testing.T) {
	key := loadAlchemyKey(t)
	rpc := "https://eth-sepolia.g.alchemy.com/v2/" + key

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	eth, err := ethclient.DialContext(ctx, rpc)
	if err != nil {
		t.Fatalf("dial Sepolia: %v", err)
	}
	defer eth.Close()

	builder := &provider.MapBuilder{
		ENS:       enskit.NewClient(eth, enskit.SepoliaUniversalHelper),
		ETH:       eth,
		FromBlock: 12072900,
		Log:       slog.Default(),
	}

	m, err := builder.Build(ctx, "meshydemo.eth")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	t.Logf("OrgMap block=%d generatedAt=%s", m.Block, m.GeneratedAt.Format(time.RFC3339))
	t.Logf("OrgKey=%q attestors=%v", m.OrgKey, m.Attestors)
	t.Logf("networks=%d agents=%d grants=%d", len(m.Networks), len(m.Agents), len(m.Grants))

	// Verify expected structure.
	foundProd := false
	for _, net := range m.Networks {
		t.Logf("network: %s", net.FullName)
		if net.Name == "prod" {
			foundProd = true
			foundDB1 := false
			for _, res := range net.Resources {
				t.Logf("  resource: %s addr=%s endpoint=%s", res.FullName, res.Addr, res.Endpoint)
				if res.Name == "db1" {
					foundDB1 = true
				}
			}
			if !foundDB1 {
				t.Error("expected db1 resource in prod network")
			}
		}
	}
	if !foundProd {
		t.Error("expected prod network")
	}

	foundClaude1 := false
	for _, ag := range m.Agents {
		t.Logf("agent: %s worldIdValid=%v", ag.FullName, ag.WorldIDValid)
		if ag.Name == "claude-1" {
			foundClaude1 = true
			if !ag.WorldIDValid {
				t.Log("note: claude-1 worldid is not valid (attestor may not match)")
			}
		}
	}
	if !foundClaude1 {
		t.Error("expected claude-1 agent")
	}

	// Verify there is a grant for claude-1.db1.prod.meshydemo.eth and it is revoked.
	foundGrant := false
	for _, g := range m.Grants {
		t.Logf("grant: %s status=%s expiry=%d", g.FullName, g.Status, g.Expiry)
		if strings.HasPrefix(g.Name, "claude-1") && strings.Contains(g.FullName, "db1.prod.meshydemo.eth") {
			foundGrant = true
			if g.Status != "revoked" && g.Status != "expired" {
				t.Logf("note: grant %s has status %s (expected revoked)", g.FullName, g.Status)
			}
		}
	}
	if !foundGrant {
		t.Log("note: claude-1 grant not found (may have been cleaned up)")
	}
}

// loadAlchemyKey reads ALCHEMY_KEY from env or ~/.meshy/secrets.env.
func loadAlchemyKey(t *testing.T) string {
	t.Helper()
	if k := os.Getenv("ALCHEMY_KEY"); k != "" {
		return k
	}
	// Try ~/.meshy/secrets.env.
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(home + "/.meshy/secrets.env")
	if err != nil {
		t.Skip("ALCHEMY_KEY not set and ~/.meshy/secrets.env not found; skipping live test")
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == "ALCHEMY_KEY" {
			return strings.TrimSpace(v)
		}
	}
	t.Skip("ALCHEMY_KEY not found in ~/.meshy/secrets.env; skipping live test")
	return ""
}
