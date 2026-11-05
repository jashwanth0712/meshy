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
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/netbirdio/netbird/client/embed"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

type opts struct {
	setupKey, mgmt, state, name string
}

func (o *opts) register(fs *flag.FlagSet) {
	fs.StringVar(&o.setupKey, "setup-key", env("MESHY_SETUP_KEY", ""), "setup key issued by the provider (meshy-sync /v1/admit)")
	fs.StringVar(&o.mgmt, "management", env("MESHY_MANAGEMENT_URL", ""), "provider management URL")
	h, _ := os.UserHomeDir()
	fs.StringVar(&o.state, "state", env("MESHY_MESH_STATE", filepath.Join(h, ".meshy", "mesh")), "state dir (WireGuard key, peer config)")
	host, _ := os.Hostname()
	fs.StringVar(&o.name, "name", env("MESHY_DEVICE_NAME", host), "device name on the mesh")
}

func (o *opts) start(ctx context.Context, quiet bool) (*embed.Client, error) {
	if o.mgmt == "" {
		return nil, errors.New("--management required")
	}
	if err := os.MkdirAll(o.state, 0o700); err != nil {
		return nil, err
	}
	var logOut io.Writer = os.Stderr
	if quiet {
		logOut = io.Discard
	}
	c, err := embed.New(embed.Options{
		DeviceName:    o.name,
		SetupKey:      o.setupKey,
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

func main() {
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
	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	conn, err := c.DialContext(dctx, "tcp", net.JoinHostPort(fs.Arg(0), fs.Arg(1)))
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
