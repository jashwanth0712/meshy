package main

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jashwanth0712/meshy/meshy/policy"
)

// Keys are the daemon's identities, stored in the state dir.
type Keys struct {
	Policy *ecdh.PrivateKey  // decrypts meshy.policy (pub -> meshy.enckey)
	Host   gossh.Signer      // SSH host key (pub -> meshy.ssh-host)
	Eth    *ecdsa.PrivateKey // daemon address (addr record) + Arkiv writer
}

func loadOrCreateKeys(dir string, create bool) (*Keys, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	k := &Keys{}
	// Policy key.
	pp := filepath.Join(dir, "policy.key")
	if b, err := os.ReadFile(pp); err == nil {
		if k.Policy, err = policy.ParsePrivateKey(strings.TrimSpace(string(b))); err != nil {
			return nil, err
		}
	} else if create {
		k.Policy, _ = policy.GenerateKey()
		if err := os.WriteFile(pp, []byte(policy.EncodePrivateKey(k.Policy)+"\n"), 0o600); err != nil {
			return nil, err
		}
	} else {
		return nil, missing(pp)
	}
	// SSH host key.
	hp := filepath.Join(dir, "host_ed25519")
	if b, err := os.ReadFile(hp); err == nil {
		if k.Host, err = gossh.ParsePrivateKey(b); err != nil {
			return nil, err
		}
	} else if create {
		_, priv, _ := ed25519.GenerateKey(rand.Reader)
		blk, err := gossh.MarshalPrivateKey(priv, "meshyd host key")
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(hp, pem.EncodeToMemory(blk), 0o600); err != nil {
			return nil, err
		}
		k.Host, _ = gossh.NewSignerFromKey(priv)
	} else {
		return nil, missing(hp)
	}
	// Ethereum key.
	ep := filepath.Join(dir, "eth.key")
	if b, err := os.ReadFile(ep); err == nil {
		if k.Eth, err = crypto.HexToECDSA(strings.TrimPrefix(strings.TrimSpace(string(b)), "0x")); err != nil {
			return nil, err
		}
	} else if create {
		k.Eth, _ = crypto.GenerateKey()
		if err := os.WriteFile(ep, []byte(fmt.Sprintf("%x\n", crypto.FromECDSA(k.Eth))), 0o600); err != nil {
			return nil, err
		}
	} else {
		return nil, missing(ep)
	}
	return k, nil
}

func missing(p string) error {
	return errors.New("missing " + p + " (run `meshyd init` first)")
}
