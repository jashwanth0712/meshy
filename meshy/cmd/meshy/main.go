// Command meshy is the agent/human CLI.
//
//	meshy join <invite-url>               generate keys, open approval page, wait for ENS
//	meshy whoami                          show this agent's identity and records
//	meshy top [--demo]                    live mission-control dashboard (JSON when piped)
//	meshy grants <resource>...            grant status (expiry countdown) per resource
//	meshy ssh <resource> [--] <command>   run a command on a resource (ENS-pinned host key)
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jashwanth0712/meshy/meshy/enskit"
	"github.com/jashwanth0712/meshy/meshy/tui"
)

const defaultRPC = "https://ethereum-sepolia-rpc.publicnode.com"

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// Identity is persisted in ~/.meshy/agent.json (+ key files).
type Identity struct {
	Agent string `json:"agent"` // label, e.g. claude-1
	Org   string `json:"org"`   // acme.eth
	// Kind is "" (an agent, <label>.agents.<org>) or "person" (a member,
	// <label>.people.<org>, joined with an invite from the People page).
	Kind string `json:"kind,omitempty"`
	Addr string `json:"addr"`
	SSH  string `json:"ssh"` // authorized_keys line
	// Mesh is the provider management URL once `meshy mesh-join` succeeded;
	// resources are then dialled by ENS name over the WireGuard mesh.
	Mesh string `json:"mesh,omitempty"`
	dir  string
	key  gossh.Signer
	eth  *ecdsa.PrivateKey
}

func (i *Identity) Name() enskit.Name {
	if i.Kind == "person" {
		return enskit.Name(i.Agent + ".people." + i.Org)
	}
	return enskit.Name(i.Agent + ".agents." + i.Org)
}

func home() string {
	if d := os.Getenv("MESHY_HOME"); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".meshy")
}

func loadIdentity() (*Identity, error) { return loadIdentityFrom(home()) }

// loadIdentityFor picks the identity for the org that name belongs to: the one
// in MESHY_HOME if set; else ~/.meshy if its org matches; else any
// ~/.meshy/person-*.<org> created by `meshy login` or `meshy join-org`.
func loadIdentityFor(name enskit.Name) (*Identity, error) {
	if os.Getenv("MESHY_HOME") != "" {
		return loadIdentity()
	}
	org := string(name.Org())
	def, defErr := loadIdentity()
	if defErr == nil && def.Org == org {
		return def, nil
	}
	h, _ := os.UserHomeDir()
	dirs, _ := filepath.Glob(filepath.Join(h, ".meshy", "person-*"))
	for _, d := range dirs {
		if id, err := loadIdentityFrom(d); err == nil && id.Org == org {
			return id, nil
		}
	}
	if defErr == nil {
		return def, nil // let the daemon say MESHY_UNKNOWN_AGENT / MESHY_NO_GRANT
	}
	return nil, fmt.Errorf("no Meshy identity for %s on this machine: run `meshy login` (or `meshy join <invite-link>`)", org)
}

func loadIdentityFrom(dir string) (*Identity, error) {
	b, err := os.ReadFile(filepath.Join(dir, "agent.json"))
	if err != nil {
		return nil, errors.New("no Meshy identity yet: run `meshy join <invite-link>`")
	}
	var id Identity
	if err := json.Unmarshal(b, &id); err != nil {
		return nil, err
	}
	id.dir = dir
	kb, err := os.ReadFile(filepath.Join(dir, "id_ed25519"))
	if err != nil {
		return nil, err
	}
	if id.key, err = gossh.ParsePrivateKey(kb); err != nil {
		return nil, err
	}
	eb, err := os.ReadFile(filepath.Join(dir, "eth.key"))
	if err != nil {
		return nil, err
	}
	id.eth, err = crypto.HexToECDSA(strings.TrimSpace(string(eb)))
	return &id, err
}

func createIdentity(agent, org string) (*Identity, error) {
	dir := home()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if id, err := loadIdentity(); err == nil {
		if id.Agent == agent && id.Org == org {
			return id, nil // re-join with the same keys
		}
		return nil, fmt.Errorf("an identity for %s already exists in %s (set MESHY_HOME for another agent)", id.Name(), dir)
	}
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	blk, err := gossh.MarshalPrivateKey(priv, agent+".agents."+org)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "id_ed25519"), pem.EncodeToMemory(blk), 0o600); err != nil {
		return nil, err
	}
	signer, _ := gossh.NewSignerFromKey(priv)
	ek, _ := crypto.GenerateKey()
	if err := os.WriteFile(filepath.Join(dir, "eth.key"), []byte(fmt.Sprintf("%x\n", crypto.FromECDSA(ek))), 0o600); err != nil {
		return nil, err
	}
	id := &Identity{
		Agent: agent, Org: org, Addr: crypto.PubkeyToAddress(ek.PublicKey).Hex(),
		SSH: strings.TrimSpace(string(gossh.MarshalAuthorizedKey(signer.PublicKey()))),
		dir: dir, key: signer, eth: ek,
	}
	b, _ := json.MarshalIndent(id, "", "  ")
	return id, os.WriteFile(filepath.Join(dir, "agent.json"), b, 0o600)
}

func ensClient(ctx context.Context) (*enskit.Client, error) {
	c, err := ethclient.DialContext(ctx, env("MESHY_SEPOLIA", defaultRPC))
	if err != nil {
		return nil, err
	}
	return enskit.NewClient(c, common.HexToAddress(env("MESHY_ENS_HELPER", enskit.SepoliaUniversalHelper.Hex()))), nil
}

// version is set at release time: -ldflags "-X main.version=v0.3.2" (scripts/release.sh).
var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-v", "--version", "version":
			fmt.Printf("meshy %s %s/%s\n", version, runtime.GOOS, runtime.GOARCH)
			return
		}
	}
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "join":
		err = cmdJoin(os.Args[2:])
	case "join-org":
		err = cmdJoinOrg(os.Args[2:])
	case "login":
		err = cmdLogin(os.Args[2:])
	case "whoami":
		err = cmdWhoami()
	case "top":
		err = cmdTop(os.Args[2:])
	case "grants":
		err = cmdGrants(os.Args[2:])
	case "connect":
		err = cmdConnect(os.Args[2:])
	case "mesh-join":
		err = cmdMeshJoin(os.Args[2:])
	case "mount":
		err = cmdMount(os.Args[2:])
	case "request":
		err = cmdRequest(os.Args[2:])
	case "verify-siwe":
		err = cmdVerifySIWE(os.Args[2:])
	case "curl":
		var code int
		code, err = cmdCurl(os.Args[2:])
		if err == nil {
			os.Exit(code)
		}
	case "ssh":
		var code int
		code, err = cmdSSH(os.Args[2:])
		if err == nil {
			os.Exit(code)
		}
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var stdout io.Writer = os.Stdout

func usage() {
	fmt.Fprint(os.Stderr, `meshy — agent access to Meshy resources (ENSv2-controlled)

  meshy login                           sign in with your wallet; joins this machine to your orgs
  meshy join <invite-link>              set up this agent and wait for approval
  meshy join-org <invite-link> [--name alice]
                                        join an org as a person (People → Invite link)
  meshy whoami                          identity and ENS records
  meshy grants <resource>...            what you can access and for how long
  meshy ssh <resource> [--] <command>   run a command on a resource (folder sandbox)
  meshy mount <device>                  show the folders you were granted in Finder (network volume)
  meshy request access <target> [--path p --mode ro|rw --dur s]
                                        ask the org's admins for access (they see it in Meshy)
  meshy connect <service> [--local :8001]  tunnel to a service, e.g. camera.pi.home.acme.eth
  meshy mesh-join --provider <url>      join the provider's WireGuard mesh as this agent's own peer
  meshy curl <service> [/path]          one HTTP request to a service
  meshy verify-siwe --sig 0x… < msg     check a Sign-In with Ethereum message, print the signer
  meshy -v                              print the version
  meshy top [--org o.eth] [--demo]      live mission-control view of the org (JSON when piped)

Errors are machine-readable: MESHY_BLOCKED, MESHY_REVOKED, MESHY_EXPIRED,
MESHY_NO_GRANT, MESHY_NOT_HUMAN_BACKED.
`)
	os.Exit(2)
}

// ---- top ----

func cmdTop(args []string) error {
	fs := flag.NewFlagSet("top", flag.ExitOnError)
	var o tui.Options
	o.Flags(fs)
	fs.Parse(args)
	o.Mode = "agent"
	ids := localIdentities()
	o.Orgs = orgsOf(ids)
	o.MeFor = func(org string) string {
		for _, id := range ids {
			if id.Org == org {
				return string(id.Name())
			}
		}
		return ""
	}
	if o.Org == "" && len(o.Orgs) > 0 {
		o.Org = o.Orgs[0]
	}
	if o.Org == "" && !o.Demo {
		return errors.New("MESHY_NO_ORG this machine isn't in an org yet: run `meshy login` (or open Meshy for Mac and sign in), or pass --org <org.eth>")
	}
	o.Me = o.MeFor(o.Org)
	return tui.Run(context.Background(), o)
}

// localIdentities is every Meshy identity on this machine: the agent in
// ~/.meshy (or MESHY_HOME) and each member identity from `meshy login`.
func localIdentities() []*Identity {
	var out []*Identity
	if id, err := loadIdentity(); err == nil {
		out = append(out, id)
	}
	if os.Getenv("MESHY_HOME") == "" {
		h, _ := os.UserHomeDir()
		dirs, _ := filepath.Glob(filepath.Join(h, ".meshy", "person-*"))
		for _, d := range dirs {
			if id, err := loadIdentityFrom(d); err == nil {
				out = append(out, id)
			}
		}
	}
	return out
}

// orgsOf lists the orgs of ids (first seen first), plus the org this Mac
// shares a device in (from the Meshy for Mac app), so `meshy top` follows the
// machine instead of a fixed org.
func orgsOf(ids []*Identity) []string {
	seen := map[string]bool{}
	var orgs []string
	add := func(o string) {
		o = strings.ToLower(strings.TrimSpace(o))
		if o != "" && !seen[o] {
			seen[o] = true
			orgs = append(orgs, o)
		}
	}
	for _, id := range ids {
		add(id.Org)
	}
	for _, o := range macAppOrgs() {
		add(o)
	}
	return orgs
}

// macAppOrgs reads the orgs the signed-in wallet added in Meshy for Mac
// (its preferences: "orgs.<address>"), current org first.
func macAppOrgs() []string {
	if runtime.GOOS != "darwin" {
		return nil
	}
	raw, err := exec.Command("/bin/sh", "-c", "defaults export eth.meshydemo.mac - | plutil -convert json -o - -").Output()
	if err != nil {
		return nil
	}
	var prefs map[string]any
	if json.Unmarshal(raw, &prefs) != nil {
		return nil
	}
	who := strings.ToLower(fmt.Sprint(prefs["identity.address"]))
	var out []string
	if cur, ok := prefs["org."+who].(string); ok {
		out = append(out, cur)
	}
	if list, ok := prefs["orgs."+who].([]any); ok {
		for _, o := range list {
			if s, ok := o.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// ---- join ----

// Invite links look like
//
//	https://<dashboard>/approve?org=acme.eth&agent=claude-1
//
// (the admin reserved claude-1.agents.acme.eth). We append our public keys
// and open the page; the human signs in, completes World ID Agent Pass and
// sends the ENS transaction. We then wait until ENS shows our keys.
func cmdJoin(args []string) error {
	fs := flag.NewFlagSet("join", flag.ExitOnError)
	noOpen := fs.Bool("no-open", false, "print the link instead of opening a browser")
	timeout := fs.Duration("timeout", 20*time.Minute, "how long to wait for approval")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("usage: meshy join <invite-link>")
	}
	u, err := url.Parse(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("bad invite link: %w", err)
	}
	// People-page invites (https://meshy.live/join/…) are for people: same flow as join-org.
	if strings.HasPrefix(u.Path, "/join/") {
		return cmdJoinOrg(append([]string{fs.Arg(0)}, args[:len(args)-1]...))
	}
	q := u.Query()
	org, agent := q.Get("org"), q.Get("agent")
	if org == "" || agent == "" {
		return errors.New("that isn't a Meshy invite link: use a meshy.live/join/… link (people) or an /approve?org=…&agent=… link (agents)")
	}
	id, err := createIdentity(agent, org)
	if err != nil {
		return err
	}
	q.Set("addr", id.Addr)
	q.Set("ssh", id.SSH)
	u.RawQuery = q.Encode()
	link := u.String()

	fmt.Printf("Agent identity: %s\n  addr %s\n  ssh  %s\n\n", id.Name(), id.Addr, fingerprint(id.key.PublicKey()))
	fmt.Printf("MESHY_APPROVE_URL %s\n", link)
	fmt.Println("Ask your human to open this link, sign in, complete World ID, and accept the invite.")
	if !*noOpen {
		openBrowser(link)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	ens, err := ensClient(ctx)
	if err != nil {
		return err
	}
	fmt.Print("Waiting for approval on ENS")
	for {
		ok, why := joined(ctx, ens, id)
		if ok {
			fmt.Printf("\nMESHY_JOINED agent=%s\n", id.Name())
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("\nMESHY_JOIN_TIMEOUT %s (last state: %s)", id.Name(), why)
		case <-time.After(4 * time.Second):
			fmt.Print(".")
		}
	}
}

func joined(ctx context.Context, ens *enskit.Client, id *Identity) (bool, string) {
	r, err := ens.Lookup(ctx, id.Name())
	if err != nil {
		return false, err.Error()
	}
	if r.State.Status != enskit.Registered {
		return false, "name " + r.State.Status.String()
	}
	ssh, _ := r.Text(ctx, ens, "meshy.ssh")
	if strings.TrimSpace(ssh) != id.SSH {
		return false, "meshy.ssh not set to our key yet"
	}
	if id.Kind == "person" {
		if a, err := ens.Addr(ctx, r.Resolver, id.Name()); err != nil || !strings.EqualFold(a.Hex(), id.Addr) {
			return false, "waiting for this device's key on the name (second step on the join page)"
		}
		return true, ""
	}
	if att, _ := r.Text(ctx, ens, "meshy.worldid"); att == "" {
		return false, "waiting for World ID attestation"
	}
	return true, ""
}

func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "linux":
		cmd = exec.Command("xdg-open", u)
	default:
		return
	}
	_ = cmd.Start()
}

func fingerprint(k gossh.PublicKey) string { return gossh.FingerprintSHA256(k) }

// ---- whoami / grants ----

func cmdWhoami() error {
	id, err := loadIdentity()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ens, err := ensClient(ctx)
	if err != nil {
		return err
	}
	ok, why := joined(ctx, ens, id)
	state := "active (human-backed)"
	if !ok {
		state = "NOT ACTIVE: " + why
	}
	fmt.Printf("agent  %s\naddr   %s\nssh    %s\nstatus %s\n", id.Name(), id.Addr, fingerprint(id.key.PublicKey()), state)
	return nil
}

func cmdGrants(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: meshy grants <resource ens name>...")
	}
	id, err := loadIdentityFor(enskit.Name(args[0]))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ens, err := ensClient(ctx)
	if err != nil {
		return err
	}
	// A member's grants may be owned by their wallet (the owner of their
	// people name) rather than this machine's key.
	wallet := common.Address{}
	if id.Kind == "person" {
		wallet, _ = ens.ExactOwner(ctx, id.Name(), nil)
	}
	for _, res := range args {
		g, err := enskit.GrantName(id.Agent, enskit.Name(res))
		if err != nil {
			fmt.Printf("%s  error: %v\n", res, err)
			continue
		}
		owner, err := ens.ExactOwner(ctx, g, nil)
		if err != nil {
			fmt.Printf("%s  error: %v\n", res, err)
			continue
		}
		if owner == (common.Address{}) {
			fmt.Printf("MESHY_NO_GRANT resource=%s grant=%s\n", res, g)
			continue
		}
		r, err := ens.Lookup(ctx, g)
		if err != nil {
			fmt.Printf("%s  error: %v\n", res, err)
			continue
		}
		exp := time.Unix(int64(r.State.Expiry), 0)
		fmt.Printf("MESHY_GRANT resource=%s grant=%s expires=%s left=%s owner_ok=%v\n",
			res, g, exp.Format(time.RFC3339), time.Until(exp).Round(time.Second), strings.EqualFold(owner.Hex(), id.Addr) || (wallet != (common.Address{}) && owner == wallet))
	}
	return nil
}

// ---- ssh ----

func cmdSSH(args []string) (int, error) {
	fs := flag.NewFlagSet("ssh", flag.ExitOnError)
	endpoint := fs.String("endpoint", "", "override host:port (default: ENS meshy.endpoint, else <resource>:22 via mesh DNS)")
	fs.Parse(args)
	rest := fs.Args()
	if len(rest) < 1 {
		return 0, errors.New("usage: meshy ssh <resource> [--] <command>")
	}
	resource := enskit.Name(rest[0])
	cmdArgs := rest[1:]
	if len(cmdArgs) > 0 && cmdArgs[0] == "--" {
		cmdArgs = cmdArgs[1:]
	}
	id, err := loadIdentityFor(resource)
	if err != nil {
		return 0, err
	}
	conn, err := dialResource(id, resource, *endpoint)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	sess, err := conn.NewSession()
	if err != nil {
		return 0, err
	}
	defer sess.Close()
	sess.Stdout, sess.Stderr = os.Stdout, os.Stderr
	if fi, _ := os.Stdin.Stat(); fi != nil && fi.Mode()&os.ModeCharDevice == 0 {
		sess.Stdin = os.Stdin
	}
	err = sess.Run(shellJoin(cmdArgs))
	var ee *gossh.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &ee):
		return ee.ExitStatus(), nil
	default:
		var em *gossh.ExitMissingError
		if errors.As(err, &em) {
			return 255, nil
		}
		return 255, err
	}
}

// shellJoin rebuilds a command line. A single argument is passed verbatim
// (so `meshy ssh r -- 'cat a | grep b'` works); multiple args are quoted.
func shellJoin(args []string) string {
	if len(args) == 1 {
		return args[0]
	}
	q := make([]string, len(args))
	for i, a := range args {
		if a != "" && !strings.ContainsAny(a, " \t\n'\"\\$`|;&<>(){}*?[]#~") {
			q[i] = a
		} else {
			q[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(q, " ")
}

// dialResource opens an SSH connection to a resource's meshyd as this agent,
// verifying the host key pinned in ENS (meshy.ssh-host).
func dialResource(id *Identity, resource enskit.Name, endpoint string) (*gossh.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ens, err := ensClient(ctx)
	if err != nil {
		return nil, err
	}
	r, err := ens.Lookup(ctx, resource)
	if err != nil {
		return nil, fmt.Errorf("MESHY_UNKNOWN_RESOURCE %s: %v", resource, err)
	}
	hostRec, _ := r.Text(ctx, ens, "meshy.ssh-host")
	if hostRec == "" {
		return nil, fmt.Errorf("MESHY_UNKNOWN_RESOURCE %s has no meshy.ssh-host record", resource)
	}
	hostKey, _, _, _, err := gossh.ParseAuthorizedKey([]byte(hostRec))
	if err != nil {
		return nil, fmt.Errorf("bad meshy.ssh-host on %s: %v", resource, err)
	}
	addr := endpoint
	if addr == "" {
		addr, _ = r.Text(ctx, ens, "meshy.endpoint")
	}
	if addr == "" {
		addr = net.JoinHostPort(string(resource), "22") // resolved by Meshy mesh DNS
	}
	cfg := &gossh.ClientConfig{
		User: id.Agent,
		Auth: []gossh.AuthMethod{gossh.PublicKeys(id.key)},
		// The host key is pinned in ENS: no trust-on-first-use.
		HostKeyCallback: gossh.FixedHostKey(hostKey),
		Timeout:         15 * time.Second,
	}
	var conn *gossh.Client
	if id.Mesh == "" && endpoint == "" && strings.HasSuffix(addr, ":22") && strings.HasPrefix(addr, string(resource)) {
		// No endpoint on ENS and not on the mesh yet: join it now instead of
		// failing on a name only mesh DNS knows.
		fmt.Fprintf(os.Stderr, "Joining the Meshy mesh as %s (first time only)…\n", id.Name())
		if err := meshJoin(id, env("MESHY_PROVIDER", "https://api.meshy.live")); err != nil {
			return nil, fmt.Errorf("MESHY_NOT_ON_MESH %s can't join the mesh: %v", id.Name(), err)
		}
	}
	if id.Mesh != "" && endpoint == "" {
		// Over the provider mesh: the endpoint record is ignored and the
		// resource is found by its ENS name among mesh peers.
		nc, derr := meshDial(id, string(resource), "22")
		if derr == nil {
			var c gossh.Conn
			var chans <-chan gossh.NewChannel
			var reqs <-chan *gossh.Request
			c, chans, reqs, err = gossh.NewClientConn(nc, string(resource)+":22", cfg)
			if err == nil {
				conn = gossh.NewClient(c, chans, reqs)
			} else {
				nc.Close()
			}
		} else {
			err = derr
		}
		addr = "mesh:" + string(resource)
	} else {
		conn, err = gossh.Dial("tcp", addr, cfg)
	}
	if err != nil {
		if strings.Contains(err.Error(), "unable to authenticate") {
			return nil, fmt.Errorf("MESHY_NOT_ADMITTED resource=%s agent=%s (identity not approved, key mismatch, or not human-backed; run `meshy whoami`)", resource, id.Name())
		}
		return nil, fmt.Errorf("MESHY_UNREACHABLE resource=%s addr=%s: %v", resource, addr, err)
	}
	return conn, nil
}

// ---- services: connect / curl ----

// A service is a child of a device: camera.pi.home.acme.eth lives on
// pi.home.acme.eth. We SSH to the device and open a direct-tcpip channel whose
// destination is the service's ENS name; meshyd maps it to the target that ENS
// declares and only while our grant <agent>.<service> is live.
func cmdConnect(args []string) error {
	fs := flag.NewFlagSet("connect", flag.ExitOnError)
	local := fs.String("local", "127.0.0.1:0", "local listen address")
	endpoint := fs.String("endpoint", "", "override the device's endpoint")
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		args = append(append([]string{}, args[1:]...), args[0]) // allow: connect <svc> --local ...
	}
	fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("usage: meshy connect <service ens name> [--local 127.0.0.1:8001]")
	}
	service := enskit.Name(fs.Arg(0))
	id, err := loadIdentityFor(service)
	if err != nil {
		return err
	}
	conn, err := dialResource(id, service.Parent(), *endpoint)
	if err != nil {
		return err
	}
	defer conn.Close()
	// Probe once so authorization errors surface immediately.
	probe, err := conn.Dial("tcp", string(service)+":0")
	if err != nil {
		return fmt.Errorf("%s", strings.TrimPrefix(err.Error(), "ssh: rejected: administratively prohibited ("))
	}
	probe.Close()
	ln, err := net.Listen("tcp", *local)
	if err != nil {
		return err
	}
	fmt.Printf("MESHY_CONNECTED service=%s local=%s (Ctrl-C to stop)\n", service, ln.Addr())
	for {
		c, err := ln.Accept()
		if err != nil {
			return err
		}
		go func() {
			defer c.Close()
			up, err := conn.Dial("tcp", string(service)+":0")
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return
			}
			defer up.Close()
			done := make(chan struct{}, 2)
			go func() { io.Copy(up, c); done <- struct{}{} }()
			go func() { io.Copy(c, up); done <- struct{}{} }()
			<-done
		}()
	}
}

// cmdCurl does one HTTP GET through a service tunnel: the easy path for agents.
func cmdCurl(args []string) (int, error) {
	fs := flag.NewFlagSet("curl", flag.ExitOnError)
	method := fs.String("X", "GET", "HTTP method")
	data := fs.String("d", "", "request body")
	fs.Parse(args)
	if fs.NArg() < 1 {
		return 0, errors.New("usage: meshy curl <service ens name> [/path] [-X POST -d body]")
	}
	service := enskit.Name(fs.Arg(0))
	path := "/"
	if fs.NArg() > 1 {
		path = fs.Arg(1)
	}
	id, err := loadIdentityFor(service)
	if err != nil {
		return 0, err
	}
	conn, err := dialResource(id, service.Parent(), "")
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		c, err := conn.Dial("tcp", string(service)+":0")
		if err != nil {
			return nil, errors.New(strings.TrimSuffix(strings.TrimPrefix(err.Error(), "ssh: rejected: administratively prohibited ("), ")"))
		}
		return c, nil
	}}
	req, err := http.NewRequest(*method, "http://"+string(service)+path, strings.NewReader(*data))
	if err != nil {
		return 0, err
	}
	resp, err := (&http.Client{Transport: tr, Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	fmt.Fprintf(os.Stderr, "MESHY_HTTP %s %d via %s\n", resp.Proto, resp.StatusCode, service)
	io.Copy(os.Stdout, resp.Body)
	if resp.StatusCode >= 400 {
		return 22, nil
	}
	return 0, nil
}
