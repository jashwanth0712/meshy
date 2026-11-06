package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/jashwanth0712/meshy/meshy/meshadmit"
)

// cmdMeshJoin admits this agent to the provider's mesh as its own WireGuard
// peer. Afterwards `meshy ssh/connect/curl` reach resources by ENS name over
// the mesh, and only where a live ENS grant has opened a policy.
func cmdMeshJoin(args []string) error {
	fs := flag.NewFlagSet("mesh-join", flag.ExitOnError)
	provider := fs.String("provider", env("MESHY_PROVIDER", ""), "meshy-sync URL")
	fs.Parse(args)
	if *provider == "" {
		return errors.New("usage: meshy mesh-join --provider <meshy-sync url>")
	}
	id, err := loadIdentity()
	if err != nil {
		return err
	}
	state := filepath.Join(id.dir, "mesh")
	pub, err := meshadmit.WGPubKey(state)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	resp, err := meshadmit.Admit(ctx, *provider, string(id.Name()), pub, id.eth)
	if err != nil {
		return err
	}
	fmt.Printf("admitted %s as mesh peer %s\n", id.Name(), resp.PeerName)
	// Register the peer once with the setup key; later runs reuse the state.
	st := exec.Command(meshadmit.MeshBin(), "status", "--state", state, "--ens", string(id.Name()),
		"--management", resp.ManagementURL, "--setup-key", resp.SetupKey)
	st.Stdout, st.Stderr = os.Stdout, os.Stderr
	if err := st.Run(); err != nil {
		return fmt.Errorf("meshy-mesh status: %w", err)
	}
	id.Mesh = resp.ManagementURL
	b, _ := json.MarshalIndent(id, "", "  ")
	return os.WriteFile(filepath.Join(id.dir, "agent.json"), b, 0o600)
}

// meshDial opens a TCP stream to host:port over the mesh by running
// `meshy-mesh proxy` and speaking through its stdin/stdout.
func meshDial(id *Identity, host, port string) (net.Conn, error) {
	cmd := exec.Command(meshadmit.MeshBin(), "proxy", "--state", filepath.Join(id.dir, "mesh"),
		"--ens", string(id.Name()), "--management", id.Mesh, host, port)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start meshy-mesh: %w", err)
	}
	return &procConn{cmd: cmd, w: in, r: out}, nil
}

type procConn struct {
	cmd *exec.Cmd
	w   io.WriteCloser
	r   io.ReadCloser
}

func (c *procConn) Read(p []byte) (int, error)  { return c.r.Read(p) }
func (c *procConn) Write(p []byte) (int, error) { return c.w.Write(p) }
func (c *procConn) Close() error {
	c.w.Close()
	if c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
	c.cmd.Wait()
	return nil
}
func (c *procConn) LocalAddr() net.Addr              { return meshAddr("local") }
func (c *procConn) RemoteAddr() net.Addr             { return meshAddr("mesh") }
func (c *procConn) SetDeadline(time.Time) error      { return nil }
func (c *procConn) SetReadDeadline(time.Time) error  { return nil }
func (c *procConn) SetWriteDeadline(time.Time) error { return nil }

type meshAddr string

func (a meshAddr) Network() string { return "meshy-mesh" }
func (a meshAddr) String() string  { return string(a) }
