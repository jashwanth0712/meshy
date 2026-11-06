package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jashwanth0712/meshy/meshy/meshadmit"
)

// cmdMeshJoin puts this device on the provider's WireGuard mesh:
//
//  1. meshy-mesh keygen -> WireGuard public key
//  2. POST /v1/admit signed with the daemon key (the ENS addr record)
//  3. exec meshy-mesh serve, forwarding mesh :22 to meshyd
//
// On later runs the mesh state is reused and step 2 is skipped.
func cmdMeshJoin(args []string) error {
	fs := flag.NewFlagSet("mesh-join", flag.ExitOnError)
	var c common
	c.register(fs)
	provider := fs.String("provider", env("MESHY_PROVIDER", ""), "meshy-sync URL, e.g. http://provider:8787")
	forward := fs.String("forward", env("MESHY_MESH_FORWARD", "22=127.0.0.1:2222"), "meshPort=meshyd listen address")
	fs.Parse(args)
	if c.name == "" {
		return fmt.Errorf("--name required")
	}
	keys, err := loadOrCreateKeys(c.state, false)
	if err != nil {
		return fmt.Errorf("no daemon keys in %s (run meshyd init): %w", c.state, err)
	}
	state := filepath.Join(c.state, "mesh")
	mgmtFile := filepath.Join(state, "management")
	serve := []string{"meshy-mesh", "serve", "--state", state, "--ens", c.name, "--forward", *forward}

	if mg, err := os.ReadFile(mgmtFile); err == nil && fileExists(filepath.Join(state, "config.json")) {
		serve = append(serve, "--management", strings.TrimSpace(string(mg)))
	} else {
		if *provider == "" {
			return fmt.Errorf("--provider required for the first mesh join")
		}
		pub, err := meshadmit.WGPubKey(state)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		resp, err := meshadmit.Admit(ctx, *provider, c.name, pub, keys.Eth)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "admitted %s as mesh peer %s (group %s)\nattestation %s\n", c.name, resp.PeerName, resp.Group, resp.Attestation)
		if err := os.WriteFile(mgmtFile, []byte(resp.ManagementURL+"\n"), 0o600); err != nil {
			return err
		}
		serve = append(serve, "--management", resp.ManagementURL, "--setup-key", resp.SetupKey)
	}
	bin, err := exec.LookPath(meshadmit.MeshBin())
	if err != nil {
		return fmt.Errorf("meshy-mesh not found: %w", err)
	}
	return syscall.Exec(bin, serve, os.Environ())
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
