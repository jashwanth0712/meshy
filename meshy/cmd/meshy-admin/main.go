// Command meshy-admin manages a Meshy org in ENSv2 from the command line (the
// dashboard does the same with the admin's wallet). Useful for scripted demos
// and the e2e suite.
//
//	meshy-admin org init     --label acme [--attestor 0x..]
//	meshy-admin network add  --org acme.eth --name prod
//	meshy-admin resource add --org acme.eth --network prod --join meshy-join:...
//	meshy-admin agent add    --org acme.eth --approve '<MESHY_APPROVE_URL>' [--dev-attest-key hex]
//	meshy-admin grant        --resource db1.prod.acme.eth --agent claude-1 --path /srv/reports:ro --for 1h
//	meshy-admin revoke       --resource db1.prod.acme.eth --agent claude-1
//	meshy-admin show         --org acme.eth
package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/jashwanth0712/meshy/meshy/enskit"
	"github.com/jashwanth0712/meshy/meshy/policy"
	"github.com/jashwanth0712/meshy/meshy/worldid"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// State remembers the contracts behind an org (ENS is still the source of
// truth; this is a cache of addresses we deployed).
type State struct {
	Org        string            `json:"org"`
	Resolver   string            `json:"resolver"`
	OrgReg     string            `json:"orgRegistry"`
	AgentsReg  string            `json:"agentsRegistry"`
	Networks   map[string]string `json:"networks"`  // name -> registry
	Resources  map[string]string `json:"resources"` // full name -> grants registry
	OrgKeyPriv string            `json:"orgKeyPriv"`
}

func stateDir() string {
	if d := os.Getenv("MESHY_ADMIN_HOME"); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".meshy", "admin")
}

func loadState(org string) (*State, error) {
	b, err := os.ReadFile(filepath.Join(stateDir(), org+".json"))
	if err != nil {
		return nil, fmt.Errorf("no admin state for %s (run `meshy-admin org init`): %w", org, err)
	}
	var s State
	return &s, json.Unmarshal(b, &s)
}

func (s *State) save() error {
	os.MkdirAll(stateDir(), 0o700)
	b, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(filepath.Join(stateDir(), s.Org+".json"), b, 0o600)
}

type cli struct {
	rpc, key string
	anvil    bool
}

func (c *cli) register(fs *flag.FlagSet) {
	fs.StringVar(&c.rpc, "rpc", env("MESHY_SEPOLIA", "http://127.0.0.1:8545"), "Sepolia RPC")
	fs.StringVar(&c.key, "key", env("MESHY_ADMIN_KEY", ""), "admin private key (hex)")
	fs.BoolVar(&c.anvil, "anvil", env("MESHY_ANVIL", "") != "", "local anvil fork: warp time instead of waiting")
}

func (c *cli) admin(ctx context.Context) (*enskit.Admin, error) {
	if c.key == "" {
		return nil, errors.New("--key / MESHY_ADMIN_KEY required")
	}
	k, err := crypto.HexToECDSA(strings.TrimPrefix(c.key, "0x"))
	if err != nil {
		return nil, err
	}
	a, err := enskit.NewAdmin(ctx, c.rpc, k)
	if err != nil {
		return nil, err
	}
	if c.anvil {
		a.Wait = func(ctx context.Context, d time.Duration) error {
			if err := a.Eth.Client().CallContext(ctx, nil, "evm_increaseTime", int64(d.Seconds())); err != nil {
				return err
			}
			return a.Eth.Client().CallContext(ctx, nil, "evm_mine")
		}
	}
	return a, nil
}

func main() {
	if len(os.Args) < 3 && !(len(os.Args) == 2 && (os.Args[1] == "grant" || os.Args[1] == "revoke" || os.Args[1] == "show")) {
		usage()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	var err error
	switch os.Args[1] + " " + arg(2) {
	case "org init":
		err = orgInit(ctx, os.Args[3:])
	case "network add":
		err = networkAdd(ctx, os.Args[3:])
	case "resource add":
		err = resourceAdd(ctx, os.Args[3:])
	case "agent add":
		err = agentAdd(ctx, os.Args[3:])
	case "service add":
		err = serviceAdd(ctx, os.Args[3:])
	case "admin add":
		err = adminAdd(ctx, os.Args[3:])
	default:
		switch os.Args[1] {
		case "grant":
			err = grant(ctx, os.Args[2:])
		case "revoke":
			err = revoke(ctx, os.Args[2:])
		case "show":
			err = show(ctx, os.Args[2:])
		default:
			usage()
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "meshy-admin:", err)
		os.Exit(1)
	}
}

func arg(i int) string {
	if len(os.Args) > i {
		return os.Args[i]
	}
	return ""
}

func usage() {
	fmt.Fprint(os.Stderr, `meshy-admin — manage a Meshy org in ENSv2

  org init      --label acme [--attestor 0x..]
  network add   --org acme.eth --name prod
  resource add  --org acme.eth --network prod --join meshy-join:...
  agent add     --org acme.eth --approve '<MESHY_APPROVE_URL>' [--dev-attest-key hex]
  service add   --device pi.home.acme.eth --name camera --target 127.0.0.1:8001 [--proto http] [--desc ..]
  admin add     --org acme.eth --network prod --addr 0x..   (EAC: delegated network admin)
  grant         --resource db1.prod.acme.eth --agent claude-1 --path /srv/reports:ro [--path /srv/tmp:rw] [--for 1h]
  grant         --resource camera.pi.home.acme.eth --agent claude-1 [--for 1h]     (service: no --path)
  revoke        --resource db1.prod.acme.eth --agent claude-1
  show          --org acme.eth

Common flags: --rpc (MESHY_SEPOLIA) --key (MESHY_ADMIN_KEY) --anvil
`)
	os.Exit(2)
}

func step(format string, a ...any) { fmt.Printf("  → "+format+"\n", a...) }

var yearExpiry = func() time.Time { return time.Now().Add(365 * 24 * time.Hour) }

func orgInit(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("org init", flag.ExitOnError)
	var c cli
	c.register(fs)
	label := fs.String("label", "", "org label (registers <label>.eth)")
	attestor := fs.String("attestor", "", "trusted World ID attestor address(es), comma-separated (provider signer)")
	fs.Parse(args)
	if *label == "" {
		return errors.New("--label required")
	}
	a, err := c.admin(ctx)
	if err != nil {
		return err
	}
	org := *label + ".eth"
	fmt.Printf("Creating org %s (admin %s)\n", org, a.From().Hex())
	resolver, err := a.DeployResolver(ctx)
	if err != nil {
		return fmt.Errorf("deploy resolver: %w", err)
	}
	step("org resolver %s", resolver.Hex())
	orgReg, err := a.DeployRegistry(ctx)
	if err != nil {
		return fmt.Errorf("deploy registry: %w", err)
	}
	step("org registry %s", orgReg.Hex())
	if _, err := a.RegisterETH(ctx, *label, a.From(), orgReg, resolver, 365*24*time.Hour); err != nil {
		return fmt.Errorf("register %s: %w", org, err)
	}
	step("registered %s (commit → reveal, paid in MockUSDC)", org)
	agentsReg, err := a.DeployRegistry(ctx)
	if err != nil {
		return err
	}
	if _, err := a.Register(ctx, orgReg, "agents", a.From(), agentsReg, resolver, nil, yearExpiry()); err != nil {
		return fmt.Errorf("register agents.%s: %w", org, err)
	}
	step("agents.%s → registry %s", org, agentsReg.Hex())
	ok, _ := policy.GenerateKey()
	texts := map[string]string{"meshy.orgkey": policy.EncodePublicKey(ok.PublicKey()), "description": "Meshy org"}
	if *attestor != "" {
		texts["meshy.attestor"] = *attestor
	}
	if _, err := a.SetRecords(ctx, resolver, enskit.RecordSet{Name: enskit.Name(org), Texts: texts}); err != nil {
		return fmt.Errorf("org records: %w", err)
	}
	step("records meshy.orgkey, meshy.attestor set")
	s := &State{Org: org, Resolver: resolver.Hex(), OrgReg: orgReg.Hex(), AgentsReg: agentsReg.Hex(),
		Networks: map[string]string{}, Resources: map[string]string{}, OrgKeyPriv: policy.EncodePrivateKey(ok)}
	if err := s.save(); err != nil {
		return err
	}
	fmt.Printf("Org ready. Org key saved in %s (needed to read encrypted audit payloads).\n", stateDir())
	return nil
}

func networkAdd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("network add", flag.ExitOnError)
	var c cli
	c.register(fs)
	org := fs.String("org", "", "org, e.g. acme.eth")
	name := fs.String("name", "", "network label, e.g. prod")
	fs.Parse(args)
	s, err := loadState(*org)
	if err != nil {
		return err
	}
	a, err := c.admin(ctx)
	if err != nil {
		return err
	}
	reg, err := a.DeployRegistry(ctx)
	if err != nil {
		return err
	}
	if _, err := a.Register(ctx, common.HexToAddress(s.OrgReg), *name, a.From(), reg, common.HexToAddress(s.Resolver), nil, yearExpiry()); err != nil {
		return err
	}
	s.Networks[*name] = reg.Hex()
	step("network %s.%s → registry %s", *name, *org, reg.Hex())
	return s.save()
}

type join struct {
	Name     string   `json:"name"`
	Addr     string   `json:"addr"`
	EncKey   string   `json:"meshy.enckey"`
	SSHHost  string   `json:"meshy.ssh-host"`
	Roots    []string `json:"meshy.roots"`
	Endpoint string   `json:"meshy.endpoint,omitempty"`
}

func resourceAdd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("resource add", flag.ExitOnError)
	var c cli
	c.register(fs)
	org := fs.String("org", "", "org")
	network := fs.String("network", "", "network label")
	code := fs.String("join", "", "join code printed by `meshyd init`")
	kind := fs.String("kind", "", "device kind, e.g. server, gpu-server, raspberry-pi")
	meta := fs.String("meta", "", `device metadata JSON, e.g. '{"gpus":"2x RTX 4090","location":"lab"}'`)
	fs.Parse(args)
	s, err := loadState(*org)
	if err != nil {
		return err
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(*code, "meshy-join:"))
	if err != nil {
		return fmt.Errorf("bad join code: %w", err)
	}
	var j join
	if err := json.Unmarshal(raw, &j); err != nil {
		return err
	}
	name := enskit.Name(j.Name)
	if name.Parent() != enskit.Name(*network+"."+*org) {
		return fmt.Errorf("join code is for %s, not a resource in %s.%s", j.Name, *network, *org)
	}
	netReg, ok := s.Networks[*network]
	if !ok {
		return fmt.Errorf("unknown network %s (meshy-admin network add)", *network)
	}
	a, err := c.admin(ctx)
	if err != nil {
		return err
	}
	grantsReg, err := a.DeployRegistry(ctx)
	if err != nil {
		return err
	}
	if _, err := a.Register(ctx, common.HexToAddress(netReg), name.Label(), a.From(), grantsReg, common.HexToAddress(s.Resolver), nil, yearExpiry()); err != nil {
		return err
	}
	roots, _ := json.Marshal(j.Roots)
	addr := common.HexToAddress(j.Addr)
	texts := map[string]string{"meshy.enckey": j.EncKey, "meshy.ssh-host": j.SSHHost, "meshy.roots": string(roots)}
	if j.Endpoint != "" {
		texts["meshy.endpoint"] = j.Endpoint
	}
	if *kind != "" {
		texts["meshy.kind"] = *kind
	}
	if *meta != "" {
		if !json.Valid([]byte(*meta)) {
			return errors.New("--meta must be JSON")
		}
		texts["meshy.meta"] = *meta
	}
	if _, err := a.SetRecords(ctx, common.HexToAddress(s.Resolver), enskit.RecordSet{Name: name, Addr: &addr, Texts: texts}); err != nil {
		return err
	}
	s.Resources[j.Name] = grantsReg.Hex()
	step("resource %s registered (grants registry %s)", j.Name, grantsReg.Hex())
	return s.save()
}

func agentAdd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("agent add", flag.ExitOnError)
	var c cli
	c.register(fs)
	org := fs.String("org", "", "org")
	approve := fs.String("approve", "", "MESHY_APPROVE_URL printed by `meshy join`")
	devAttest := fs.String("dev-attest-key", "", "DEV ONLY: sign the World ID attestation with this key instead of the provider")
	fs.Parse(args)
	s, err := loadState(*org)
	if err != nil {
		return err
	}
	u, err := url.Parse(*approve)
	if err != nil {
		return err
	}
	q := u.Query()
	agent, addrS, ssh := q.Get("agent"), q.Get("addr"), q.Get("ssh")
	if agent == "" || !common.IsHexAddress(addrS) || ssh == "" {
		return errors.New("approve URL must carry agent, addr and ssh")
	}
	a, err := c.admin(ctx)
	if err != nil {
		return err
	}
	name := enskit.Name(agent + ".agents." + *org)
	addr := common.HexToAddress(addrS)
	// The agent gets its OWN PermissionedResolver: the approving human holds
	// every role, the agent may edit only its meshy.ssh key (key rotation).
	// The org still controls membership via the agents registry (unregister).
	agentRes, err := a.DeployResolver(ctx, a.From())
	if err != nil {
		return fmt.Errorf("deploy agent resolver: %w", err)
	}
	step("agent resolver %s (owned by the approver, not the org resolver)", agentRes.Hex())
	if _, err := a.Register(ctx, common.HexToAddress(s.AgentsReg), agent, a.From(), common.Address{}, agentRes, nil, yearExpiry()); err != nil {
		return err
	}
	texts := map[string]string{
		"meshy.ssh":     ssh,
		"meshy.owner":   a.From().Hex(),
		"agent-context": "Meshy agent " + agent + " (Claude Code). Access is granted per resource via ENS subnames.",
	}
	if *devAttest != "" {
		k, err := crypto.HexToECDSA(strings.TrimPrefix(*devAttest, "0x"))
		if err != nil {
			return err
		}
		att, err := devAttestation(name, addr, k)
		if err != nil {
			return err
		}
		texts["meshy.worldid"] = att
		step("DEV attestation signed by %s (production: provider after World ID Agent Pass)", crypto.PubkeyToAddress(k.PublicKey).Hex())
	}
	if _, err := a.SetRecords(ctx, agentRes, enskit.RecordSet{Name: name, Addr: &addr, Texts: texts}); err != nil {
		return err
	}
	if _, err := a.GrantTextKey(ctx, agentRes, "meshy.ssh", addr); err != nil {
		return fmt.Errorf("grant meshy.ssh setter role: %w", err)
	}
	step("agent %s → %s (agent may rotate only meshy.ssh)", name, addr.Hex())
	return nil
}

func devAttestation(agent enskit.Name, addr common.Address, k *ecdsa.PrivateKey) (string, error) {
	now := time.Now()
	return worldid.Sign(worldid.Attestation{
		Agent: string(agent), Addr: addr.Hex(), Human: worldid.HumanHash("dev", "dev-human", string(agent.Org())),
		ACR: "dev", AuthTime: now.Unix(), IssuedAt: now.Unix(), Expires: now.Add(7 * 24 * time.Hour).Unix(),
	}, k)
}

type pathFlags []policy.PathRule

func (p *pathFlags) String() string { return fmt.Sprint(*p) }
func (p *pathFlags) Set(v string) error {
	path, mode, ok := strings.Cut(v, ":")
	if !ok {
		mode = "ro"
	}
	*p = append(*p, policy.PathRule{Path: path, Mode: policy.Mode(mode)})
	return nil
}

func grant(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("grant", flag.ExitOnError)
	var c cli
	c.register(fs)
	resource := fs.String("resource", "", "resource ENS name")
	agent := fs.String("agent", "", "agent label")
	var paths pathFlags
	fs.Var(&paths, "path", "path[:ro|rw], repeatable")
	dur := fs.Duration("for", time.Hour, "grant duration (agents: max 4h)")
	fs.Parse(args)
	if *dur > 4*time.Hour {
		return errors.New("agent grants are capped at 4h")
	}
	res := enskit.Name(*resource)
	s, err := loadState(string(res.Org()))
	if err != nil {
		return err
	}
	grantsReg, ok := s.Resources[*resource]
	if !ok {
		return fmt.Errorf("unknown resource %s", *resource)
	}
	a, err := c.admin(ctx)
	if err != nil {
		return err
	}
	ens := enskit.NewClient(a.Eth, enskit.SepoliaUniversalHelper)
	agentName, _ := enskit.AgentName(*agent, res)
	ag, err := ens.Lookup(ctx, agentName)
	if err != nil {
		return fmt.Errorf("agent %s: %w", agentName, err)
	}
	agentAddr, err := ens.Addr(ctx, ag.Resolver, agentName)
	if err != nil {
		return err
	}
	gname := enskit.Name(*agent + "." + string(res))
	expiry := time.Now().Add(*dur)
	if len(paths) == 0 {
		// Service grant: port-level access to <service>; no folder policy.
		if _, err := a.Register(ctx, common.HexToAddress(grantsReg), *agent, agentAddr, common.Address{}, common.HexToAddress(s.Resolver), nil, expiry); err != nil {
			return fmt.Errorf("register grant: %w", err)
		}
		fmt.Printf("MESHY_GRANTED grant=%s service=%s expires=%s\n", gname, res, expiry.Format(time.RFC3339))
		return nil
	}
	rr, err := ens.Lookup(ctx, res)
	if err != nil {
		return err
	}
	encKey, _ := rr.Text(ctx, ens, "meshy.enckey")
	daemonKey, err := policy.ParsePublicKey(encKey)
	if err != nil {
		return fmt.Errorf("resource %s has no valid meshy.enckey: %w", res, err)
	}
	orgPriv, err := policy.ParsePrivateKey(s.OrgKeyPriv)
	if err != nil {
		return err
	}
	if _, err := a.Register(ctx, common.HexToAddress(grantsReg), *agent, agentAddr, common.Address{}, common.HexToAddress(s.Resolver), nil, expiry); err != nil {
		return fmt.Errorf("register grant: %w", err)
	}
	st, err := ens.State(ctx, common.HexToAddress(grantsReg), *agent, nil)
	if err != nil {
		return err
	}
	pol := &policy.Policy{V: policy.Version, Grant: string(gname), GrantTokenID: st.TokenID.String(), Paths: paths}
	sealed, err := policy.Seal(pol, daemonKey, orgPriv.PublicKey())
	if err != nil {
		return err
	}
	if _, err := a.SetRecords(ctx, common.HexToAddress(s.Resolver), enskit.RecordSet{Name: gname, Texts: map[string]string{"meshy.policy": sealed}}); err != nil {
		return err
	}
	fmt.Printf("MESHY_GRANTED grant=%s paths=%s expires=%s\n", gname, pol.AllowedSummary(), expiry.Format(time.RFC3339))
	return nil
}

func revoke(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("revoke", flag.ExitOnError)
	var c cli
	c.register(fs)
	resource := fs.String("resource", "", "resource ENS name")
	agent := fs.String("agent", "", "agent label")
	fs.Parse(args)
	res := enskit.Name(*resource)
	s, err := loadState(string(res.Org()))
	if err != nil {
		return err
	}
	a, err := c.admin(ctx)
	if err != nil {
		return err
	}
	sent := time.Now()
	r, err := a.Unregister(ctx, common.HexToAddress(s.Resources[*resource]), *agent)
	if err != nil {
		return err
	}
	fmt.Printf("MESHY_REVOKE_MINED grant=%s.%s block=%d tx=%s after=%s\n", *agent, *resource, r.BlockNumber, r.TxHash.Hex(), time.Since(sent).Round(time.Millisecond))
	return nil
}

func show(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("show", flag.ExitOnError)
	org := fs.String("org", "", "org")
	fs.Parse(args)
	s, err := loadState(*org)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	fmt.Println(string(b))
	return nil
}

func serviceAdd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("service add", flag.ExitOnError)
	var c cli
	c.register(fs)
	device := fs.String("device", "", "device (resource) ENS name, e.g. pi.home.acme.eth")
	name := fs.String("name", "", "service label, e.g. camera")
	target := fs.String("target", "", "host:port on the device, e.g. 127.0.0.1:8001")
	proto := fs.String("proto", "http", "protocol: http, tcp, rtsp, ...")
	desc := fs.String("desc", "", "description")
	fs.Parse(args)
	dev := enskit.Name(*device)
	s, err := loadState(string(dev.Org()))
	if err != nil {
		return err
	}
	devReg, ok := s.Resources[*device]
	if !ok {
		return fmt.Errorf("unknown device %s (meshy-admin resource add first)", *device)
	}
	if *name == "" || *target == "" {
		return errors.New("--name and --target are required")
	}
	a, err := c.admin(ctx)
	if err != nil {
		return err
	}
	svcName := *name + "." + *device
	grantsReg, err := a.DeployRegistry(ctx)
	if err != nil {
		return err
	}
	if _, err := a.Register(ctx, common.HexToAddress(devReg), *name, a.From(), grantsReg, common.HexToAddress(s.Resolver), nil, yearExpiry()); err != nil {
		return fmt.Errorf("register %s: %w", svcName, err)
	}
	spec, _ := json.Marshal(map[string]string{"proto": *proto, "target": *target, "desc": *desc})
	texts := map[string]string{"meshy.service": string(spec), "meshy.kind": "service", "url": *proto + "://" + svcName}
	if *desc != "" {
		texts["description"] = *desc
	}
	if _, err := a.SetRecords(ctx, common.HexToAddress(s.Resolver), enskit.RecordSet{Name: enskit.Name(svcName), Texts: texts}); err != nil {
		return err
	}
	s.Resources[svcName] = grantsReg.Hex()
	step("service %s → %s://%s (grants registry %s)", svcName, *proto, *target, grantsReg.Hex())
	return s.save()
}

// adminAdd delegates a network to another address with Enhanced Access
// Control: create/revoke/renew names in the network and its resources, and
// write ONLY meshy.policy records on the org resolver.
func adminAdd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("admin add", flag.ExitOnError)
	var c cli
	c.register(fs)
	org := fs.String("org", "", "org")
	network := fs.String("network", "", "network label")
	addrS := fs.String("addr", "", "admin address")
	fs.Parse(args)
	s, err := loadState(*org)
	if err != nil {
		return err
	}
	netReg, ok := s.Networks[*network]
	if !ok || !common.IsHexAddress(*addrS) {
		return errors.New("--network must exist and --addr must be an address")
	}
	a, err := c.admin(ctx)
	if err != nil {
		return err
	}
	who := common.HexToAddress(*addrS)
	if _, err := a.GrantRootRoles(ctx, common.HexToAddress(netReg), enskit.AdminRoles, who); err != nil {
		return fmt.Errorf("network roles: %w", err)
	}
	step("%s: REGISTRAR|UNREGISTER|RENEW on %s.%s", who.Hex(), *network, *org)
	suffix := "." + *network + "." + *org
	for name, reg := range s.Resources {
		if strings.HasSuffix(name, suffix) {
			if _, err := a.GrantRootRoles(ctx, common.HexToAddress(reg), enskit.AdminRoles, who); err != nil {
				return fmt.Errorf("roles on %s: %w", name, err)
			}
			step("%s: can grant/revoke on %s", who.Hex(), name)
		}
	}
	if _, err := a.GrantTextKey(ctx, common.HexToAddress(s.Resolver), "meshy.policy", who); err != nil {
		return fmt.Errorf("resolver setter role: %w", err)
	}
	step("%s: may set ONLY the meshy.policy text record on the org resolver", who.Hex())
	return nil
}
