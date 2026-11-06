// Command meshyd is Meshy's per-resource daemon.
//
//	meshyd init  --name db1.prod.acme.eth --roots /srv        # keys + ENS records to set
//	meshyd run   --name db1.prod.acme.eth --roots /srv --sepolia wss://...
//	meshyd top [--demo] | status [-w] | sessions | kill <id> | logs -f | doctor
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jashwanth0712/meshy/meshy/arkivkit"
	"github.com/jashwanth0712/meshy/meshy/audit"
	"github.com/jashwanth0712/meshy/meshy/enskit"
	"github.com/jashwanth0712/meshy/meshy/meshyd"
	"github.com/jashwanth0712/meshy/meshy/policy"
	"github.com/jashwanth0712/meshy/meshy/tui"
	"github.com/jashwanth0712/meshy/meshy/worldid"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func defaultState() string {
	if os.Geteuid() == 0 {
		return "/var/lib/meshy"
	}
	h, _ := os.UserHomeDir()
	return h + "/.meshy/meshyd"
}

func defaultControl() string {
	if os.Geteuid() == 0 {
		return "/var/run/meshyd.sock"
	}
	return os.TempDir() + "/meshyd.sock"
}

type common struct {
	name, state, roots, control string
}

func (c *common) register(fs *flag.FlagSet) {
	fs.StringVar(&c.name, "name", env("MESHY_NAME", ""), "ENS name of this resource, e.g. db1.prod.acme.eth")
	fs.StringVar(&c.state, "state", env("MESHY_STATE", defaultState()), "state dir for keys")
	fs.StringVar(&c.roots, "roots", env("MESHY_ROOTS", "/srv"), "comma-separated data roots agents may be granted")
	fs.StringVar(&c.control, "control", env("MESHY_CONTROL", defaultControl()), "control socket")
}

func (c *common) rootList() []string {
	var out []string
	for _, r := range strings.Split(c.roots, ",") {
		if r = strings.TrimSpace(r); r != "" {
			out = append(out, r)
		}
	}
	return out
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "init":
		err = cmdInit(os.Args[2:])
	case "run":
		err = cmdRun(os.Args[2:])
	case "status":
		err = cmdStatus(os.Args[2:])
	case "top":
		err = cmdTop(os.Args[2:])
	case "sessions":
		err = cmdSessions(os.Args[2:])
	case "kill":
		err = cmdKill(os.Args[2:])
	case "logs":
		err = cmdLogs(os.Args[2:])
	case "doctor":
		err = cmdDoctor(os.Args[2:])
	case "mesh-join":
		err = cmdMeshJoin(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "meshyd:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `meshyd — Meshy resource daemon

  meshyd init     --name <ens> [--roots /srv] [--endpoint host:port]
  meshyd run      --name <ens> [--roots /srv] --sepolia <ws/http url> [--listen :22]
  meshyd top      [--demo] [--json]  mission-control dashboard (topology, grants, sessions, chain, audit)
  meshyd status   [-w] [-plain]  status; -w opens the dashboard on a terminal (-plain: old view)
  meshyd sessions               list active sessions
  meshyd kill     <session-id>  terminate a session now
  meshyd logs     [-f]          audit events
  meshyd doctor                 check sandbox, chain, keys
  meshyd mesh-join --provider <meshy-sync url>
                                join the provider's WireGuard mesh (admit + meshy-mesh serve)
`)
	os.Exit(2)
}

// ---- init ----

// Join is what an admin pastes into the dashboard ("Add resource") to
// register this resource in ENS.
type Join struct {
	Name     string   `json:"name"`
	Addr     string   `json:"addr"`
	EncKey   string   `json:"meshy.enckey"`
	SSHHost  string   `json:"meshy.ssh-host"`
	Roots    []string `json:"meshy.roots"`
	Endpoint string   `json:"meshy.endpoint,omitempty"`
}

func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	var c common
	c.register(fs)
	endpoint := fs.String("endpoint", env("MESHY_ENDPOINT", ""), "direct host:port for agents (dev / before mesh DNS)")
	fs.Parse(args)
	if c.name == "" {
		return errors.New("--name is required")
	}
	k, err := loadOrCreateKeys(c.state, true)
	if err != nil {
		return err
	}
	j := Join{
		Name: c.name, Addr: crypto.PubkeyToAddress(k.Eth.PublicKey).Hex(),
		EncKey:  policy.EncodePublicKey(k.Policy.PublicKey()),
		SSHHost: strings.TrimSpace(string(gossh.MarshalAuthorizedKey(k.Host.PublicKey()))),
		Roots:   c.rootList(), Endpoint: *endpoint,
	}
	tui.PrintLogo(os.Stderr)
	b, _ := json.MarshalIndent(j, "", "  ")
	code, _ := json.Marshal(j)
	fmt.Printf("Keys in %s\n\nENS records to set on %s:\n%s\n\nJoin code (paste into the dashboard → Add resource):\n  meshy-join:%s\n",
		c.state, c.name, b, base64.RawURLEncoding.EncodeToString(code))
	return nil
}

// ---- run ----

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	var c common
	c.register(fs)
	sepolia := fs.String("sepolia", env("MESHY_SEPOLIA", ""), "Sepolia RPC (wss:// preferred for fast revocation)")
	helper := fs.String("ens-helper", env("MESHY_ENS_HELPER", enskit.SepoliaUniversalHelper.Hex()), "ENSv2 UniversalHelper address")
	listen := fs.String("listen", env("MESHY_LISTEN", ":2222"), "SSH listen address (mesh IP:22 in production)")
	arkivRPC := fs.String("arkiv", env("MESHY_ARKIV_RPC", arkivkit.RabdiRPC), "Arkiv RPC URL (\"\" disables)")
	requireHuman := fs.Bool("require-human", env("MESHY_REQUIRE_HUMAN", "true") == "true", "require World ID attestation for agents")
	unsafe := fs.Bool("unsafe-no-sandbox", false, "run sessions without confinement (dev only)")
	maxHeadAge := fs.Duration("max-head-age", 45*time.Second, "fail closed if no new block for this long")
	fs.Parse(args)
	if c.name == "" || *sepolia == "" {
		return errors.New("--name and --sepolia are required")
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	keys, err := loadOrCreateKeys(c.state, false)
	if err != nil {
		return err
	}
	eth, err := ethclient.DialContext(ctx, *sepolia)
	if err != nil {
		return fmt.Errorf("sepolia: %w", err)
	}
	ens := enskit.NewClient(eth, commonAddr(*helper))
	resource := enskit.Name(c.name)

	// Sandbox.
	var sb meshyd.Sandbox
	if *unsafe {
		log.Warn("RUNNING WITHOUT A SANDBOX (--unsafe-no-sandbox): agents can read the whole host")
		sb = meshyd.UnsafeSandbox{}
	} else {
		if sb, err = platformSandbox(ctx, c.rootList(), c.state); err != nil {
			return err
		}
		if err := sb.Check(); err != nil {
			return fmt.Errorf("sandbox %s unavailable: %w (or pass --unsafe-no-sandbox for dev)", sb.Name(), err)
		}
	}

	// Audit sinks.
	ring := &audit.Ring{N: 1000}
	sinks := audit.Multi{ring, &audit.JSONL{W: os.Stdout}}
	if *arkivRPC != "" {
		w, err := arkivkit.Dial(ctx, *arkivRPC, keys.Eth)
		if err != nil {
			log.Error("arkiv disabled", "err", err)
		} else {
			b := &arkivkit.Batcher{Exec: w, Org: string(resource.Org()), Resource: c.name, Log: log}
			if k, err := orgKey(ctx, ens, resource.Org()); err != nil {
				log.Warn("org has no meshy.orgkey: Arkiv payloads will be UNENCRYPTED", "err", err)
			} else {
				b.OrgKey = k
			}
			go b.Run(ctx)
			sinks = append(sinks, b)
			log.Info("arkiv audit enabled", "writer", w.From().Hex(), "rpc", *arkivRPC)
		}
	}

	// Chain watcher.
	var heads enskit.HeadSource = enskit.PollHeads{Client: eth, Interval: time.Second}
	if strings.HasPrefix(*sepolia, "ws") {
		heads = enskit.WSHeads{URL: *sepolia, Log: log}
	}
	srv := &meshyd.Server{Resource: c.name, HostKey: keys.Host, Sandbox: sb, Audit: sinks, Log: log}
	watcher := &enskit.Watcher{Heads: heads, Checker: ens, MaxHeadAge: *maxHeadAge, OnRevoke: srv.Revoke, Log: log}
	srv.Watcher = watcher
	authz := &meshyd.ENSAuthorizer{ENS: ens, Resource: resource, Key: keys.Policy}
	if *requireHuman {
		authz.Human = &worldid.Verifier{Trusted: worldid.ENSTrusted(ens)}
	} else {
		log.Warn("World ID attestation NOT required (--require-human=false)")
	}
	srv.Auth = authz
	go func() {
		if err := watcher.Run(ctx); err != nil && ctx.Err() == nil {
			log.Error("watcher stopped", "err", err)
		}
	}()

	// Control socket.
	ctl, err := serveControl(c.control, srv, watcher, ring, sb.Name(), keys)
	if err != nil {
		return err
	}
	defer ctl.Close()

	// On the mesh, meshy-mesh forwards the mesh IP :22 to this listener
	// (keep it on 127.0.0.1 so SSH is reachable only through the mesh).
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	log.Info("meshyd ready", "resource", c.name, "listen", ln.Addr().String(), "sandbox", sb.Name(),
		"addr", crypto.PubkeyToAddress(keys.Eth.PublicKey).Hex(), "control", c.control)
	go func() { <-ctx.Done(); srv.Close(); ln.Close() }()
	if err := srv.Serve(ln); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

func orgKey(ctx context.Context, ens *enskit.Client, org enskit.Name) (*ecdhPub, error) {
	r, err := ens.Lookup(ctx, org)
	if err != nil {
		return nil, err
	}
	s, err := r.Text(ctx, ens, "meshy.orgkey")
	if err != nil {
		return nil, err
	}
	if s == "" {
		return nil, errors.New("record empty")
	}
	return policy.ParsePublicKey(s)
}
