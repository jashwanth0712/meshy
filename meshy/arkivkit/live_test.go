package arkivkit

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
)

func TestLiveWrite(t *testing.T) {
	url := os.Getenv("LIVE_ARKIV")
	if url == "" {
		t.Skip()
	}
	b, _ := os.ReadFile(os.Getenv("HOME") + "/.meshy/meshyd-demo/eth.key")
	k, err := crypto.HexToECDSA(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	t0 := time.Now()
	w, err := Dial(ctx, url, k)
	if err != nil {
		t.Fatal("dial:", err)
	}
	t.Logf("dial %s chain=%s from=%s", time.Since(t0), w.ChainID, w.From().Hex())
	tx, err := w.Execute(ctx, Create{MinLifetime: 1000, Flags: FlagReadOnly, Attrs: []Attr{Schema("meshy.selftest"), Str("org", "meshy-selftest", true), U64("ts", uint64(time.Now().UnixMilli()), true)}})
	t.Logf("execute after %s: tx=%s err=%v", time.Since(t0), tx.Hex(), err)
	if err != nil {
		t.Fatal(err)
	}
}
