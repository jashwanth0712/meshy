package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

func signedSIWE(t *testing.T, nonce string, exp time.Time) (msg, sig, addr string) {
	k, _ := crypto.GenerateKey()
	addr = crypto.PubkeyToAddress(k.PublicKey).Hex()
	msg = fmt.Sprintf("meshy-bay.vercel.app wants you to sign in with your Ethereum account:\n%s\n\nSign in to Meshy for Mac.\n\nURI: https://meshy-bay.vercel.app/connect\nVersion: 1\nChain ID: 11155111\nNonce: %s\nIssued At: %s\nExpiration Time: %s",
		addr, nonce, time.Now().UTC().Format(time.RFC3339), exp.UTC().Format(time.RFC3339))
	s, err := crypto.Sign(accounts.TextHash([]byte(msg)), k)
	if err != nil {
		t.Fatal(err)
	}
	s[64] += 27 // wallets return v = 27/28
	return msg, hexutil.Encode(s), addr
}

func TestVerifySIWE(t *testing.T) {
	now := time.Now()
	msg, sig, addr := signedSIWE(t, "abc123", now.Add(10*time.Minute))
	m, err := verifySIWE(msg, sig, "abc123", "meshy-bay.vercel.app", 11155111, now)
	if err != nil || m.Address != addr {
		t.Fatalf("valid message rejected: %v %v", m, err)
	}
	cases := map[string]func() error{
		"wrong nonce":  func() error { _, e := verifySIWE(msg, sig, "zzz", "", 11155111, now); return e },
		"wrong domain": func() error { _, e := verifySIWE(msg, sig, "", "evil.example", 11155111, now); return e },
		"wrong chain":  func() error { _, e := verifySIWE(msg, sig, "", "", 1, now); return e },
		"expired":      func() error { _, e := verifySIWE(msg, sig, "", "", 0, now.Add(time.Hour)); return e },
		"tampered": func() error {
			_, e := verifySIWE(strings.Replace(msg, "abc123", "abc124", 1), sig, "", "", 0, now)
			return e
		},
		"bad sig": func() error { _, e := verifySIWE(msg, "0x1234", "", "", 0, now); return e },
	}
	for name, f := range cases {
		if f() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
