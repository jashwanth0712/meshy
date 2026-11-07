package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// inviteBlob is what web/src/lib/orgs.ts puts in /join/<base64url(json)>.
type inviteBlob struct {
	Org  string `json:"org"`
	Role string `json:"role"`
	Inv  struct {
		ParentName string `json:"parentName"`
		Label      string `json:"label"`
	} `json:"inv"`
}

var labelRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])?$`)

// cmdJoinOrg joins an org as a person (member) from a People-page invite link:
//
//  1. create this device's SSH + eth keys locally (nothing leaves the machine)
//  2. open the invite with ?ssh=<key>&device=<addr>; the human accepts it in the
//     browser with their wallet (claim the name, then point it at this device)
//  3. wait until <name>.people.<org> carries our meshy.ssh and addr on ENS
func cmdJoinOrg(args []string) error {
	fs := flag.NewFlagSet("join-org", flag.ExitOnError)
	name := fs.String("name", "", "the name you'll claim in the org (if the invite doesn't fix one)")
	noOpen := fs.Bool("no-open", false, "print the link instead of opening a browser")
	timeout := fs.Duration("timeout", 30*time.Minute, "how long to wait for the claim")
	// flags may follow the link
	var rest []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			rest = append(rest, args[i:]...)
			args = args[:i]
			break
		}
	}
	fs.Parse(rest)
	if len(args) != 1 {
		return errors.New("usage: meshy join-org <invite-link> [--name alice]")
	}
	u, err := url.Parse(args[0])
	if err != nil || !strings.HasPrefix(u.Path, "/join/") {
		return errors.New("that isn't a Meshy invite link (https://meshy.live/join/…)")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(u.Path, "/join/"))
	if err != nil {
		return fmt.Errorf("bad invite link: %w", err)
	}
	b, err := parseInvite(raw)
	if err != nil {
		return err
	}
	if b.Role != "member" {
		return errors.New("this invite is for an agent; run `meshy join` from Directory → Invite agent instead")
	}
	label := b.Inv.Label
	if label == "" {
		label = strings.ToLower(strings.TrimSpace(*name))
	}
	if !labelRe.MatchString(label) {
		return errors.New("choose your name with --name (1–32 lowercase letters, numbers or dashes)")
	}
	if os.Getenv("MESHY_HOME") == "" {
		// keep people and agents on one machine apart by default
		h, _ := os.UserHomeDir()
		os.Setenv("MESHY_HOME", filepath.Join(h, ".meshy", "person-"+label+"."+b.Org))
	}
	id, err := createIdentity(label, b.Org)
	if err != nil {
		return err
	}
	if id.Kind != "person" {
		id.Kind = "person"
		bb, _ := json.MarshalIndent(id, "", "  ")
		if err := os.WriteFile(filepath.Join(id.dir, "agent.json"), bb, 0o600); err != nil {
			return err
		}
	}
	q := u.Query()
	q.Set("ssh", id.SSH)
	q.Set("device", id.Addr)
	q.Set("name", label)
	u.RawQuery = q.Encode()
	link := u.String()

	fmt.Printf("Person identity: %s\n  device %s\n  ssh    %s\n  keys   %s\n\n", id.Name(), id.Addr, fingerprint(id.key.PublicKey()), id.dir)
	fmt.Printf("MESHY_CLAIM_URL %s\n", link)
	fmt.Println("Open this link, connect your wallet and accept the invite (2 transactions: claim your name, then link this device).")
	if !*noOpen {
		openBrowser(link)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	ens, err := ensClient(ctx)
	if err != nil {
		return err
	}
	fmt.Print("Waiting for your claim on ENS")
	for {
		ok, why := joined(ctx, ens, id)
		if ok {
			fmt.Printf("\nMESHY_JOINED member=%s (export MESHY_HOME=%s)\n", id.Name(), id.dir)
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

// parseInvite reads a /join/ link: the compact v2 form (web/src/lib/orgs.ts
// encodeInvite: [0]=2, [1]=flags, …, signature to byte 91, optional 20-byte
// invitee, then "org\0label") or the older base64url JSON.
func parseInvite(raw []byte) (inviteBlob, error) {
	var b inviteBlob
	if len(raw) > 0 && raw[0] == 2 {
		if len(raw) < 92 {
			return b, errors.New("bad invite link: too short")
		}
		at := 91
		if raw[1]&2 != 0 {
			at += 20
		}
		if len(raw) <= at {
			return b, errors.New("bad invite link: no org")
		}
		org, label, _ := strings.Cut(string(raw[at:]), "\x00")
		b.Org, b.Role = org, "member"
		if raw[1]&1 != 0 {
			b.Role = "agent"
		}
		b.Inv.Label = label
		b.Inv.ParentName = "people." + org
		if b.Role == "agent" {
			b.Inv.ParentName = "agents." + org
		}
		return b, nil
	}
	if err := json.Unmarshal(raw, &b); err != nil || b.Org == "" {
		return b, errors.New("bad invite link: can't read the invite")
	}
	return b, nil
}
