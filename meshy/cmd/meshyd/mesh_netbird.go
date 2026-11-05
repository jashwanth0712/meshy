//go:build netbird

package main

// Mesh listener via NetBird's embeddable client (userspace WireGuard +
// netstack). Build with: go build -tags netbird ./cmd/meshyd
//
// meshyd then has no host-visible listener at all: SSH is only reachable over
// the WireGuard mesh, on this peer's mesh IP, port 22.

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/netbirdio/netbird/client/embed"
)

func init() {
	meshListen = func(ctx context.Context, name, state, setupKey, mgmtURL string, log *slog.Logger) (net.Listener, func(), error) {
		c, err := embed.New(embed.Options{
			DeviceName:    name,
			SetupKey:      setupKey,
			ManagementURL: mgmtURL,
			ConfigPath:    filepath.Join(state, "netbird.json"),
			StatePath:     filepath.Join(state, "netbird-state.json"),
			BlockInbound:  false, // agents must reach our SSH listener
			LogOutput:     os.Stderr,
			LogLevel:      "warn",
		})
		if err != nil {
			return nil, nil, fmt.Errorf("netbird: %w", err)
		}
		sctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		if err := c.Start(sctx); err != nil {
			return nil, nil, fmt.Errorf("netbird start: %w", err)
		}
		ln, err := c.ListenTCP(":22")
		if err != nil {
			c.Stop(context.Background())
			return nil, nil, fmt.Errorf("netbird listen: %w", err)
		}
		if st, err := c.Status(); err == nil {
			log.Info("joined mesh", "ip", st.LocalPeerState.IP, "fqdn", st.LocalPeerState.FQDN, "management", mgmtURL)
		}
		stop := func() {
			ln.Close()
			c.Stop(context.Background())
		}
		return ln, stop, nil
	}
}
