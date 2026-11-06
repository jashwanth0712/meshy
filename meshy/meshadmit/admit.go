// Package meshadmit is the client side of meshy-sync's POST /v1/admit: a
// device (meshyd) or agent (meshy) proves it holds the key in its ENS addr
// record and gets a one-off NetBird setup key for the provider's mesh.
//
// The WireGuard key itself lives with meshy-mesh (a separate Go module, see
// meshnet/), so this package shells out to `meshy-mesh keygen` for it.
package meshadmit

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Prefix must match provider.AdmitPrefix.
const Prefix = "meshy-admit:"

// Response mirrors provider.AdmitResponse (only the fields clients use).
type Response struct {
	SetupKey      string          `json:"setupKey"`
	ManagementURL string          `json:"managementUrl"`
	PeerName      string          `json:"peerName"`
	Group         string          `json:"group"`
	Attestation   json.RawMessage `json:"attestation"`
	Seats         json.RawMessage `json:"seats,omitempty"`
}

// Sign returns the EIP-191 signature meshy-sync expects.
func Sign(name, wgPub string, key *ecdsa.PrivateKey) (string, error) {
	h := accounts.TextHash([]byte(Prefix + name + ":" + wgPub))
	sig, err := crypto.Sign(h, key)
	if err != nil {
		return "", err
	}
	sig[64] += 27
	return "0x" + common.Bytes2Hex(sig), nil
}

// Admit asks the provider to admit name with the given WireGuard public key.
func Admit(ctx context.Context, provider, name, wgPub string, key *ecdsa.PrivateKey) (*Response, error) {
	sig, err := Sign(name, wgPub, key)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(map[string]string{"name": name, "wgPubKey": wgPub, "sig": sig})
	var resp *http.Response
	for attempt := 1; ; attempt++ {
		req, rerr := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(provider, "/")+"/v1/admit", bytes.NewReader(body))
		if rerr != nil {
			return nil, rerr
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err = (&http.Client{Timeout: 60 * time.Second}).Do(req)
		if err == nil {
			break
		}
		if attempt == 3 || ctx.Err() != nil {
			return nil, fmt.Errorf("MESHY_PROVIDER_UNREACHABLE %s: %w", provider, err)
		}
		time.Sleep(time.Duration(attempt) * 2 * time.Second) // admission is idempotent until the key is used
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		var e struct {
			Error string `json:"error"`
		}
		json.Unmarshal(raw, &e)
		if e.Error == "" {
			e.Error = string(raw)
		}
		return nil, fmt.Errorf("admit %s refused (HTTP %d): %s", name, resp.StatusCode, e.Error)
	}
	var out Response
	return &out, json.Unmarshal(raw, &out)
}

// MeshBin finds the meshy-mesh binary: $MESHY_MESH_BIN, next to this
// executable, or on PATH.
func MeshBin() string {
	if b := os.Getenv("MESHY_MESH_BIN"); b != "" {
		return b
	}
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "meshy-mesh")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "meshy-mesh"
}

// WGPubKey returns the WireGuard public key meshy-mesh will use for state dir.
func WGPubKey(state string) (string, error) {
	out, err := exec.Command(MeshBin(), "keygen", "--state", state).Output()
	if err != nil {
		return "", fmt.Errorf("meshy-mesh keygen: %w (install meshy-mesh from meshnet/)", err)
	}
	return strings.TrimSpace(string(out)), nil
}
