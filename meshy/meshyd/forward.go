package meshyd

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jashwanth0712/meshy/meshy/audit"
	"github.com/jashwanth0712/meshy/meshy/enskit"
)

// directTCPIP handles `ssh -L` / client.Dial channels. The requested
// destination host is interpreted as a service ENS name under this resource
// (e.g. camera.pi.home.acme.eth); the port the client asks for is ignored.
// The daemon dials the target declared in the service's `meshy.service`
// record, only while the agent holds a live grant <agent>.<service>.
func (s *Server) directTCPIP(_ *ssh.Server, conn *gossh.ServerConn, newChan gossh.NewChannel, ctx ssh.Context) {
	var d struct {
		DestAddr   string
		DestPort   uint32
		OriginAddr string
		OriginPort uint32
	}
	if err := gossh.Unmarshal(newChan.ExtraData(), &d); err != nil {
		newChan.Reject(gossh.ConnectionFailed, "MESHY_BAD_REQUEST "+err.Error())
		return
	}
	id, _ := ctx.Value(ctxIdentity).(*Identity)
	if id == nil {
		newChan.Reject(gossh.Prohibited, "MESHY_DENIED no identity")
		return
	}
	if s.Watcher != nil && !s.Watcher.Healthy(time.Now()) {
		newChan.Reject(gossh.Prohibited, "MESHY_UNAVAILABLE chain view is stale")
		return
	}
	actx, cancel := context.WithTimeout(ctx, 15*time.Second)
	sa, err := s.Auth.AuthorizeService(actx, id, enskit.Name(d.DestAddr))
	cancel()
	if err != nil {
		s.emit(audit.Event{Kind: audit.Blocked, Agent: id.Agent, Op: "connect", Path: d.DestAddr, Reason: err.Error()})
		s.log().Info("forward denied", "agent", id.Agent, "service", d.DestAddr, "reason", err)
		newChan.Reject(gossh.Prohibited, err.Error())
		return
	}
	up, err := net.DialTimeout("tcp", sa.Spec.Target, 5*time.Second)
	if err != nil {
		newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("MESHY_SERVICE_DOWN service=%s target=%s: %v", sa.Service, sa.Spec.Target, err))
		return
	}
	ch, reqs, err := newChan.Accept()
	if err != nil {
		up.Close()
		return
	}
	go gossh.DiscardRequests(reqs)

	fw := &forward{id: newID(), id_: id, sa: sa, started: time.Now(), ch: ch, up: up}
	s.mu.Lock()
	s.forwards[fw.id] = fw
	s.mu.Unlock()
	if s.Watcher != nil {
		s.Watcher.Watch(fw.id, sa.Grant)
	}
	s.emit(audit.Event{Kind: audit.SessionStart, Session: fw.id, Agent: id.Agent, Grant: string(sa.Grant.Name), GrantExpiry: sa.Expiry, Op: "connect", Path: string(sa.Service), Argv: []string{"connect", string(sa.Service), sa.Spec.Proto + "://" + sa.Spec.Target}})
	s.log().Info("forward opened", "id", fw.id, "agent", id.Agent, "service", sa.Service, "target", sa.Spec.Target, "expires", sa.Expiry)

	expTimer := time.AfterFunc(time.Until(sa.Expiry), func() { fw.close() })
	var in, out atomic.Int64
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); n, _ := io.Copy(up, ch); in.Add(n); fw.close() }()
	go func() { defer wg.Done(); n, _ := io.Copy(ch, up); out.Add(n); fw.close() }()
	wg.Wait()
	expTimer.Stop()

	s.mu.Lock()
	delete(s.forwards, fw.id)
	s.mu.Unlock()
	if s.Watcher != nil {
		s.Watcher.Unwatch(fw.id)
	}
	s.emit(audit.Event{Kind: audit.SessionEnd, Session: fw.id, Agent: id.Agent, Grant: string(sa.Grant.Name), Op: "connect", Path: string(sa.Service), Bytes: in.Load() + out.Load(), DurationMs: time.Since(fw.started).Milliseconds(), Reason: fmt.Sprintf("closed (%d B in, %d B out)", in.Load(), out.Load())})
}

func (f *forward) close() {
	f.once.Do(func() {
		f.ch.Close()
		f.up.Close()
	})
}

// revokeForward cuts a live forward because its service grant died on chain.
func (s *Server) revokeForward(f *forward, r enskit.Revocation) {
	var blk uint64
	if r.Block != nil {
		blk = r.Block.Uint64()
	}
	// Best effort: tell the client why before closing (as channel stderr).
	_, _ = f.ch.Stderr().Write([]byte(fmt.Sprintf("MESHY_REVOKED grant=%s block=%d reason=%q; connection terminated\n", r.Grant, blk, r.Reason)))
	f.close()
	var latency float64
	if !r.HeadSeen.IsZero() {
		latency = float64(time.Since(r.HeadSeen).Microseconds()) / 1000
	}
	s.emit(audit.Event{Kind: audit.SessionRevoke, Session: f.id, Agent: f.id_.Agent, Grant: string(r.Grant), Op: "connect", Path: string(f.sa.Service), Reason: r.Reason, Block: blk, Latency: latency})
	s.log().Warn("forward revoked", "id", f.id, "grant", r.Grant, "block", blk)
}

// Forwards lists active forwards (for status output).
func (s *Server) Forwards() []SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SessionInfo, 0, len(s.forwards))
	for _, f := range s.forwards {
		out = append(out, SessionInfo{
			ID: f.id, Agent: f.id_.Agent, Grant: string(f.sa.Grant.Name),
			Argv:    []string{"connect", string(f.sa.Service), "→", f.sa.Spec.Target},
			Started: f.started, Expiry: f.sa.Expiry, Paths: "port " + f.sa.Spec.Target,
		})
	}
	return out
}
