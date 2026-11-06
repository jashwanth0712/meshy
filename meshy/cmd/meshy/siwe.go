package main

// meshy verify-siwe: check a Sign-In with Ethereum (EIP-4361) message and its
// EIP-191 personal_sign signature, and print who signed it. Meshy for Mac runs
// this (bundled in the app) on the meshy://auth callback, so the menu bar only
// trusts an address that really signed the nonce it issued.
//
//	meshy verify-siwe --sig 0x… [--nonce n] [--domain d] [--chain 11155111] < message.txt
//
// Prints one JSON line {"address","domain","nonce","chainId","issuedAt","expiresAt"}
// and exits 0, or prints MESHY_SIWE_INVALID <reason> and exits 1.

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type siweMessage struct {
	Domain    string `json:"domain"`
	Address   string `json:"address"`
	URI       string `json:"uri"`
	Version   string `json:"version"`
	ChainID   int64  `json:"chainId"`
	Nonce     string `json:"nonce"`
	IssuedAt  string `json:"issuedAt"`
	ExpiresAt string `json:"expiresAt,omitempty"`
}

func parseSIWE(msg string) (*siweMessage, error) {
	lines := strings.Split(strings.ReplaceAll(msg, "\r\n", "\n"), "\n")
	if len(lines) < 2 {
		return nil, errors.New("not a SIWE message")
	}
	const suffix = " wants you to sign in with your Ethereum account:"
	if !strings.HasSuffix(lines[0], suffix) {
		return nil, errors.New("first line is not a SIWE header")
	}
	m := &siweMessage{Domain: strings.TrimSuffix(lines[0], suffix), Address: strings.TrimSpace(lines[1])}
	if !common.IsHexAddress(m.Address) {
		return nil, errors.New("second line is not an address")
	}
	for _, l := range lines[2:] {
		k, v, ok := strings.Cut(l, ": ")
		if !ok {
			continue
		}
		switch k {
		case "URI":
			m.URI = v
		case "Version":
			m.Version = v
		case "Chain ID":
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return nil, errors.New("bad Chain ID")
			}
			m.ChainID = n
		case "Nonce":
			m.Nonce = v
		case "Issued At":
			m.IssuedAt = v
		case "Expiration Time":
			m.ExpiresAt = v
		}
	}
	if m.Version != "1" || m.Nonce == "" || m.IssuedAt == "" || m.ChainID == 0 {
		return nil, errors.New("missing Version, Chain ID, Nonce or Issued At")
	}
	return m, nil
}

// recoverSigner returns the address that personal_signed msg.
func recoverSigner(msg, sigHex string) (common.Address, error) {
	sig, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(sigHex), "0x"))
	if err != nil || len(sig) != 65 {
		return common.Address{}, errors.New("signature must be 65 bytes of hex")
	}
	if sig[64] >= 27 {
		sig[64] -= 27
	}
	pub, err := crypto.SigToPub(accounts.TextHash([]byte(msg)), sig)
	if err != nil {
		return common.Address{}, err
	}
	return crypto.PubkeyToAddress(*pub), nil
}

func verifySIWE(msg, sig, nonce, domain string, chain int64, now time.Time) (*siweMessage, error) {
	m, err := parseSIWE(msg)
	if err != nil {
		return nil, err
	}
	signer, err := recoverSigner(msg, sig)
	if err != nil {
		return nil, fmt.Errorf("bad signature: %w", err)
	}
	if signer != common.HexToAddress(m.Address) {
		return nil, fmt.Errorf("signed by %s, not %s", signer.Hex(), m.Address)
	}
	if nonce != "" && m.Nonce != nonce {
		return nil, errors.New("nonce does not match")
	}
	if domain != "" && m.Domain != domain {
		return nil, fmt.Errorf("domain %q, expected %q", m.Domain, domain)
	}
	if chain != 0 && m.ChainID != chain {
		return nil, fmt.Errorf("chain %d, expected %d", m.ChainID, chain)
	}
	if m.ExpiresAt != "" {
		exp, err := time.Parse(time.RFC3339, m.ExpiresAt)
		if err != nil {
			return nil, errors.New("bad Expiration Time")
		}
		if now.After(exp) {
			return nil, errors.New("message expired")
		}
	}
	m.Address = signer.Hex()
	return m, nil
}

func cmdVerifySIWE(args []string) error {
	fs := flag.NewFlagSet("verify-siwe", flag.ExitOnError)
	sig := fs.String("sig", "", "0x… personal_sign signature")
	nonce := fs.String("nonce", "", "expected nonce")
	domain := fs.String("domain", "", "expected domain (host of the sign-in page)")
	chain := fs.Int64("chain", 11155111, "expected chain ID (0 = any)")
	file := fs.String("message-file", "-", "message file, - for stdin")
	fs.Parse(args)
	var b []byte
	var err error
	if *file == "-" {
		b, err = io.ReadAll(os.Stdin)
	} else {
		b, err = os.ReadFile(*file)
	}
	if err != nil {
		return err
	}
	m, err := verifySIWE(string(b), *sig, *nonce, *domain, *chain, time.Now())
	if err != nil {
		fmt.Println("MESHY_SIWE_INVALID", err)
		os.Exit(1)
	}
	return json.NewEncoder(os.Stdout).Encode(m)
}
