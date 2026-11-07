package enskit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// SepoliaRPCs are the default Sepolia HTTP endpoints, tried in order.
// meshy.live/api/rpc is the dashboard's Alchemy-backed proxy: the Alchemy key
// stays on the server, so nothing secret ships in the binaries. The public
// nodes behind it are fallbacks for when the proxy is slow or down.
var SepoliaRPCs = []string{
	"https://meshy.live/api/rpc",
	"https://ethereum-sepolia-rpc.publicnode.com",
	"https://sepolia.drpc.org",
	"https://1rpc.io/sepolia",
}

// RPCList turns a comma-separated setting (e.g. $MESHY_SEPOLIA) into a list,
// falling back to SepoliaRPCs when it is empty.
func RPCList(s string) []string {
	var out []string
	for _, u := range strings.Split(s, ",") {
		if u = strings.TrimSpace(u); u != "" {
			out = append(out, u)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), SepoliaRPCs...)
	}
	return out
}

// DialFailover connects to Sepolia over HTTP with per-request failover: each
// JSON-RPC request goes to the endpoint that last worked, and on a timeout,
// connection error, 429 or 5xx it is retried on the next one. A single ws(s)://
// URL is dialled directly (subscriptions can't fail over per request).
func DialFailover(ctx context.Context, urls []string) (*ethclient.Client, error) {
	if len(urls) == 0 {
		urls = SepoliaRPCs
	}
	if len(urls) == 1 && strings.HasPrefix(urls[0], "ws") {
		return ethclient.DialContext(ctx, urls[0])
	}
	var hs []string
	for _, u := range urls {
		if strings.HasPrefix(u, "http") {
			hs = append(hs, u)
		}
	}
	if len(hs) == 0 {
		return nil, fmt.Errorf("enskit: no http(s) RPC endpoint in %v", urls)
	}
	hc := &http.Client{Transport: &failover{urls: hs, per: 8 * time.Second, base: http.DefaultTransport}}
	rc, err := rpc.DialOptions(ctx, hs[0], rpc.WithHTTPClient(hc))
	if err != nil {
		return nil, err
	}
	return ethclient.NewClient(rc), nil
}

type failover struct {
	urls []string
	per  time.Duration // per-attempt timeout
	base http.RoundTripper
	good atomic.Int32 // index of the endpoint that last answered
}

func (f *failover) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, err
		}
		body = b
	}
	start := int(f.good.Load())
	var lastErr error
	for i := range f.urls {
		idx := (start + i) % len(f.urls)
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		resp, err := f.try(req, f.urls[idx], body)
		if err == nil {
			f.good.Store(int32(idx))
			return resp, nil
		}
		lastErr = fmt.Errorf("%s: %w", host(f.urls[idx]), err)
	}
	return nil, lastErr
}

func (f *failover) try(req *http.Request, target string, body []byte) (*http.Response, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(req.Context(), f.per)
	defer cancel()
	r := req.Clone(ctx)
	r.URL, r.Host = u, u.Host
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	resp, err := f.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	// Read the whole answer before the attempt's timeout is cancelled.
	b, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, errors.New(resp.Status)
	}
	resp.Body = io.NopCloser(bytes.NewReader(b))
	resp.ContentLength = int64(len(b))
	return resp, nil
}

func host(s string) string {
	if u, err := url.Parse(s); err == nil {
		return u.Host
	}
	return s
}
