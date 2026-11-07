// Command meshy-mesh joins a Meshy provider's WireGuard mesh in userspace
// (NetBird client/embed: no TUN, no NET_ADMIN) and bridges TCP between the
// mesh and localhost.
//
// It lives in its own Go module because NetBird and go-ethereum (used by
// meshyd) require incompatible pion/stun versions.
//
//	# on a resource, next to meshyd (which listens on 127.0.0.1:2222):
//	meshy-mesh serve --setup-key KEY --management https://provider --forward 22=127.0.0.1:2222
//
//	# on an agent, as an ssh ProxyCommand (the agent is its own mesh peer):
//	meshy-mesh proxy --setup-key KEY --management https://provider db1.prod.acme.eth 22
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/netbirdio/netbird/client/embed"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

type opts struct {
	setupKey, mgmt, state, name, ens string
}

func (o *opts) register(fs *flag.FlagSet) {
	fs.StringVar(&o.setupKey, "setup-key", env("MESHY_SETUP_KEY", ""), "setup key issued by the provider (meshy-sync /v1/admit)")
	fs.StringVar(&o.mgmt, "management", env("MESHY_MANAGEMENT_URL", ""), "provider management URL")
	h, _ := os.UserHomeDir()
	fs.StringVar(&o.state, "state", env("MESHY_MESH_STATE", filepath.Join(h, ".meshy", "mesh")), "state dir (WireGuard key, peer config)")
	fs.StringVar(&o.ens, "ens", env("MESHY_ENS_NAME", ""), "this peer's ENS name; the mesh peer name becomes <ens with dots as dashes>")
	fs.StringVar(&o.name, "name", env("MESHY_DEVICE_NAME", ""), "device name on the mesh (default: derived from --ens, else hostname)")
}

// peerName is the mesh peer name for an ENS name; meshy-sync groups and
// policies match peers by exactly this name.
func peerName(ens string) string { return strings.ReplaceAll(strings.ToLower(ens), ".", "-") }

// wgKey returns this peer's WireGuard private key. NetBird keeps it in
// config.json ("PrivateKey") and only generates one when that is empty, so
// we seed config.json ourselves: the key exists before the first join and its
// public half can be signed into the /v1/admit request.
func wgKey(state string, create bool) (wgtypes.Key, error) {
	p := filepath.Join(state, "config.json")
	if b, err := os.ReadFile(p); err == nil {
		var c struct{ PrivateKey string }
		if json.Unmarshal(b, &c) == nil && c.PrivateKey != "" {
			return wgtypes.ParseKey(c.PrivateKey)
		}
	}
	if !create {
		return wgtypes.Key{}, errors.New("no WireGuard key in " + p + " (run meshy-mesh keygen)")
	}
	k, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return k, err
	}
	if err := os.MkdirAll(state, 0o700); err != nil {
		return k, err
	}
	seed, _ := json.Marshal(map[string]string{"PrivateKey": k.String()})
	return k, os.WriteFile(p, seed, 0o600)
}

func (o *opts) start(ctx context.Context, quiet bool) (*embed.Client, error) {
	if o.mgmt == "" {
		return nil, errors.New("--management required")
	}
	if err := os.MkdirAll(o.state, 0o700); err != nil {
		return nil, err
	}
	if o.name == "" && o.ens != "" {
		o.name = peerName(o.ens)
	}
	if o.name == "" {
		o.name, _ = os.Hostname()
	}
	key, err := wgKey(o.state, true)
	if err != nil {
		return nil, err
	}
	// NetBird wants exactly one credential: the one-off setup key on the
	// first join, the (already registered) WireGuard key afterwards.
	priv := ""
	if o.setupKey == "" {
		priv = key.String()
	}
	var logOut io.Writer = os.Stderr
	if quiet {
		logOut = io.Discard
	}
	c, err := embed.New(embed.Options{
		DeviceName:    o.name,
		SetupKey:      o.setupKey,
		PrivateKey:    priv,
		ManagementURL: o.mgmt,
		ConfigPath:    filepath.Join(o.state, "config.json"),
		StatePath:     filepath.Join(o.state, "state.json"),
		LogOutput:     logOut,
		LogLevel:      "warn",
	})
	if err != nil {
		return nil, err
	}
	sctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if err := c.Start(sctx); err != nil {
		return nil, fmt.Errorf("join mesh: %w", err)
	}
	return c, nil
}

// version is set at release time: -ldflags "-X main.version=v0.3.2" (scripts/release.sh).
var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-v", "--version", "version":
			fmt.Printf("meshy-mesh %s %s/%s\n", version, runtime.GOOS, runtime.GOARCH)
			return
		}
	}
	if len(os.Args) < 2 {
		usage()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve(ctx, os.Args[2:])
	case "proxy":
		err = proxy(ctx, os.Args[2:])
	case "status":
		err = status(ctx, os.Args[2:])
	case "keygen":
		err = keygen(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "meshy-mesh:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `meshy-mesh — join a Meshy mesh in userspace and bridge TCP

  meshy-mesh serve  [--setup-key K] --management URL --forward 22=127.0.0.1:2222 [--forward ...]
  meshy-mesh proxy  [--setup-key K] --management URL <host> <port>     (ssh ProxyCommand)
  meshy-mesh status [--setup-key K] --management URL
  meshy-mesh keygen [--state DIR]      print this peer's WireGuard public key (creates it)

  <host> may be an ENS name (db1.prod.acme.eth): it is resolved to the mesh IP
  of the peer that meshy-sync admitted under that name.
`)
	os.Exit(2)
}

type forwards []string

func (f *forwards) String() string     { return strings.Join(*f, ",") }
func (f *forwards) Set(v string) error { *f = append(*f, v); return nil }

func serve(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	var o opts
	o.register(fs)
	var fw forwards
	fs.Var(&fw, "forward", "meshPort=localHost:port (repeatable), e.g. 22=127.0.0.1:2222")
	fs.Parse(args)
	if len(fw) == 0 {
		fw = forwards{"22=127.0.0.1:2222"}
	}
	c, err := o.start(ctx, false)
	if err != nil {
		return err
	}
	defer c.Stop(context.Background())
	if st, err := c.Status(); err == nil {
		log.Printf("on mesh as %s (%s)", st.LocalPeerState.FQDN, st.LocalPeerState.IP)
	}
	var wg sync.WaitGroup
	for _, f := range fw {
		port, target, ok := strings.Cut(f, "=")
		if !ok {
			return fmt.Errorf("bad --forward %q", f)
		}
		ln, err := c.ListenTCP(":" + port)
		if err != nil {
			return fmt.Errorf("listen mesh :%s: %w", port, err)
		}
		log.Printf("forwarding mesh :%s -> %s", port, target)
		wg.Add(1)
		go func() {
			defer wg.Done()
			go func() { <-ctx.Done(); ln.Close() }()
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				go func() {
					defer conn.Close()
					up, err := net.DialTimeout("tcp", target, 5*time.Second)
					if err != nil {
						log.Printf("forward %s: %v", target, err)
						return
					}
					defer up.Close()
					pipe(conn, up)
				}()
			}
		}()
	}
	wg.Wait()
	return nil
}

func proxy(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("proxy", flag.ExitOnError)
	var o opts
	o.register(fs)
	fs.Parse(args)
	if fs.NArg() != 2 {
		return errors.New("usage: meshy-mesh proxy <host> <port>")
	}
	c, err := o.start(ctx, true)
	if err != nil {
		return err
	}
	defer c.Stop(context.Background())
	host, err := resolvePeer(ctx, c, fs.Arg(0))
	if err != nil {
		return err
	}
	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	conn, err := c.DialContext(dctx, "tcp", net.JoinHostPort(host, fs.Arg(1)))
	if err != nil {
		return fmt.Errorf("MESHY_UNREACHABLE %s:%s over mesh: %w", fs.Arg(0), fs.Arg(1), err)
	}
	defer conn.Close()
	pipe(stdio{}, conn)
	return nil
}

func status(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	var o opts
	o.register(fs)
	fs.Parse(args)
	c, err := o.start(ctx, true)
	if err != nil {
		return err
	}
	defer c.Stop(context.Background())
	st, err := c.Status()
	if err != nil {
		return err
	}
	fmt.Printf("me     %s  %s\n", st.LocalPeerState.FQDN, st.LocalPeerState.IP)
	for _, p := range st.Peers {
		fmt.Printf("peer   %-40s %-16s %s\n", p.FQDN, p.IP, p.ConnStatus)
	}
	return nil
}

// resolvePeer maps an ENS name to the mesh IP of the peer admitted under it.
// The peer only shows up once a policy (= a live ENS grant) lets us reach it,
// so "not found" means no grant or the device is offline.
func resolvePeer(ctx context.Context, c *embed.Client, host string) (string, error) {
	if net.ParseIP(host) != nil || !strings.Contains(host, ".") {
		return host, nil
	}
	want := peerName(host)
	deadline := time.Now().Add(20 * time.Second)
	for {
		if st, err := c.Status(); err == nil {
			// A device that re-joined the mesh gets a renamed DNS label (want-1, want-2…);
			// the newest (highest suffix) is the live one.
			best, bestN := "", -1
			for _, p := range st.Peers {
				label, _, _ := strings.Cut(p.FQDN, ".")
				if n, ok := twinIndex(label, want); ok && n > bestN {
					best, bestN = p.IP, n
				}
			}
			if best != "" {
				return best, nil
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("MESHY_UNREACHABLE %s: no mesh peer %q visible (no live grant, or the device is offline)", host, want)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func keygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	var o opts
	o.register(fs)
	fs.Parse(args)
	k, err := wgKey(o.state, true)
	if err != nil {
		return err
	}
	fmt.Println(k.PublicKey().String())
	return nil
}

type stdio struct{}

func (stdio) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdio) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdio) Close() error                { return nil }

func pipe(a, b io.ReadWriteCloser) {
	done := make(chan struct{}, 2)
	go func() { io.Copy(a, b); done <- struct{}{} }()
	go func() { io.Copy(b, a); done <- struct{}{} }()
	<-done
}

// twinIndex reports whether label is want (0) or a renamed twin want-N (N).
func twinIndex(label, want string) (int, bool) {
	if label == want {
		return 0, true
	}
	rest, ok := strings.CutPrefix(label, want+"-")
	if !ok || rest == "" {
		return 0, false
	}
	n := 0
	for _, c := range rest {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}
