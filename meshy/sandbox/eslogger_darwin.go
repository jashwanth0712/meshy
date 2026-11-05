//go:build darwin

package sandbox

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/jashwanth0712/meshy/meshy/audit"
	"github.com/jashwanth0712/meshy/meshy/policy"
)

// ESMonitor records allowed file operations of sandboxed sessions using
// Apple's eslogger (Endpoint Security). Requires root and Full Disk Access
// for meshyd. One eslogger process serves all sessions; events are
// attributed by following each session's process tree via fork events.
type ESMonitor struct {
	Log *slog.Logger

	mu       sync.Mutex
	started  bool
	sessions map[string]*esSession // by session id
	pids     map[int]*esSession    // live pids -> session
}

type esSession struct {
	id   string
	pol  *policy.Policy
	sink audit.Sink
}

// Events we subscribe to. close carries `modified` for writes.
var esEvents = []string{"fork", "exit", "open", "close", "create", "rename", "unlink"}

// Start launches eslogger. Safe to call once; returns an error if eslogger
// cannot run (not root, no Full Disk Access).
func (m *ESMonitor) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil
	}
	m.started = true
	m.sessions, m.pids = map[string]*esSession{}, map[int]*esSession{}
	m.mu.Unlock()

	cmd := exec.CommandContext(ctx, "eslogger", append(esEvents, "--format", "json")...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr safeBuf
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("eslogger: %w (meshyd must run as root with Full Disk Access)", err)
	}
	go func() {
		m.consume(out)
		err := cmd.Wait()
		m.log().Error("eslogger exited", "err", err, "stderr", stderr.String())
	}()
	// eslogger fails fast if not entitled; give it a moment.
	time.Sleep(300 * time.Millisecond)
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return fmt.Errorf("eslogger exited: %s", stderr.String())
	}
	return nil
}

func (m *ESMonitor) log() *slog.Logger {
	if m.Log == nil {
		return slog.Default()
	}
	return m.Log
}

// Track registers a session; the returned OnStart must be called with the
// sandbox-exec pid, and stop when the session ends.
func (m *ESMonitor) Track(id string, _ *exec.Cmd, pol *policy.Policy, sink audit.Sink) (stop func()) {
	s := &esSession{id: id, pol: pol, sink: sink}
	m.mu.Lock()
	if m.sessions == nil {
		m.sessions, m.pids = map[string]*esSession{}, map[int]*esSession{}
	}
	m.sessions[id] = s
	m.mu.Unlock()
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		delete(m.sessions, id)
		for pid, ss := range m.pids {
			if ss == s {
				delete(m.pids, pid)
			}
		}
	}
}

// Attach binds the session's root pid.
func (m *ESMonitor) Attach(id string, pid int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.sessions[id]; s != nil {
		m.pids[pid] = s
	}
}

// esEvent is the subset of eslogger's JSON we read.
type esEvent struct {
	Process struct {
		AuditToken struct {
			PID int `json:"pid"`
		} `json:"audit_token"`
	} `json:"process"`
	Event map[string]json.RawMessage `json:"event"`
}

type esFile struct {
	Path string `json:"path"`
}

func (m *ESMonitor) consume(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	for sc.Scan() {
		m.handle(sc.Bytes())
	}
}

// handle processes one eslogger JSON line (exported for tests via HandleLine).
func (m *ESMonitor) handle(line []byte) {
	var ev esEvent
	if json.Unmarshal(line, &ev) != nil {
		return
	}
	pid := ev.Process.AuditToken.PID
	m.mu.Lock()
	s := m.pids[pid]
	m.mu.Unlock()
	if s == nil {
		return
	}
	for kind, raw := range ev.Event {
		switch kind {
		case "fork":
			var f struct {
				Child struct {
					AuditToken struct {
						PID int `json:"pid"`
					} `json:"audit_token"`
				} `json:"child"`
			}
			if json.Unmarshal(raw, &f) == nil && f.Child.AuditToken.PID != 0 {
				m.mu.Lock()
				m.pids[f.Child.AuditToken.PID] = s
				m.mu.Unlock()
			}
		case "exit":
			m.mu.Lock()
			delete(m.pids, pid)
			m.mu.Unlock()
		case "open":
			var o struct {
				File esFile `json:"file"`
			}
			if json.Unmarshal(raw, &o) == nil {
				m.op(s, "open", o.File.Path)
			}
		case "close":
			var c struct {
				Modified bool   `json:"modified"`
				Target   esFile `json:"target"`
			}
			if json.Unmarshal(raw, &c) == nil && c.Modified {
				m.op(s, "write", c.Target.Path)
			}
		case "create":
			var c struct {
				Destination struct {
					ExistingFile *esFile `json:"existing_file"`
					NewPath      *struct {
						Dir      esFile `json:"dir"`
						Filename string `json:"filename"`
					} `json:"new_path"`
				} `json:"destination"`
			}
			if json.Unmarshal(raw, &c) == nil {
				switch {
				case c.Destination.NewPath != nil:
					m.op(s, "create", filepath.Join(c.Destination.NewPath.Dir.Path, c.Destination.NewPath.Filename))
				case c.Destination.ExistingFile != nil:
					m.op(s, "create", c.Destination.ExistingFile.Path)
				}
			}
		case "unlink":
			var u struct {
				Target esFile `json:"target"`
			}
			if json.Unmarshal(raw, &u) == nil {
				m.op(s, "unlink", u.Target.Path)
			}
		case "rename":
			var rn struct {
				Source esFile `json:"source"`
			}
			if json.Unmarshal(raw, &rn) == nil {
				m.op(s, "rename", rn.Source.Path)
			}
		}
	}
}

// op records only accesses inside the grant: system reads (dyld, libs)
// are noise, and outside-grant attempts are denied by Seatbelt anyway.
func (m *ESMonitor) op(s *esSession, op, path string) {
	if path == "" || !s.pol.Check(path, false).Allowed {
		return
	}
	if s.sink != nil {
		s.sink.Emit(audit.Event{Kind: audit.Op, TS: time.Now(), Op: op, Path: path})
	}
}

type safeBuf struct {
	mu sync.Mutex
	b  []byte
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.b) < 8192 {
		s.b = append(s.b, p...)
	}
	return len(p), nil
}
func (s *safeBuf) String() string { s.mu.Lock(); defer s.mu.Unlock(); return string(s.b) }
