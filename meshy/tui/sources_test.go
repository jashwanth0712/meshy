package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jashwanth0712/meshy/meshy/audit"
)

// A fake meshyd control socket speaking the same API as cmd/meshyd/control.go.
func TestControlSocket(t *testing.T) {
	sock := filepath.Join(os.TempDir(), fmt.Sprintf("meshy-tui-%d.sock", os.Getpid()))
	os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skip(err)
	}
	defer ln.Close()
	killed := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"resource":"db1.prod.acme.eth","healthy":true,"headNumber":7,"sessions":[{"ID":"abc","Agent":"claude-1","Grant":"claude-1.db1.prod.acme.eth"}],"recent":[{"kind":"blocked","ts":"2026-11-07T09:00:00Z","path":"/etc/passwd"}]}`)
	})
	mux.HandleFunc("POST /kill/{id}", func(w http.ResponseWriter, r *http.Request) { killed <- r.PathValue("id"); fmt.Fprintln(w, "killed") })
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(audit.Event{Kind: audit.SessionRevoke, TS: time.Now(), Grant: "claude-1.db1.prod.acme.eth", Latency: 900})
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	go http.Serve(ln, mux)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	d := fetchDaemon(ctx, sock)
	if d.Err != nil || d.D.Resource != "db1.prod.acme.eth" || len(d.D.Sessions) != 1 || len(d.Recent) != 1 {
		t.Fatalf("status: %+v", d)
	}
	if err := killSession(sock, "abc"); err != nil || <-killed != "abc" {
		t.Fatal("kill", err)
	}
	ch := make(chan audit.Event, 1)
	go followEvents(ctx, sock, ch)
	select {
	case e := <-ch:
		if e.Kind != audit.SessionRevoke || e.Latency != 900 {
			t.Fatalf("event %+v", e)
		}
	case <-ctx.Done():
		t.Fatal("no streamed event")
	}
	st := collect(ctx, Options{Control: sock, Mode: "daemon", RPC: "http://127.0.0.1:1", API: "http://127.0.0.1:1"})
	if st.Org != "acme.eth" || st.Me != "db1.prod.acme.eth" || st.Daemon == nil {
		t.Fatalf("collect: org=%q me=%q", st.Org, st.Me)
	}
}
