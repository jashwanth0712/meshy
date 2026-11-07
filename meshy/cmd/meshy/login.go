package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jashwanth0712/meshy/meshy/enskit"
)

// cmdLogin signs this machine in to every org your wallet belongs to, with no
// invite link:
//
//  1. make (once) this machine's SSH + device keys in ~/.meshy/device
//  2. open <dashboard>/login with the public halves and a one-shot
//     http://127.0.0.1:<port>/callback; there you connect your wallet, see your
//     orgs (read from chain) and link this machine: one tx where you already
//     have a name, sign + 2 txs where you're an owner/admin without one
//  3. the page redirects to the callback with the names; we only trust what
//     ENS says: each name must carry our meshy.ssh and our device address
//  4. write one identity per org (~/.meshy/person-<name>.<org>); ssh/connect/
//     curl/grants then pick the right one by the target's org
func cmdLogin(args []string) error {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	dash := fs.String("dashboard", env("MESHY_DASHBOARD", "https://meshy.live"), "dashboard URL")
	noOpen := fs.Bool("no-open", false, "print the link instead of opening a browser")
	timeout := fs.Duration("timeout", 20*time.Minute, "how long to wait")
	fs.Parse(args)

	h, _ := os.UserHomeDir()
	keyDir := filepath.Join(h, ".meshy", "device")
	sshPriv, sshPub, ethKey, err := deviceKeys(keyDir)
	if err != nil {
		return err
	}
	device := crypto.PubkeyToAddress(ethKey.PublicKey).Hex()
	sshLine := strings.TrimSpace(string(gossh.MarshalAuthorizedKey(sshPub.PublicKey())))

	stateB := make([]byte, 16)
	rand.Read(stateB)
	state := hex.EncodeToString(stateB)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	cb := fmt.Sprintf("http://%s/callback", ln.Addr().String())
	q := url.Values{"ssh": {sshLine}, "device": {device}, "cb": {cb}, "state": {state}}
	link := strings.TrimRight(*dash, "/") + "/login?" + q.Encode()

	fmt.Printf("This machine\n  device %s\n  ssh    %s\n\n", device, fingerprint(sshPub.PublicKey()))
	fmt.Printf("MESHY_LOGIN_URL %s\n", link)
	fmt.Println("Sign in with your wallet in the browser and pick the orgs this machine should join.")
	if !*noOpen {
		openBrowser(link)
	}

	type result struct {
		names  []string
		wallet string
	}
	got := make(chan result, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" || r.URL.Query().Get("state") != state {
			http.Error(w, "not for this login", http.StatusBadRequest)
			return
		}
		var names []string
		for _, n := range strings.Split(r.URL.Query().Get("names"), ",") {
			if n = strings.TrimSpace(strings.ToLower(n)); n != "" {
				names = append(names, n)
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>Meshy</title><body style="font-family:-apple-system,Inter,sans-serif;max-width:520px;margin:80px auto;color:#0b1020"><h2>You're signed in</h2><p>Return to the terminal: meshy is checking %s on ENS.</p><p style="color:#8a93b2">You can close this tab.</p></body>`, html.EscapeString(strings.Join(names, ", ")))
		select {
		case got <- result{names, r.URL.Query().Get("wallet")}:
		default:
		}
	})}
	go srv.Serve(ln)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	var res result
	select {
	case res = <-got:
	case <-ctx.Done():
		return errors.New("MESHY_LOGIN_TIMEOUT: nothing came back from the browser")
	}
	if len(res.names) == 0 {
		return errors.New("MESHY_LOGIN_EMPTY: no org was linked to this machine")
	}

	ens, err := ensClient(ctx)
	if err != nil {
		return err
	}
	fmt.Print("Checking on ENS")
	var ok []string
	for _, n := range res.names {
		name := enskit.Name(n)
		label, kind := name.Label(), "person"
		if strings.Contains(n, ".agents.") {
			kind = ""
		}
		org := string(name.Org())
		// The tx may still be propagating to our RPC: retry for a bit.
		var why string
		for i := 0; i < 20; i++ {
			why = ""
			r, err := ens.Lookup(ctx, name)
			if err != nil || r.State.Status != enskit.Registered {
				why = "not registered yet"
			} else if s, _ := r.Text(ctx, ens, "meshy.ssh"); strings.TrimSpace(s) != sshLine {
				why = "meshy.ssh isn't this machine's key yet"
			} else if a, err := ens.Addr(ctx, r.Resolver, name); err != nil || !strings.EqualFold(a.Hex(), device) {
				why = "the name isn't linked to this machine yet"
			}
			if why == "" {
				break
			}
			fmt.Print(".")
			time.Sleep(3 * time.Second)
		}
		if why != "" {
			fmt.Printf("\n  ✗ %s: %s\n", n, why)
			continue
		}
		dir := filepath.Join(h, ".meshy", "person-"+label+"."+org)
		if err := writeIdentity(dir, keyDir, &Identity{Agent: label, Org: org, Kind: kind, Addr: device, SSH: sshLine}); err != nil {
			return err
		}
		ok = append(ok, n)
	}
	fmt.Println()
	if len(ok) == 0 {
		return errors.New("MESHY_LOGIN_FAILED: none of the names point at this machine")
	}
	for _, n := range ok {
		fmt.Printf("MESHY_LOGGED_IN %s\n", n)
	}
	fmt.Println("\nNext: `meshy mesh-join --org <org>` once per org, then `meshy grants <device>` / `meshy ssh <device> -- …`.")
	_ = sshPriv
	return nil
}

// deviceKeys returns this machine's SSH and device keys, creating them once.
func deviceKeys(dir string) (ed25519.PrivateKey, gossh.Signer, *ecdsa.PrivateKey, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, nil, err
	}
	kp, ep := filepath.Join(dir, "id_ed25519"), filepath.Join(dir, "eth.key")
	if _, err := os.Stat(kp); errors.Is(err, os.ErrNotExist) {
		_, priv, _ := ed25519.GenerateKey(rand.Reader)
		blk, err := gossh.MarshalPrivateKey(priv, "meshy-device")
		if err != nil {
			return nil, nil, nil, err
		}
		if err := os.WriteFile(kp, pem.EncodeToMemory(blk), 0o600); err != nil {
			return nil, nil, nil, err
		}
		ek, _ := crypto.GenerateKey()
		if err := os.WriteFile(ep, []byte(fmt.Sprintf("%x\n", crypto.FromECDSA(ek))), 0o600); err != nil {
			return nil, nil, nil, err
		}
	}
	kb, err := os.ReadFile(kp)
	if err != nil {
		return nil, nil, nil, err
	}
	signer, err := gossh.ParsePrivateKey(kb)
	if err != nil {
		return nil, nil, nil, err
	}
	eb, err := os.ReadFile(ep)
	if err != nil {
		return nil, nil, nil, err
	}
	ek, err := crypto.HexToECDSA(strings.TrimSpace(string(eb)))
	return nil, signer, ek, err
}

// writeIdentity creates dir with copies of the device keys and agent.json.
func writeIdentity(dir, keyDir string, id *Identity) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, f := range []string{"id_ed25519", "eth.key"} {
		b, err := os.ReadFile(filepath.Join(keyDir, f))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, f), b, 0o600); err != nil {
			return err
		}
	}
	// keep an existing mesh-join setting
	if b, err := os.ReadFile(filepath.Join(dir, "agent.json")); err == nil {
		var prev Identity
		if json.Unmarshal(b, &prev) == nil {
			id.Mesh = prev.Mesh
		}
	}
	b, _ := json.MarshalIndent(id, "", "  ")
	return os.WriteFile(filepath.Join(dir, "agent.json"), b, 0o600)
}
