package main

import (
	"bufio"
	"context"
	"crypto/ecdh"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/jashwanth0712/meshy/meshy/audit"
	"github.com/jashwanth0712/meshy/meshy/enskit"
	"github.com/jashwanth0712/meshy/meshy/meshyd"
	"github.com/jashwanth0712/meshy/meshy/tui"
)

type ecdhPub = ecdh.PublicKey

func commonAddr(s string) ethcommon.Address { return ethcommon.HexToAddress(s) }

// Status is returned by GET /status.
type Status struct {
	Resource   string               `json:"resource"`
	Addr       string               `json:"addr"`
	Sandbox    string               `json:"sandbox"`
	Healthy    bool                 `json:"healthy"`
	HeadNumber uint64               `json:"headNumber"`
	HeadAgeSec float64              `json:"headAgeSec"`
	Sessions   []meshyd.SessionInfo `json:"sessions"`
	Recent     []audit.Event        `json:"recent"`
}

func serveControl(path string, srv *meshyd.Server, w *enskit.Watcher, ring *audit.Ring, sandboxName string, keys *Keys) (io.Closer, error) {
	os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("control socket: %w", err)
	}
	os.Chmod(path, 0o600)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(rw http.ResponseWriter, r *http.Request) {
		h := w.LastHead()
		st := Status{
			Resource: srv.Resource, Addr: crypto.PubkeyToAddress(keys.Eth.PublicKey).Hex(), Sandbox: sandboxName,
			Healthy: w.Healthy(time.Now()), Sessions: srv.Sessions(),
		}
		if h.Number != nil {
			st.HeadNumber, st.HeadAgeSec = h.Number.Uint64(), time.Since(h.SeenAt).Seconds()
		}
		evs := ring.Snapshot()
		for i := len(evs) - 1; i >= 0 && len(st.Recent) < 15; i-- {
			if evs[i].Kind != audit.Op {
				st.Recent = append(st.Recent, evs[i])
			}
		}
		json.NewEncoder(rw).Encode(st)
	})
	mux.HandleFunc("POST /kill/{id}", func(rw http.ResponseWriter, r *http.Request) {
		if !srv.Kill(r.PathValue("id"), "by operator") {
			http.Error(rw, "no such session", http.StatusNotFound)
			return
		}
		fmt.Fprintln(rw, "killed")
	})
	mux.HandleFunc("GET /events", func(rw http.ResponseWriter, r *http.Request) {
		enc := json.NewEncoder(rw)
		for _, e := range ring.Snapshot() {
			enc.Encode(e)
		}
		if r.URL.Query().Get("follow") == "" {
			return
		}
		fl, _ := rw.(http.Flusher)
		sub := ring.Subscribe()
		for {
			if fl != nil {
				fl.Flush()
			}
			select {
			case <-r.Context().Done():
				return
			case e := <-sub:
				enc.Encode(e)
			}
		}
	})
	go http.Serve(ln, mux)
	return ln, nil
}

func client(sock string) *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", sock)
	}}}
}

func ctlFlags(name string, args []string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	sock := fs.String("control", env("MESHY_CONTROL", defaultControl()), "control socket")
	return fs, sock
}

func getStatus(sock string) (*Status, error) {
	resp, err := client(sock).Get("http://meshyd/status")
	if err != nil {
		return nil, fmt.Errorf("is meshyd running? %w", err)
	}
	defer resp.Body.Close()
	var st Status
	return &st, json.NewDecoder(resp.Body).Decode(&st)
}

func cmdStatus(args []string) error {
	fs, sock := ctlFlags("status", args)
	watch := fs.Bool("w", false, "watch: opens the dashboard on a terminal")
	plain := fs.Bool("plain", false, "with -w: the plain refreshing text view")
	fs.Parse(args)
	if *watch && !*plain && term.IsTerminal(os.Stdout.Fd()) {
		return runTop(tui.Options{Control: *sock, NoSplash: true})
	}
	for {
		st, err := getStatus(*sock)
		if err != nil {
			return err
		}
		if *watch {
			fmt.Print("\033[H\033[2J")
		}
		renderStatus(os.Stdout, st)
		if !*watch {
			return nil
		}
		time.Sleep(time.Second)
	}
}

func renderStatus(w io.Writer, st *Status) {
	health := "\033[32m● chain fresh\033[0m"
	if !st.Healthy {
		health = "\033[31m● CHAIN STALE — failing closed\033[0m"
	}
	fmt.Fprintf(w, "\033[1mmeshyd\033[0m  %s   %s\n", st.Resource, health)
	fmt.Fprintf(w, "  addr %s   sandbox %s   sepolia #%d (%.0fs ago)\n\n", st.Addr, st.Sandbox, st.HeadNumber, st.HeadAgeSec)
	fmt.Fprintf(w, "\033[1mSESSIONS (%d)\033[0m\n", len(st.Sessions))
	sort.Slice(st.Sessions, func(i, j int) bool { return st.Sessions[i].Started.Before(st.Sessions[j].Started) })
	for _, s := range st.Sessions {
		left := time.Until(s.Expiry).Round(time.Second)
		fmt.Fprintf(w, "  %s  %-10s %-34s %6s left  %s  $ %s\n", s.ID, s.Agent, s.Grant, left, s.Paths, strings.Join(s.Argv, " "))
	}
	if len(st.Sessions) == 0 {
		fmt.Fprintln(w, "  (none)")
	}
	fmt.Fprintf(w, "\n\033[1mRECENT\033[0m\n")
	for _, e := range st.Recent {
		color := "\033[0m"
		switch e.Kind {
		case audit.Blocked, audit.Denied:
			color = "\033[31m"
		case audit.SessionRevoke:
			color = "\033[33m"
		case audit.SessionStart:
			color = "\033[32m"
		}
		detail := e.Path
		if e.Kind == audit.SessionRevoke {
			detail = fmt.Sprintf("block %d, killed %.0fms after head", e.Block, e.Latency)
		}
		if e.Reason != "" && e.Kind != audit.SessionRevoke {
			detail += "  (" + e.Reason + ")"
		}
		fmt.Fprintf(w, "  %s %s%-14s\033[0m %-10s %s\n", e.TS.Format("15:04:05"), color, e.Kind, e.Agent, detail)
	}
}

// cmdTop is the mission-control dashboard (meshy/tui). On a non-TTY stdout
// it prints the same state as JSON, so agents can call it safely.
func cmdTop(args []string) error {
	fs, sock := ctlFlags("top", args)
	var o tui.Options
	o.Flags(fs)
	fs.Parse(args)
	o.Control = *sock
	if o.Org == "" && os.Getenv("MESHY_NAME") != "" {
		o.Org = string(enskit.Name(os.Getenv("MESHY_NAME")).Org())
	}
	return runTop(o)
}

func runTop(o tui.Options) error {
	o.Mode = "daemon"
	if o.Demo {
		o.Control = ""
	}
	return tui.Run(context.Background(), o)
}

func cmdSessions(args []string) error {
	fs, sock := ctlFlags("sessions", args)
	fs.Parse(args)
	st, err := getStatus(*sock)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(st.Sessions)
}

func cmdKill(args []string) error {
	fs, sock := ctlFlags("kill", args)
	fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("usage: meshyd kill <session-id>")
	}
	resp, err := client(*sock).Post("http://meshyd/kill/"+fs.Arg(0), "", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	fmt.Print(string(b))
	return nil
}

func cmdLogs(args []string) error {
	fs, sock := ctlFlags("logs", args)
	follow := fs.Bool("f", false, "follow")
	fs.Parse(args)
	u := "http://meshyd/events"
	if *follow {
		u += "?follow=1"
	}
	resp, err := client(*sock).Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		fmt.Println(sc.Text())
	}
	return sc.Err()
}

func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	var c common
	c.register(fs)
	sepolia := fs.String("sepolia", env("MESHY_SEPOLIA", ""), "Sepolia RPC")
	fs.Parse(args)
	ok := func(name string, err error) {
		if err != nil {
			fmt.Printf("  \033[31m✗\033[0m %-22s %v\n", name, err)
		} else {
			fmt.Printf("  \033[32m✓\033[0m %s\n", name)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err := loadOrCreateKeys(c.state, false)
	ok("keys in "+c.state, err)
	sb, err := platformSandbox(ctx, c.rootList(), c.state)
	if err == nil {
		err = sb.Check()
		ok("sandbox "+sb.Name(), err)
	} else {
		ok("sandbox", err)
	}
	if *sepolia == "" {
		ok("sepolia rpc", errors.New("--sepolia / MESHY_SEPOLIA not set"))
		return nil
	}
	ethc, err := dialEth(ctx, *sepolia)
	ok("sepolia rpc", err)
	if err == nil && c.name != "" {
		ens := enskit.NewClient(ethc, enskit.SepoliaUniversalHelper)
		owner, err := ens.ExactOwner(ctx, enskit.Name(c.name), nil)
		if err == nil && owner == (ethcommon.Address{}) {
			err = fmt.Errorf("%s is not registered (or an ancestor expired)", c.name)
		}
		ok("ens name "+c.name, err)
		if _, err := orgKey(ctx, ens, enskit.Name(c.name).Org()); err != nil {
			ok("org meshy.orgkey", err)
		} else {
			ok("org meshy.orgkey", nil)
		}
	}
	return nil
}

func dialEth(ctx context.Context, url string) (*ethclient.Client, error) {
	c, err := ethclient.DialContext(ctx, url)
	if err != nil {
		return nil, err
	}
	_, err = c.BlockNumber(ctx)
	return c, err
}
