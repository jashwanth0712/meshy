// Package worldid defines the "human-backed agent" attestation stored in an
// agent's ENS `meshy.worldid` record.
//
// Flow: the agent starts a World ID Agent Pass device flow through the
// provider (a confidential OIDC client of auth.world.org). The provider
// validates the ID token server-side (signature, iss, aud, exp, auth_time,
// acr/amr), then signs this attestation binding the agent's address to an
// opaque hash of the human (iss, sub). Resource daemons verify the provider's
// EIP-191 signature against the org's trusted attestors (`meshy.attestor`).
// No World ID token or personal data ever goes on chain.
package worldid

import (
	"context"
	"crypto/ecdsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/jashwanth0712/meshy/meshy/enskit"
)

// Attestation is the signed claim.
type Attestation struct {
	V        int    `json:"v"`
	Agent    string `json:"agent"`    // ENS name, e.g. claude-1.agents.acme.eth
	Addr     string `json:"addr"`     // agent address (checksummed hex)
	Human    string `json:"human"`    // hex keccak256(iss || "|" || sub || "|" || org): opaque, per-org
	ACR      string `json:"acr"`      // e.g. https://world.org/oidc/acr/orb
	AuthTime int64  `json:"authTime"` // unix seconds of the World ID proof
	IssuedAt int64  `json:"iat"`
	Expires  int64  `json:"exp"`
	Attestor string `json:"attestor"` // provider signer address
	Sig      string `json:"sig,omitempty"`
}

// HumanHash derives the opaque human identifier.
func HumanHash(iss, sub, org string) string {
	return crypto.Keccak256Hash([]byte(iss + "|" + sub + "|" + org)).Hex()
}

func (a *Attestation) digest() ([]byte, error) {
	cp := *a
	cp.Sig = ""
	b, err := json.Marshal(cp)
	if err != nil {
		return nil, err
	}
	return accounts.TextHash(b), nil
}

// Sign fills Attestor and Sig and returns the record value (base64 JSON).
func Sign(a Attestation, key *ecdsa.PrivateKey) (string, error) {
	a.V = 1
	a.Attestor = crypto.PubkeyToAddress(key.PublicKey).Hex()
	d, err := a.digest()
	if err != nil {
		return "", err
	}
	sig, err := crypto.Sign(d, key)
	if err != nil {
		return "", err
	}
	sig[64] += 27
	a.Sig = "0x" + common.Bytes2Hex(sig)
	b, _ := json.Marshal(a)
	return base64.RawURLEncoding.EncodeToString(b), nil
}

var (
	ErrMalformed  = errors.New("worldid: malformed attestation")
	ErrSignature  = errors.New("worldid: bad signature")
	ErrUntrusted  = errors.New("worldid: attestor not trusted by org")
	ErrMismatch   = errors.New("worldid: attestation is for a different agent")
	ErrExpiredAtt = errors.New("worldid: attestation expired; re-verify with World ID")
)

// Parse decodes and verifies the signature, returning the attestation and
// the recovered signer.
func Parse(record string) (*Attestation, common.Address, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(record))
	if err != nil {
		return nil, common.Address{}, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	var a Attestation
	if err := json.Unmarshal(raw, &a); err != nil || a.V != 1 || a.Sig == "" {
		return nil, common.Address{}, ErrMalformed
	}
	sig := common.FromHex(a.Sig)
	if len(sig) != 65 {
		return nil, common.Address{}, ErrSignature
	}
	sig = append([]byte{}, sig...)
	if sig[64] >= 27 {
		sig[64] -= 27
	}
	d, err := a.digest()
	if err != nil {
		return nil, common.Address{}, err
	}
	pub, err := crypto.SigToPub(d, sig)
	if err != nil {
		return nil, common.Address{}, ErrSignature
	}
	signer := crypto.PubkeyToAddress(*pub)
	if !strings.EqualFold(signer.Hex(), a.Attestor) {
		return nil, common.Address{}, ErrSignature
	}
	return &a, signer, nil
}

// Verifier implements meshyd.HumanVerifier.
type Verifier struct {
	// Trusted returns the attestor addresses the agent's org trusts. The ENS
	// implementation reads the org's `meshy.attestor` text record.
	Trusted func(ctx context.Context, org enskit.Name) ([]common.Address, error)
	Now     func() time.Time
}

func (v *Verifier) VerifyAgent(ctx context.Context, agent enskit.Name, addr common.Address, record string) error {
	a, signer, err := Parse(record)
	if err != nil {
		return err
	}
	if a.Agent != string(agent) || !strings.EqualFold(a.Addr, addr.Hex()) {
		return ErrMismatch
	}
	now := time.Now()
	if v.Now != nil {
		now = v.Now()
	}
	if a.Expires != 0 && now.Unix() > a.Expires {
		return ErrExpiredAtt
	}
	trusted, err := v.Trusted(ctx, agent.Org())
	if err != nil {
		return fmt.Errorf("worldid: trusted attestors: %w", err)
	}
	for _, t := range trusted {
		if t == signer {
			return nil
		}
	}
	return ErrUntrusted
}

// ENSTrusted reads comma-separated addresses from the org's `meshy.attestor`.
func ENSTrusted(c *enskit.Client) func(ctx context.Context, org enskit.Name) ([]common.Address, error) {
	return func(ctx context.Context, org enskit.Name) ([]common.Address, error) {
		r, err := c.Lookup(ctx, org)
		if err != nil {
			return nil, err
		}
		s, err := r.Text(ctx, c, "meshy.attestor")
		if err != nil {
			return nil, err
		}
		var out []common.Address
		for _, p := range strings.Split(s, ",") {
			if p = strings.TrimSpace(p); common.IsHexAddress(p) {
				out = append(out, common.HexToAddress(p))
			}
		}
		return out, nil
	}
}
