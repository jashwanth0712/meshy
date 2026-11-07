package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/crypto"

	"github.com/jashwanth0712/meshy/meshy/enskit"
	"github.com/jashwanth0712/meshy/meshy/provider"
)

// cmdRequest asks the admins of an org for something; they see it in their
// Meshy menu and on the dashboard, and sign it there. No link to send.
//
//	meshy request access <device|service> [--path /folder] [--mode ro|rw] [--dur 3600] [--for claude-1]
//	meshy request device <device>.<network>.<org> --join-code meshy-join:…
//	meshy request roots <device> --roots /Users/me/a,/Users/me/b
//	meshy request list <org>
//	meshy request withdraw <id> --org <org>
func cmdRequest(args []string) error {
	if len(args) < 2 {
		return errors.New("usage: meshy request access <target> [--path p --mode ro|rw --dur s --for label] | device <name> --join-code c | list <org> | withdraw <id> --org o")
	}
	kind, rest := args[0], args[1:]
	fs := flag.NewFlagSet("request", flag.ExitOnError)
	path := fs.String("path", "", "folder on the device (access to a device)")
	mode := fs.String("mode", "ro", "ro or rw")
	dur := fs.Int("dur", 3600, "seconds")
	forLabel := fs.String("for", "", "who gets the grant (default: you)")
	join := fs.String("join-code", "", "meshyd join code (device)")
	roots := fs.String("roots", "", "comma-separated folders the device shares from now on (roots)")
	org := fs.String("org", "", "org (withdraw)")
	api := fs.String("provider", env("MESHY_PROVIDER", "https://api.meshy.live"), "meshy-sync URL")
	target := rest[0]
	fs.Parse(rest[1:])
	base := strings.TrimRight(*api, "/")

	switch kind {
	case "list":
		resp, err := http.Get(base + "/v1/requests?org=" + target)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, err = io.Copy(stdout, resp.Body)
		return err
	case "withdraw":
		if *org == "" {
			return errors.New("usage: meshy request withdraw <id> --org <org>")
		}
		id, err := loadIdentityFor(enskit.Name("x." + *org))
		if err != nil {
			return err
		}
		at := time.Now().Unix()
		sig, err := provider.SignText(provider.WithdrawMessage(target, string(id.Name()), at), keySigner{id})
		if err != nil {
			return err
		}
		return postJSON(base+"/v1/requests/withdraw", map[string]any{"id": target, "from": id.Name(), "at": at, "sig": sig}, nil)
	case "access", "device", "roots":
	default:
		return fmt.Errorf("unknown request kind %q", kind)
	}
	name := enskit.Name(strings.ToLower(target))
	id, err := loadIdentityFor(name)
	if err != nil {
		return err
	}
	r := provider.Request{Org: string(name.Org()), From: string(id.Name()), Kind: kind, Target: string(name), At: time.Now().Unix()}
	if kind == "access" {
		r.For, r.Path, r.Mode, r.Dur = *forLabel, *path, *mode, *dur
		if r.For == "" {
			r.For = id.Agent
		}
		// No --path: "any of its folders", the admin picks; the mode is still what was asked.
	} else if kind == "roots" {
		for _, p := range strings.Split(*roots, ",") {
			if p = strings.TrimSpace(p); p != "" {
				r.Roots = append(r.Roots, p)
			}
		}
	} else {
		r.JoinCode = *join
	}
	if err := r.Validate(time.Now()); err != nil {
		return err
	}
	if r.Sig, err = provider.SignText(provider.RequestMessage(r), keySigner{id}); err != nil {
		return err
	}
	var got provider.Request
	if err := postJSON(base+"/v1/requests", r, &got); err != nil {
		return err
	}
	fmt.Printf("MESHY_REQUESTED id=%s org=%s kind=%s target=%s\n", got.ID, got.Org, got.Kind, got.Target)
	fmt.Println("Admins of " + got.Org + " see it in their Meshy menu and on the dashboard.")
	return nil
}

type keySigner struct{ id *Identity }

func (k keySigner) Sign(h []byte) ([]byte, error) { return crypto.Sign(h, k.id.eth) }

func postJSON(url string, body, out any) error {
	b, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return errors.New("MESHY_NO_REQUESTS this provider doesn't take requests yet")
	}
	if resp.StatusCode != 200 {
		var e struct{ Error string }
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return fmt.Errorf("provider: HTTP %d", resp.StatusCode)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	_, err = stdout.Write(raw)
	return err
}
