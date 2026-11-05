package policy

import (
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

// Envelope is the multi-recipient ECIES ciphertext stored in `meshy.policy`.
//
// Scheme: a random 32-byte content key encrypts the policy JSON with
// XChaCha20-Poly1305 (AAD = grant name). For each recipient the content key is
// wrapped with X25519(ephemeral, recipient) -> HKDF-SHA256 -> XChaCha20-Poly1305.
// Recipients are the resource daemon and the org key, so only they (not the
// provider, not the public) can read which folders an agent may touch.
type Envelope struct {
	V          int         `json:"v"`
	Alg        string      `json:"alg"`
	Recipients []Recipient `json:"r"`
	Nonce      string      `json:"n"`
	Ciphertext string      `json:"c"`
}

// Recipient is one wrapped copy of the content key.
type Recipient struct {
	KID        string `json:"kid"` // first 8 bytes of sha256(pubkey), hex
	Ephemeral  string `json:"epk"`
	Nonce      string `json:"n"`
	WrappedKey string `json:"wk"`
}

const alg = "x25519-hkdf-sha256-xchacha20poly1305"

var (
	ErrNotRecipient = errors.New("policy: key is not a recipient of this envelope")
	ErrEnvelope     = errors.New("policy: malformed envelope")
)

var b64 = base64.RawStdEncoding

// GenerateKey creates a new X25519 key pair for policy encryption.
func GenerateKey() (*ecdh.PrivateKey, error) { return ecdh.X25519().GenerateKey(rand.Reader) }

// ParsePublicKey decodes a base64 X25519 public key (as stored in ENS).
func ParsePublicKey(s string) (*ecdh.PublicKey, error) {
	raw, err := b64.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("policy: public key: %w", err)
	}
	return ecdh.X25519().NewPublicKey(raw)
}

// ParsePrivateKey decodes a base64 X25519 private key.
func ParsePrivateKey(s string) (*ecdh.PrivateKey, error) {
	raw, err := b64.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("policy: private key: %w", err)
	}
	return ecdh.X25519().NewPrivateKey(raw)
}

// EncodePublicKey renders a public key for ENS text records.
func EncodePublicKey(k *ecdh.PublicKey) string { return b64.EncodeToString(k.Bytes()) }

// EncodePrivateKey renders a private key for local storage.
func EncodePrivateKey(k *ecdh.PrivateKey) string { return b64.EncodeToString(k.Bytes()) }

// KeyID returns the short id used to find our recipient entry.
func KeyID(k *ecdh.PublicKey) string {
	h := sha256.Sum256(k.Bytes())
	return fmt.Sprintf("%x", h[:8])
}

// Seal validates the policy and encrypts it to every recipient. The returned
// string is what goes into the ENS text record.
func Seal(p *Policy, recipients ...*ecdh.PublicKey) (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	if len(recipients) == 0 {
		return "", errors.New("policy: no recipients")
	}
	plain, err := p.Marshal()
	if err != nil {
		return "", err
	}
	cek := make([]byte, chacha20poly1305.KeySize)
	if _, err := rand.Read(cek); err != nil {
		return "", err
	}
	aead, _ := chacha20poly1305.NewX(cek)
	nonce := randNonce()
	env := Envelope{
		V:          1,
		Alg:        alg,
		Nonce:      b64.EncodeToString(nonce),
		Ciphertext: b64.EncodeToString(aead.Seal(nil, nonce, plain, []byte(p.Grant))),
	}
	for _, pub := range recipients {
		eph, err := GenerateKey()
		if err != nil {
			return "", err
		}
		kek, err := deriveKEK(eph, pub, eph.PublicKey(), pub)
		if err != nil {
			return "", err
		}
		wrap, _ := chacha20poly1305.NewX(kek)
		wn := randNonce()
		env.Recipients = append(env.Recipients, Recipient{
			KID:        KeyID(pub),
			Ephemeral:  b64.EncodeToString(eph.PublicKey().Bytes()),
			Nonce:      b64.EncodeToString(wn),
			WrappedKey: b64.EncodeToString(wrap.Seal(nil, wn, cek, []byte(p.Grant))),
		})
	}
	out, err := json.Marshal(env)
	return string(out), err
}

// Open decrypts an envelope with priv. grant must be the ENS name the record
// was read from; a mismatch (record copied from another grant) fails AEAD.
func Open(record string, grant string, priv *ecdh.PrivateKey) (*Policy, error) {
	var env Envelope
	if err := json.Unmarshal([]byte(record), &env); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEnvelope, err)
	}
	if env.V != 1 || env.Alg != alg {
		return nil, fmt.Errorf("%w: unsupported v=%d alg=%q", ErrEnvelope, env.V, env.Alg)
	}
	kid := KeyID(priv.PublicKey())
	for _, r := range env.Recipients {
		if r.KID != kid {
			continue
		}
		epkRaw, err1 := b64.DecodeString(r.Ephemeral)
		wn, err2 := b64.DecodeString(r.Nonce)
		wk, err3 := b64.DecodeString(r.WrappedKey)
		if err := errors.Join(err1, err2, err3); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrEnvelope, err)
		}
		epk, err := ecdh.X25519().NewPublicKey(epkRaw)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrEnvelope, err)
		}
		kek, err := deriveKEK(priv, epk, epk, priv.PublicKey())
		if err != nil {
			return nil, err
		}
		wrap, _ := chacha20poly1305.NewX(kek)
		cek, err := wrap.Open(nil, wn, wk, []byte(grant))
		if err != nil {
			return nil, fmt.Errorf("policy: unwrap failed (wrong grant name or key): %w", err)
		}
		n, err1 := b64.DecodeString(env.Nonce)
		ct, err2 := b64.DecodeString(env.Ciphertext)
		if err := errors.Join(err1, err2); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrEnvelope, err)
		}
		aead, _ := chacha20poly1305.NewX(cek)
		plain, err := aead.Open(nil, n, ct, []byte(grant))
		if err != nil {
			return nil, fmt.Errorf("policy: decrypt failed: %w", err)
		}
		p, err := Unmarshal(plain)
		if err != nil {
			return nil, err
		}
		if p.Grant != grant {
			return nil, fmt.Errorf("policy: grant mismatch: record for %q read from %q", p.Grant, grant)
		}
		return p, nil
	}
	return nil, ErrNotRecipient
}

// deriveKEK computes HKDF(X25519(priv, peer), info = epk || recipientPub).
func deriveKEK(priv *ecdh.PrivateKey, peer *ecdh.PublicKey, epk, rcpt *ecdh.PublicKey) ([]byte, error) {
	shared, err := priv.ECDH(peer)
	if err != nil {
		return nil, err
	}
	info := append(append([]byte("meshy-policy-v1"), epk.Bytes()...), rcpt.Bytes()...)
	kek := make([]byte, chacha20poly1305.KeySize)
	if _, err := io.ReadFull(hkdf.New(sha256.New, shared, nil, info), kek); err != nil {
		return nil, err
	}
	return kek, nil
}

func randNonce() []byte {
	n := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(n); err != nil {
		panic(err)
	}
	return n
}
