// Command meshy is the agent/human CLI.
//
//	meshy join <invite-url>               generate keys, open approval page, wait for ENS
//	meshy whoami                          show this agent's identity and records
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
	"net"
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
	Addr  string `json:"addr"`
	SSH   string `json:"ssh"` // authorized_keys line
	dir   string
	key   gossh.Signer
	eth   *ecdsa.PrivateKey
}

func (i *Identity) Name() enskit.Name { return enskit.Name(i.Agent + ".agents." + i.Org) }

func home() string {
	if d := os.Getenv("MESHY_HOME"); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".meshy")
}

func loadIdentity() (*Identity, error) {
	dir := home()
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

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "join":
		err = cmdJoin(os.Args[2:])
	case "whoami":
		err = cmdWhoami()
	case "grants":
		err = cmdGrants(os.Args[2:])
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

func usage() {
	fmt.Fprint(os.Stderr, `meshy — agent access to Meshy resources (ENSv2-controlled)

  meshy join <invite-link>              set up this agent and wait for approval
  meshy whoami                          identity and ENS records
  meshy grants <resource>...            what you can access and for how long
  meshy ssh <resource> [--] <command>   run a command on a resource

Errors are machine-readable: MESHY_BLOCKED, MESHY_REVOKED, MESHY_EXPIRED,
MESHY_NO_GRANT, MESHY_NOT_HUMAN_BACKED.
`)
	os.Exit(2)
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
	q := u.Query()
	org, agent := q.Get("org"), q.Get("agent")
	if org == "" || agent == "" {
		return errors.New("invite link must contain org= and agent=")
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
	id, err := loadIdentity()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return errors.New("usage: meshy grants <resource ens name>...")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ens, err := ensClient(ctx)
	if err != nil {
		return err
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
			res, g, exp.Format(time.RFC3339), time.Until(exp).Round(time.Second), owner.Hex() == id.Addr)
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
	id, err := loadIdentity()
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ens, err := ensClient(ctx)
	if err != nil {
		return 0, err
	}
	r, err := ens.Lookup(ctx, resource)
	if err != nil {
		return 0, fmt.Errorf("MESHY_UNKNOWN_RESOURCE %s: %v", resource, err)
	}
	hostRec, _ := r.Text(ctx, ens, "meshy.ssh-host")
	if hostRec == "" {
		return 0, fmt.Errorf("MESHY_UNKNOWN_RESOURCE %s has no meshy.ssh-host record", resource)
	}
	hostKey, _, _, _, err := gossh.ParseAuthorizedKey([]byte(hostRec))
	if err != nil {
		return 0, fmt.Errorf("bad meshy.ssh-host on %s: %v", resource, err)
	}
	addr := *endpoint
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
	conn, err := gossh.Dial("tcp", addr, cfg)
	if err != nil {
		if strings.Contains(err.Error(), "unable to authenticate") {
			return 0, fmt.Errorf("MESHY_NO_GRANT resource=%s agent=%s (no live grant, key mismatch, or not human-backed; run `meshy grants %s`)", resource, id.Name(), resource)
		}
		return 0, fmt.Errorf("MESHY_UNREACHABLE resource=%s addr=%s: %v", resource, addr, err)
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
