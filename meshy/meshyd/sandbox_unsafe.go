package meshyd

import (
	"context"
	"os/exec"
)

// UnsafeSandbox runs commands directly on the host with no confinement. It
// exists only for tests and for exercising the auth/revocation path on a dev
// machine. meshyd refuses to use it unless started with --unsafe-no-sandbox.
type UnsafeSandbox struct{}

func (UnsafeSandbox) Name() string { return "none (UNSAFE: no confinement)" }
func (UnsafeSandbox) Check() error { return nil }

func (UnsafeSandbox) Prepare(ctx context.Context, spec SandboxSpec) (*Prepared, error) {
	cmd := exec.Command(spec.Argv[0], spec.Argv[1:]...)
	cmd.Env = spec.Env
	if len(spec.Policy.Paths) > 0 {
		cmd.Dir = spec.Policy.Paths[0].Path
	}
	return &Prepared{Cmd: cmd, Cleanup: func() {}}, nil
}
