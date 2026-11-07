package enskit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// A 503 endpoint and a hanging endpoint are skipped; the working one answers.
func TestFailoverSkipsBadEndpoints(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "busy", http.StatusServiceUnavailable)
	}))
	defer down.Close()
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer slow.Close()
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x2a"}`))
	}))
	defer ok.Close()

	urls := []string{down.URL, slow.URL, ok.URL}
	f := &failover{urls: urls, per: 300 * time.Millisecond, base: http.DefaultTransport}
	rc, err := rpc.DialOptions(context.Background(), urls[0], rpc.WithHTTPClient(&http.Client{Transport: f}))
	if err != nil {
		t.Fatal(err)
	}
	c := ethclient.NewClient(rc)
	n, err := c.BlockNumber(context.Background())
	if err != nil {
		t.Fatalf("failover should reach the working endpoint: %v", err)
	}
	if n != 42 {
		t.Fatalf("block = %d, want 42", n)
	}
	if got := f.good.Load(); got != 2 {
		t.Fatalf("remembered endpoint = %d, want 2", got)
	}
}

func TestRPCList(t *testing.T) {
	if got := RPCList(""); len(got) != len(SepoliaRPCs) || !strings.Contains(got[0], "meshy.live") {
		t.Fatalf("default list = %v", got)
	}
	if got := RPCList(" https://a , https://b ,"); len(got) != 2 || got[1] != "https://b" {
		t.Fatalf("parsed = %v", got)
	}
}
