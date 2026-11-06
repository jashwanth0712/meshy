// Command meshy-sync is the Meshy provider-side service.
//
// It exposes the HTTP API that the web dashboard calls for World ID device flows,
// ENS org-tree reads, and WireGuard/NetBird mesh admission.
//
// Usage:
//
//	meshy-sync [flags]
//
// Flags (all also readable from env, see --help):
//
//	--listen          :8787
//	--sepolia         Sepolia JSON-RPC URL   (env MESHY_SEPOLIA)
//	--signer-key-file ~/.meshy/attestor.key  (env MESHY_SIGNER_KEY_FILE)
//	--world-issuer    https://sandbox.auth.world.org
//	--world-client-id                         (env WORLD_CLIENT_ID)
//	--world-client-secret                     (env WORLD_CLIENT_SECRET)
//	--dev-attest      enable /v1/agent/attest-dev (demo use only)
//	--cors            http://localhost:3000   (comma-separated allowed origins)
//	--netbird-api     https://nb.example.com  (env NETBIRD_API_URL)
//	--netbird-token                            (env NETBIRD_TOKEN)
//	--netbird-management-url
//	--from-block      12072900
package main

import (
	"context"
	"crypto/ecdsa"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/crypto"

	"github.com/jashwanth0712/meshy/meshy/provider"
)

func main() {
	fs := flag.NewFlagSet("meshy-sync", flag.ExitOnError)

	listen := fs.String("listen", env("MESHY_LISTEN", ":8787"), "listen address")
	sepolia := fs.String("sepolia", env("MESHY_SEPOLIA", ""), "Sepolia JSON-RPC URL")
	signerKeyFile := fs.String("signer-key-file", env("MESHY_SIGNER_KEY_FILE", defaultKeyFile()), "attestor private key file (hex)")
	worldIssuer := fs.String("world-issuer", env("MESHY_WORLD_ISSUER", "https://sandbox.auth.world.org"), "World ID OIDC issuer")
	worldClientID := fs.String("world-client-id", env("WORLD_CLIENT_ID", ""), "World ID OIDC client ID")
	worldClientSecret := fs.String("world-client-secret", env("WORLD_CLIENT_SECRET", ""), "World ID OIDC client secret")
	devAttest := fs.Bool("dev-attest", false, "enable /v1/agent/attest-dev (demo use only)")
	corsFlag := fs.String("cors", env("MESHY_CORS", "http://localhost:3000"), "comma-separated CORS allowed origins")
	netbirdAPI := fs.String("netbird-api", env("NETBIRD_API_URL", ""), "NetBird management API URL")
	netbirdToken := fs.String("netbird-token", env("NETBIRD_TOKEN", ""), "NetBird PAT")
	netbirdMgmt := fs.String("netbird-management-url", env("NETBIRD_MANAGEMENT_URL", ""), "NetBird management URL for peers")
	fromBlock := fs.Uint64("from-block", 12072900, "start block for ENS log scanning")

	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if *sepolia == "" {
		log.Error("--sepolia / MESHY_SEPOLIA is required")
		os.Exit(1)
	}

	signerKey, err := loadSignerKey(*signerKeyFile)
	if err != nil {
		log.Error("load signer key", "err", err, "file", *signerKeyFile)
		os.Exit(1)
	}
	log.Info("meshy-sync starting",
		"listen", *listen,
		"attestor", crypto.PubkeyToAddress(signerKey.PublicKey).Hex(),
		"dev-attest", *devAttest,
	)

	corsOrigins := strings.Split(*corsFlag, ",")
	for i, o := range corsOrigins {
		corsOrigins[i] = strings.TrimSpace(o)
	}

	cfg := provider.Config{
		Listen:            *listen,
		SepoliaRPC:        *sepolia,
		SignerKeyFile:     *signerKeyFile,
		WorldIssuer:       *worldIssuer,
		WorldClientID:     *worldClientID,
		WorldClientSecret: *worldClientSecret,
		DevAttest:         *devAttest,
		CORSOrigins:       corsOrigins,
		NetBirdAPI:        *netbirdAPI,
		NetBirdToken:      *netbirdToken,
		NetBirdMgmtURL:    *netbirdMgmt,
		FromBlock:         *fromBlock,
		Log:               log,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	srv, err := provider.NewServer(ctx, cfg, signerKey)
	cancel()
	if err != nil {
		log.Error("init server", "err", err)
		os.Exit(1)
	}

	h := srv.Handler()
	log.Info("HTTP server ready", "addr", *listen)
	if err := http.ListenAndServe(*listen, h); err != nil {
		log.Error("server exited", "err", err)
		os.Exit(1)
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func defaultKeyFile() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".meshy", "attestor.key")
}

func loadSignerKey(path string) (*ecdsa.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		// If the file doesn't exist, generate a new key and save it.
		if os.IsNotExist(err) {
			return generateAndSaveKey(path)
		}
		return nil, fmt.Errorf("read key file %s: %w", path, err)
	}
	hexStr := strings.TrimSpace(strings.TrimPrefix(string(raw), "0x"))
	k, err := crypto.HexToECDSA(hexStr)
	if err != nil {
		return nil, fmt.Errorf("parse key from %s: %w", path, err)
	}
	return k, nil
}

func generateAndSaveKey(path string) (*ecdsa.PrivateKey, error) {
	k, err := crypto.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	hexKey := fmt.Sprintf("%x", crypto.FromECDSA(k))
	if err := os.WriteFile(path, []byte(hexKey+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("save key to %s: %w", path, err)
	}
	slog.Default().Info("generated new attestor key", "path", path, "addr", crypto.PubkeyToAddress(k.PublicKey).Hex())
	return k, nil
}
