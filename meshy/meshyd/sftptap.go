package meshyd

// File telemetry for Finder volumes (`meshy mount`). The session runs the
// system sftp-server inside the sandbox, which enforces the folder policy; the
// tap only watches the SFTP packets going both ways and turns them into audit
// events: open/read/write (bytes per file, on close), list, mkdir, delete,
// rename, and every request the sandbox refused (PERMISSION_DENIED) as a block.
// It works without root, unlike eslogger, so Mac devices get file-level
// telemetry on Arkiv too.

import (
	"encoding/binary"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/jashwanth0712/meshy/meshy/audit"
)

const (
	fxpOpen     = 3
	fxpClose    = 4
	fxpRead     = 5
	fxpWrite    = 6
	fxpOpendir  = 11
	fxpRemove   = 13
	fxpMkdir    = 14
	fxpRmdir    = 15
	fxpRename   = 18
	fxpStatus   = 101
	fxpHandle   = 102
	fxpData     = 103
	fxStatusOK  = 0
	fxPermDeny  = 3
	fxpWriteBit = 0x02 // SSH_FXF_WRITE
)

type sftpReq struct {
	typ  byte
	path string
	to   string
}

type sftpFile struct {
	path     string
	dir      bool
	write    bool
	read     int64
	written  int64
	listings int
}

// SFTPTap decodes both directions of an SFTP stream into audit events.
type SFTPTap struct {
	Emit func(audit.Event)

	mu      sync.Mutex
	pending map[uint32]sftpReq   // request id -> what was asked
	handles map[string]*sftpFile // open handle -> file
	reads   map[uint32]string    // read request id -> handle
}

func NewSFTPTap(emit func(audit.Event)) *SFTPTap {
	return &SFTPTap{Emit: emit, pending: map[uint32]sftpReq{}, handles: map[string]*sftpFile{}, reads: map[uint32]string{}}
}

// Client wraps what the client sends (requests).
func (t *SFTPTap) Client(r io.Reader) io.Reader { return &tapReader{r: r, on: t.request} }

// Server wraps what sftp-server answers (responses).
func (t *SFTPTap) Server(w io.Writer) io.Writer { return &tapWriter{w: w, on: t.response} }

func (t *SFTPTap) op(op, path string, n int64) {
	t.Emit(audit.Event{Kind: audit.Op, TS: time.Now(), Op: op, Path: path, Bytes: n})
}

func (t *SFTPTap) request(p []byte) {
	if len(p) < 5 {
		return
	}
	typ := p[0]
	id := binary.BigEndian.Uint32(p[1:5])
	b := p[5:]
	t.mu.Lock()
	defer t.mu.Unlock()
	switch typ {
	case fxpOpen:
		path, rest := sshString(b)
		pflags := uint32(0)
		if len(rest) >= 4 {
			pflags = binary.BigEndian.Uint32(rest)
		}
		r := sftpReq{typ: typ, path: path}
		if pflags&fxpWriteBit != 0 {
			r.to = "w"
		}
		t.pending[id] = r
	case fxpOpendir, fxpRemove, fxpMkdir, fxpRmdir:
		path, _ := sshString(b)
		t.pending[id] = sftpReq{typ: typ, path: path}
	case fxpRename:
		from, rest := sshString(b)
		to, _ := sshString(rest)
		t.pending[id] = sftpReq{typ: typ, path: from, to: to}
	case fxpRead:
		h, _ := sshString(b)
		t.reads[id] = h
	case fxpWrite:
		h, rest := sshString(b)
		if f := t.handles[h]; f != nil && len(rest) >= 12 {
			f.written += int64(binary.BigEndian.Uint32(rest[8:12]))
		}
	case fxpClose:
		h, _ := sshString(b)
		if f := t.handles[h]; f != nil {
			delete(t.handles, h)
			t.mu.Unlock()
			switch {
			case f.dir:
				t.op("readdir", f.path, 0)
			case f.written > 0 || f.write:
				t.op("write", f.path, f.written)
			case f.read > 0:
				t.op("read", f.path, f.read)
			default:
				t.op("open", f.path, 0)
			}
			t.mu.Lock()
		}
	}
}

func (t *SFTPTap) response(p []byte) {
	if len(p) < 5 {
		return
	}
	typ := p[0]
	id := binary.BigEndian.Uint32(p[1:5])
	b := p[5:]
	t.mu.Lock()
	req, ok := t.pending[id]
	delete(t.pending, id)
	var ev *audit.Event
	switch typ {
	case fxpHandle:
		if ok {
			h, _ := sshString(b)
			t.handles[h] = &sftpFile{path: req.path, dir: req.typ == fxpOpendir, write: req.to == "w"}
		}
	case fxpData:
		if h, ok := t.reads[id]; ok {
			delete(t.reads, id)
			if f := t.handles[h]; f != nil && len(b) >= 4 {
				f.read += int64(binary.BigEndian.Uint32(b[:4]))
			}
		}
	case fxpStatus:
		delete(t.reads, id)
		if !ok || len(b) < 4 {
			break
		}
		code := binary.BigEndian.Uint32(b[:4])
		names := map[byte]string{fxpOpen: "open", fxpOpendir: "readdir", fxpRemove: "unlink", fxpMkdir: "mkdir", fxpRmdir: "rmdir", fxpRename: "rename"}
		path := req.path
		if req.typ == fxpRename {
			path = req.path + " -> " + req.to
		}
		op := names[req.typ]
		if req.typ == fxpOpen && req.to == "w" {
			op = "write"
		}
		switch code {
		case fxPermDeny:
			ev = &audit.Event{Kind: audit.Blocked, TS: time.Now(), Op: op, Path: path, Reason: "outside the grant or read-only"}
		case fxStatusOK:
			if req.typ != fxpOpen && req.typ != fxpOpendir {
				ev = &audit.Event{Kind: audit.Op, TS: time.Now(), Op: op, Path: path}
			}
		}
	}
	t.mu.Unlock()
	if ev != nil {
		t.Emit(*ev)
	}
}

// sshString reads one SSH string (uint32 length + bytes).
func sshString(b []byte) (string, []byte) {
	if len(b) < 4 {
		return "", nil
	}
	n := int(binary.BigEndian.Uint32(b))
	if n > len(b)-4 {
		return "", nil
	}
	return string(b[4 : 4+n]), b[4+n:]
}

// framer splits a byte stream into SFTP packets (uint32 length + body).
// Packets are at most a few hundred KB; anything larger means we lost the
// framing, so the tap goes quiet (the stream itself is never touched).
type framer struct {
	buf  []byte
	dead bool
	on   func([]byte)
}

func (f *framer) feed(p []byte) {
	if f.dead {
		return
	}
	f.buf = append(f.buf, p...)
	for len(f.buf) >= 4 {
		n := int(binary.BigEndian.Uint32(f.buf))
		if n > 4<<20 {
			f.dead, f.buf = true, nil
			return
		}
		if len(f.buf) < 4+n {
			return
		}
		f.on(f.buf[4 : 4+n])
		f.buf = f.buf[4+n:]
	}
}

type tapReader struct {
	r  io.Reader
	f  *framer
	on func([]byte)
}

func (t *tapReader) Read(p []byte) (int, error) {
	if t.f == nil {
		t.f = &framer{on: t.on}
	}
	n, err := t.r.Read(p)
	if n > 0 {
		t.f.feed(p[:n])
	}
	return n, err
}

type tapWriter struct {
	w  io.Writer
	f  *framer
	on func([]byte)
}

func (t *tapWriter) Write(p []byte) (int, error) {
	if t.f == nil {
		t.f = &framer{on: t.on}
	}
	t.f.feed(p)
	return t.w.Write(p)
}

// IsSFTP reports whether a session command starts the sftp-server (meshy mount).
func IsSFTP(raw string) bool { return strings.Contains(raw, "sftp-server") }
