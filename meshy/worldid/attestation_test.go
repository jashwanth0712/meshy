package worldid

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/jashwanth0712/meshy/meshy/enskit"
)

func TestSignVerify(t *testing.T) {
	provider, _ := crypto.GenerateKey()
	agentKey, _ := crypto.GenerateKey()
	agent := enskit.Name("claude-1.agents.acme.eth")
	addr := crypto.PubkeyToAddress(agentKey.PublicKey)
	rec, err := Sign(Attestation{
		Agent: string(agent), Addr: addr.Hex(), Human: HumanHash("https://auth.world.org", "sub-123", "acme.eth"),
		ACR: "https://world.org/oidc/acr/orb", AuthTime: time.Now().Unix(), IssuedAt: time.Now().Unix(),
		Expires: time.Now().Add(24 * time.Hour).Unix(),
	}, provider)
	if err != nil {
		t.Fatal(err)
	}
	trusted := crypto.PubkeyToAddress(provider.PublicKey)
	v := &Verifier{Trusted: func(context.Context, enskit.Name) ([]common.Address, error) { return []common.Address{trusted}, nil }}
	ctx := context.Background()

	if err := v.VerifyAgent(ctx, agent, addr, rec); err != nil {
		t.Fatalf("valid attestation rejected: %v", err)
	}
	if err := v.VerifyAgent(ctx, "codex.agents.acme.eth", addr, rec); !errors.Is(err, ErrMismatch) {
		t.Fatalf("other agent: %v", err)
	}
	other, _ := crypto.GenerateKey()
	if err := v.VerifyAgent(ctx, agent, crypto.PubkeyToAddress(other.PublicKey), rec); !errors.Is(err, ErrMismatch) {
		t.Fatalf("other addr: %v", err)
	}
	untrusting := &Verifier{Trusted: func(context.Context, enskit.Name) ([]common.Address, error) { return nil, nil }}
	if err := untrusting.VerifyAgent(ctx, agent, addr, rec); !errors.Is(err, ErrUntrusted) {
		t.Fatalf("untrusted: %v", err)
	}
	late := &Verifier{Trusted: v.Trusted, Now: func() time.Time { return time.Now().Add(48 * time.Hour) }}
	if err := late.VerifyAgent(ctx, agent, addr, rec); !errors.Is(err, ErrExpiredAtt) {
		t.Fatalf("expired: %v", err)
	}
	// Tamper: flip a character in the payload.
	tampered := rec[:20] + string(rune(rec[20]^1)) + rec[21:]
	if err := v.VerifyAgent(ctx, agent, addr, tampered); err == nil {
		t.Fatal("tampered attestation accepted")
	}
}
