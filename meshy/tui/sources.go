package tui

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jashwanth0712/meshy/meshy/audit"
)

// Live data sources. All are plain HTTP / JSON-RPC (no extra deps), each
// bounded by a short timeout so a dead endpoint never stalls the UI.

var httpc = &http.Client{Timeout: 8 * time.Second}

// ---- provider map (GET /v1/map?org=) ----

type mapResult struct {
	Networks []Network
	Grants   []Grant
	Block    uint64
	Err      error
}

func fetchMap(ctx context.Context, api, org string) mapResult {
	u := strings.TrimRight(api, "/") + "/v1/map?org=" + url.QueryEscape(org)
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	resp, err := httpc.Do(req)
	if err != nil {
		return mapResult{Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return mapResult{Err: fmt.Errorf("map: HTTP %d", resp.StatusCode)}
	}
	var raw struct {
		Networks []struct {
			Name, FullName string
			Resources      []struct {
				Name, FullName, Kind string
				Services             []struct{ Name, FullName string }
			}
		}
		Grants []struct {
			FullName, Agent, Target, Device, Status, Kind string
			Expiry                                        int64
		}
		Block uint64
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return mapResult{Err: err}
	}
	var r mapResult
	r.Block = raw.Block
	for _, n := range raw.Networks {
		nw := Network{Name: n.Name, Full: n.FullName}
		for _, d := range n.Resources {
			dv := Device{Name: d.Name, Full: d.FullName, Kind: d.Kind}
			for _, s := range d.Services {
				dv.Services = append(dv.Services, Service{Name: s.Name, Full: s.FullName})
			}
			nw.Devices = append(nw.Devices, dv)
		}
		r.Networks = append(r.Networks, nw)
	}
	for _, g := range raw.Grants {
		r.Grants = append(r.Grants, Grant{Name: g.FullName, Agent: g.Agent, Target: g.Target, Device: g.Device,
			Status: g.Status, Kind: g.Kind, Expiry: time.Unix(g.Expiry, 0)})
	}
	return r
}

// ---- Sepolia head (eth_getBlockByNumber latest) ----

type headResult struct {
	Num  uint64
	TS   time.Time
	Txs  int
	Err  error
	Seen time.Time
}

func rpc(ctx context.Context, endpoint, method string, params ...any) (json.RawMessage, error) {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req, _ := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	req.Header.Set("content-type", "application/json")
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Result json.RawMessage
		Error  *struct{ Message string }
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.Error != nil {
		return nil, fmt.Errorf("%s: %s", method, out.Error.Message)
	}
	return out.Result, nil
}

func hexU(s string) uint64 {
	v, _ := strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 64)
	return v
}

func fetchHead(ctx context.Context, endpoint string) headResult {
	raw, err := rpc(ctx, endpoint, "eth_getBlockByNumber", "latest", false)
	if err != nil {
		return headResult{Err: err}
	}
	var b struct {
		Number, Timestamp string
		Transactions      []json.RawMessage
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		return headResult{Err: err}
	}
	return headResult{Num: hexU(b.Number), TS: time.Unix(int64(hexU(b.Timestamp)), 0), Txs: len(b.Transactions), Seen: time.Now()}
}

// ---- Arkiv (org-wide audit, attributes only; payloads stay sealed) ----

type feedResult struct {
	Events []audit.Event
	Err    error
}

func fetchArkiv(ctx context.Context, endpoint, org string) feedResult {
	var all []audit.Event
	for _, schema := range []string{"meshy.session", "meshy.blocked"} {
		q := fmt.Sprintf("arkiv.schema = str('%s') AND org = str('%s') ORDER BY ts:u64 DESC", schema, strings.ReplaceAll(org, "'", ""))
		raw, err := rpc(ctx, endpoint, "arkiv_query", q, map[string]any{"select": map[string]bool{"key": true, "attributes": true}, "limit": "0x14"})
		if err != nil {
			return feedResult{Err: err}
		}
		var res struct {
			Data []struct {
				Attributes []struct{ Name, Type, Value string }
			}
		}
		// result is either {data:[…]} or a bare array
		if err := json.Unmarshal(raw, &res); err != nil {
			json.Unmarshal(raw, &res.Data)
		}
		for _, it := range res.Data {
			a := map[string]string{}
			for _, at := range it.Attributes {
				a[at.Name] = at.Value
			}
			e := audit.Event{
				Kind: audit.Kind(a["kind"]), Resource: a["resource"], Agent: a["agent"], Grant: a["grant"],
				Session: a["session"], Op: a["op"], Reason: a["reason"], Path: a["path"],
				TS: time.UnixMilli(int64(hexU(a["ts"]))), Block: hexU(a["block"]), Latency: float64(hexU(a["latencyMs"])),
			}
			if e.Kind == "" {
				e.Kind = audit.Kind(strings.TrimPrefix(schema, "meshy."))
			}
			all = append(all, e)
		}
	}
	return feedResult{Events: all}
}

// ---- meshyd control socket ----

func unixClient(sock string, timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", sock)
	}}}
}

type daemonResult struct {
	D      *Daemon
	Recent []audit.Event
	Err    error
}

func fetchDaemon(ctx context.Context, sock string) daemonResult {
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://meshyd/status", nil)
	resp, err := unixClient(sock, 3*time.Second).Do(req)
	if err != nil {
		return daemonResult{Err: fmt.Errorf("meshyd not reachable on %s", sock)}
	}
	defer resp.Body.Close()
	var d struct {
		Daemon
		Recent []audit.Event `json:"recent"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return daemonResult{Err: err}
	}
	return daemonResult{D: &d.Daemon, Recent: d.Recent}
}

func killSession(sock, id string) error {
	resp, err := unixClient(sock, 3*time.Second).Post("http://meshyd/kill/"+url.PathEscape(id), "", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("kill %s: HTTP %d", id, resp.StatusCode)
	}
	return nil
}

// followEvents streams /events?follow=1 into ch until ctx ends, reconnecting.
func followEvents(ctx context.Context, sock string, ch chan<- audit.Event) {
	c := &http.Client{Transport: unixClient(sock, 0).Transport}
	for ctx.Err() == nil {
		req, _ := http.NewRequestWithContext(ctx, "GET", "http://meshyd/events?follow=1", nil)
		resp, err := c.Do(req)
		if err == nil {
			sc := bufio.NewScanner(resp.Body)
			sc.Buffer(make([]byte, 64<<10), 1<<20)
			for sc.Scan() {
				var e audit.Event
				if json.Unmarshal(sc.Bytes(), &e) == nil {
					select {
					case ch <- e:
					case <-ctx.Done():
						resp.Body.Close()
						return
					}
				}
			}
			resp.Body.Close()
		}
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
}

// fetchTxHistory backfills tx counts for the n blocks before head in one
// batched JSON-RPC request, so the sparkline is full on the first frame.
func fetchTxHistory(ctx context.Context, endpoint string, head uint64, n int) []float64 {
	var batch []map[string]any
	for i := n; i >= 1; i-- {
		if uint64(i) > head {
			continue
		}
		batch = append(batch, map[string]any{"jsonrpc": "2.0", "id": i, "method": "eth_getBlockTransactionCountByNumber",
			"params": []any{fmt.Sprintf("0x%x", head-uint64(i))}})
	}
	body, _ := json.Marshal(batch)
	req, _ := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	req.Header.Set("content-type", "application/json")
	resp, err := httpc.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var out []struct {
		ID     int
		Result string
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil {
		return nil
	}
	byID := map[int]float64{}
	for _, r := range out {
		byID[r.ID] = float64(hexU(r.Result))
	}
	var vals []float64
	for i := n; i >= 1; i-- {
		if v, ok := byID[i]; ok {
			vals = append(vals, v)
		}
	}
	return vals
}
