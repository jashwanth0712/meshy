// Package meshyd is the per-resource daemon: an SSH server that admits agents
// from ENSv2 grants, confines each session to its granted folders, audits
// every file operation, and kills sessions the moment their grant dies.
package meshyd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jashwanth0712/meshy/meshy/audit"
	"github.com/jashwanth0712/meshy/meshy/enskit"
)

// Watcher is the subset of enskit.Watcher the server uses.
type Watcher interface {
	Watch(sessionID string, g enskit.Grant)
	Unwatch(sessionID string)
	Healthy(now time.Time) bool
}

// Server is meshyd's SSH front end.
type Server struct {
	Resource string // ENS name of this resource
	HostKey  gossh.Signer
	Auth     Authorizer
	Sandbox  Sandbox
	Watcher  Watcher
	Audit    audit.Sink
	Log      *slog.Logger
	// Shell is the interactive shell when no command is given.
	Shell []string

	mu       sync.Mutex
	sessions map[string]*session
	srv      *ssh.Server
}

type session struct {
	id      string
	authz   *Authz
	argv    []string
	started time.Time
	cmd     *exec.Cmd
	stderr  io.Writer
	once    sync.Once
	done    chan struct{}
	revoked chan string // carries the agent-facing message
}

// SessionInfo is a read-only view for status output.
type SessionInfo struct {
	ID      string
	Agent   string
	Grant   string
	Argv    []string
	Started time.Time
	Expiry  time.Time
	Paths   string
}

const ctxAuthz = "meshy.authz"

func (s *Server) log() *slog.Logger {
	if s.Log == nil {
		return slog.Default()
	}
	return s.Log
}

func (s *Server) emit(e audit.Event) {
	if s.Audit == nil {
		return
	}
	if e.TS.IsZero() {
		e.TS = time.Now()
	}
	e.Resource = s.Resource
	s.Audit.Emit(e)
}

// Serve accepts SSH connections on ln until it is closed.
func (s *Server) Serve(ln net.Listener) error {
	s.mu.Lock()
	if s.sessions == nil {
		s.sessions = map[string]*session{}
	}
	s.srv = &ssh.Server{
		Handler:          s.handle,
		PublicKeyHandler: s.authPublicKey,
		// No port forwarding, no agent forwarding, no X11, no SFTP subsystem
		// outside the sandbox: only exec/shell in the sandbox.
		LocalPortForwardingCallback:   func(ssh.Context, string, uint32) bool { return false },
		ReversePortForwardingCallback: func(ssh.Context, string, uint32) bool { return false },
		IdleTimeout:                   30 * time.Minute,
	}
	if s.HostKey != nil {
		s.srv.AddHostKey(s.HostKey)
	}
	srv := s.srv
	s.mu.Unlock()
	return srv.Serve(ln)
}

// Close stops accepting connections and kills all sessions.
func (s *Server) Close() error {
	s.mu.Lock()
	srv := s.srv
	sessions := make([]*session, 0, len(s.sessions))
	for _, ss := range s.sessions {
		sessions = append(sessions, ss)
	}
	s.mu.Unlock()
	for _, ss := range sessions {
		s.terminate(ss, "MESHY_SHUTDOWN resource is shutting down")
	}
	if srv != nil {
		return srv.Close()
	}
	return nil
}

func (s *Server) authPublicKey(ctx ssh.Context, key ssh.PublicKey) bool {
	actx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if s.Watcher != nil && !s.Watcher.Healthy(time.Now()) {
		s.emit(audit.Event{Kind: audit.Denied, Agent: ctx.User(), Reason: "MESHY_UNAVAILABLE chain view is stale"})
		return false
	}
	az, err := s.Auth.Authorize(actx, ctx.User(), key)
	if err != nil {
		reason := err.Error()
		s.emit(audit.Event{Kind: audit.Denied, Agent: ctx.User(), Reason: reason})
		s.log().Info("login denied", "user", ctx.User(), "remote", ctx.RemoteAddr(), "reason", reason)
		return false
	}
	ctx.SetValue(ctxAuthz, az)
	return true
}

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) handle(sess ssh.Session) {
	az, _ := sess.Context().Value(ctxAuthz).(*Authz)
	if az == nil {
		fmt.Fprintln(sess.Stderr(), "MESHY_DENIED no authorization")
		sess.Exit(1)
		return
	}
	argv, blockMsg := s.commandFor(sess, az)
	if blockMsg != "" {
		s.emit(audit.Event{Kind: audit.Blocked, Agent: az.Agent, Grant: string(az.Grant.Name), Op: "exec", Path: sess.RawCommand(), Reason: "command not allowed"})
		fmt.Fprintln(sess.Stderr(), blockMsg)
		sess.Exit(126)
		return
	}

	ss := &session{
		id: newID(), authz: az, argv: argv, started: time.Now(),
		stderr: sess.Stderr(), done: make(chan struct{}), revoked: make(chan string, 1),
	}
	sink := audit.SinkFunc(func(e audit.Event) {
		e.Session, e.Agent, e.Grant = ss.id, az.Agent, string(az.Grant.Name)
		s.emit(e)
		if e.Kind == audit.Blocked {
			// Tell the agent precisely what happened and how to get access, in
			// a machine-readable line (tools only say "Permission denied").
			fmt.Fprintf(sess.Stderr(), "MESHY_BLOCKED path=%s op=%s reason=%q allowed=%s grant=%s expires=%s ask=admins-of-%s\n",
				e.Path, e.Op, e.Reason, az.Policy.AllowedSummary(), az.Grant.Name,
				az.Expiry.Format("15:04"), az.Grant.Name.Org())
		}
	})
	prep, err := s.Sandbox.Prepare(sess.Context(), SandboxSpec{
		SessionID: ss.id, Agent: az.Agent, Grant: string(az.Grant.Name),
		Policy: az.Policy, Argv: argv, Env: sandboxEnv(az), Audit: sink,
	})
	if err != nil {
		s.log().Error("sandbox prepare failed", "session", ss.id, "err", err)
		fmt.Fprintf(sess.Stderr(), "MESHY_SANDBOX_ERROR %v\n", err)
		sess.Exit(1)
		return
	}
	defer prep.Cleanup()
	cmd := prep.Cmd
	var stderr io.Writer = sess.Stderr()
	if prep.WrapStderr != nil {
		stderr = prep.WrapStderr(stderr)
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = sess, sess, stderr
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	ss.cmd = cmd

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(sess.Stderr(), "MESHY_EXEC_ERROR %v\n", err)
		sess.Exit(127)
		return
	}
	if prep.OnStart != nil {
		prep.OnStart(cmd.Process.Pid)
	}
	s.mu.Lock()
	s.sessions[ss.id] = ss
	s.mu.Unlock()
	if s.Watcher != nil {
		s.Watcher.Watch(ss.id, az.Grant)
	}
	s.emit(audit.Event{Kind: audit.SessionStart, Session: ss.id, Agent: az.Agent, Grant: string(az.Grant.Name), Argv: argv})
	s.log().Info("session started", "session", ss.id, "agent", az.Agent, "argv", argv, "expires", az.Expiry)

	// Local expiry timer: sub-block precision. The watcher also catches
	// expiry on the first block at or after it.
	expTimer := time.AfterFunc(time.Until(az.Expiry), func() {
		s.terminate(ss, fmt.Sprintf("MESHY_EXPIRED grant=%s at %s; session terminated", az.Grant.Name, az.Expiry.Format(time.RFC3339)))
	})
	defer expTimer.Stop()

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	var exitCode int
	select {
	case err := <-waitErr:
		exitCode = exitStatus(err)
	case <-sess.Context().Done(): // client went away
		killGroup(cmd)
		<-waitErr
		exitCode = 255
	case msg := <-ss.revoked:
		fmt.Fprintln(sess.Stderr(), "\n"+msg)
		<-waitErr // terminate already killed the group
		exitCode = 137
	}
	close(ss.done)

	s.mu.Lock()
	delete(s.sessions, ss.id)
	s.mu.Unlock()
	if s.Watcher != nil {
		s.Watcher.Unwatch(ss.id)
	}
	s.emit(audit.Event{Kind: audit.SessionEnd, Session: ss.id, Agent: az.Agent, Grant: string(az.Grant.Name), Reason: fmt.Sprintf("exit %d", exitCode)})
	sess.Exit(exitCode)
}

// commandFor maps the SSH request to argv. Like OpenSSH, a command string is
// run by the shell. If the grant restricts commands, no shell is used and
// shell metacharacters are refused, so `cat x; rm y` cannot bypass the list.
// (The allow-list is defence in depth; the filesystem sandbox is the boundary.)
func (s *Server) commandFor(sess ssh.Session, az *Authz) (argv []string, blockMsg string) {
	raw := strings.TrimSpace(sess.RawCommand())
	if raw == "" {
		argv = s.Shell
		if len(argv) == 0 {
			argv = []string{"/bin/sh"}
		}
		if len(az.Policy.Cmds) > 0 {
			return nil, fmt.Sprintf("MESHY_BLOCKED cmd=<interactive shell> allowed_cmds=%v grant=%s", az.Policy.Cmds, az.Grant.Name)
		}
		return argv, ""
	}
	if len(az.Policy.Cmds) == 0 {
		return []string{"/bin/sh", "-c", raw}, ""
	}
	if strings.ContainsAny(raw, "|;&<>$`\\\n(){}") {
		return nil, fmt.Sprintf("MESHY_BLOCKED cmd=%q reason=\"shell syntax not allowed with a command allow-list\" allowed_cmds=%v grant=%s", raw, az.Policy.Cmds, az.Grant.Name)
	}
	argv = sess.Command()
	if len(argv) == 0 || !az.Policy.AllowsCommand(argv[0]) {
		return nil, fmt.Sprintf("MESHY_BLOCKED cmd=%s allowed_cmds=%v grant=%s", argv[0], az.Policy.Cmds, az.Grant.Name)
	}
	return argv, ""
}

// Revoke terminates a session because its grant died on chain. It is wired
// to enskit.Watcher.OnRevoke.
func (s *Server) Revoke(r enskit.Revocation) {
	s.mu.Lock()
	ss := s.sessions[r.SessionID]
	s.mu.Unlock()
	if ss == nil {
		return
	}
	var blk uint64
	msg := fmt.Sprintf("MESHY_REVOKED grant=%s reason=%q; session terminated", r.Grant, r.Reason)
	if r.Block != nil {
		blk = r.Block.Uint64()
		msg = fmt.Sprintf("MESHY_REVOKED grant=%s block=%d reason=%q; session terminated", r.Grant, blk, r.Reason)
	}
	killed := s.terminate(ss, msg)
	if killed {
		var latency float64
		if !r.HeadSeen.IsZero() {
			latency = float64(time.Since(r.HeadSeen).Microseconds()) / 1000
		}
		s.emit(audit.Event{Kind: audit.SessionRevoke, Session: ss.id, Agent: ss.authz.Agent, Grant: string(r.Grant), Reason: r.Reason, Block: blk, Latency: latency})
		s.log().Warn("session revoked", "session", ss.id, "grant", r.Grant, "reason", r.Reason, "block", blk)
	}
}

// RevokeGrant terminates every session of a grant (manual `meshyd kill`).
func (s *Server) Kill(sessionID, reason string) bool {
	s.mu.Lock()
	ss := s.sessions[sessionID]
	s.mu.Unlock()
	if ss == nil {
		return false
	}
	return s.terminate(ss, "MESHY_KILLED "+reason)
}

// terminate kills the session's process group exactly once.
func (s *Server) terminate(ss *session, msg string) bool {
	killed := false
	ss.once.Do(func() {
		killed = true
		killGroup(ss.cmd)
		select {
		case ss.revoked <- msg:
		default:
		}
	})
	return killed
}

// Sessions lists active sessions.
func (s *Server) Sessions() []SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SessionInfo, 0, len(s.sessions))
	for _, ss := range s.sessions {
		out = append(out, SessionInfo{
			ID: ss.id, Agent: ss.authz.Agent, Grant: string(ss.authz.Grant.Name), Argv: ss.argv,
			Started: ss.started, Expiry: ss.authz.Expiry, Paths: ss.authz.Policy.AllowedSummary(),
		})
	}
	return out
}

func killGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	// Negative pid = whole process group (we set Setpgid).
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Process.Kill()
}

func exitStatus(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return ee.ExitCode()
	}
	return 1
}

func sandboxEnv(az *Authz) []string {
	return []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"HOME=/tmp",
		"LANG=C.UTF-8",
		"MESHY_AGENT=" + az.Agent,
		"MESHY_GRANT=" + string(az.Grant.Name),
		"MESHY_ALLOWED=" + az.Policy.AllowedSummary(),
		"MESHY_EXPIRES=" + az.Expiry.UTC().Format(time.RFC3339),
	}
}
