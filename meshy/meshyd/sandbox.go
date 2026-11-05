package meshyd

import (
	"context"
	"errors"
	"io"
	"os/exec"

	"github.com/jashwanth0712/meshy/meshy/audit"
	"github.com/jashwanth0712/meshy/meshy/policy"
)

// SandboxSpec describes one sandboxed command.
type SandboxSpec struct {
	SessionID string
	Agent     string
	Grant     string
	Policy    *policy.Policy
	Argv      []string // already non-empty; an interactive login gets ["/bin/sh","-l"]
	Env       []string
	Audit     audit.Sink // receives op/blocked events with Session/Agent/Grant filled
}

// Prepared is a ready-to-start command and its teardown.
type Prepared struct {
	Cmd *exec.Cmd
	// Cleanup releases sandbox resources (FUSE mounts, temp dirs). It is called
	// after the process group is dead. Safe to call more than once.
	Cleanup func()
	// WrapStderr optionally wraps the session's stderr (e.g. to detect
	// blocked accesses from tool error messages).
	WrapStderr func(io.Writer) io.Writer
	// OnStart is called with the pid right after the command starts.
	OnStart func(pid int)
}

// Sandbox builds confined commands. The command MUST be started in its own
// process group (Setpgid) so the session can be killed as a unit.
type Sandbox interface {
	Name() string
	Prepare(ctx context.Context, spec SandboxSpec) (*Prepared, error)
	// Check reports whether the host supports this sandbox (for `meshyd doctor`).
	Check() error
}

// ErrNoSandbox is returned when no sandbox is available and unsafe mode is off.
var ErrNoSandbox = errors.New("meshyd: no sandbox available on this host")
