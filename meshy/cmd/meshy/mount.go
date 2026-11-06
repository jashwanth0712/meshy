package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/net/webdav"

	"github.com/jashwanth0712/meshy/meshy/davfs"
	"github.com/jashwanth0712/meshy/meshy/enskit"
)

// cmdMount shows a device's granted folders in Finder as a network volume:
//
//	meshy mount jash.sample1.acme.eth
//
// It opens one sandboxed session on the device (the same one `meshy ssh`
// gets), speaks SFTP through it, and serves the folders over WebDAV on
// 127.0.0.1 for Finder to mount. The volume is ejected when the grant is
// revoked or expires, or when this command stops (Ctrl-C).
func cmdMount(args []string) error {
	fs := flag.NewFlagSet("mount", flag.ExitOnError)
	endpoint := fs.String("endpoint", "", "override the device's host:port")
	listen := fs.String("listen", "127.0.0.1:0", "local WebDAV address")
	noMount := fs.Bool("no-mount", false, "serve the share and print its URL without mounting it in Finder")
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		args = append(append([]string{}, args[1:]...), args[0])
	}
	fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("usage: meshy mount <device ens name> [--no-mount]")
	}
	device := enskit.Name(strings.ToLower(fs.Arg(0)))
	id, err := loadIdentityFor(device)
	if err != nil {
		return err
	}
	roots, err := deviceRoots(device)
	if err != nil {
		return err
	}
	conn, err := dialResource(id, device, *endpoint)
	if err != nil {
		return err
	}
	defer conn.Close()
	sess, err := conn.NewSession()
	if err != nil {
		return err
	}
	stdin, _ := sess.StdinPipe()
	stdout, _ := sess.StdoutPipe()
	// The device explains refusals and revocations on stderr (MESHY_* lines).
	var last lastLine
	sess.Stderr = io.MultiWriter(os.Stderr, &last)
	if err := sess.Start(davfs.SFTPCommand); err != nil {
		return err
	}
	ended := make(chan error, 1)
	go func() { ended <- sess.Wait() }()

	client, err := sftp.NewClientPipe(stdout, stdin)
	if err != nil {
		select {
		case <-ended:
		case <-time.After(2 * time.Second):
		}
		if l := last.get(); l != "" {
			return errors.New(l)
		}
		return fmt.Errorf("MESHY_MOUNT_FAILED %s: %v", device, err)
	}
	defer client.Close()
	share, err := davfs.New(client, roots)
	if err != nil {
		return err
	}

	label := device.Label()
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	h := &webdav.Handler{Prefix: "/" + label, FileSystem: share, LockSystem: webdav.NewMemLS()}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	url := fmt.Sprintf("http://%s/%s/", ln.Addr(), label)
	for name, p := range share.Roots() {
		fmt.Printf("  %s  ←  %s\n", name, p)
	}

	volume := ""
	if !*noMount {
		if volume, err = finderMount(url); err != nil {
			srv.Close()
			return err
		}
		exec.Command("open", volume).Start()
	}
	fmt.Printf("MESHY_MOUNTED device=%s volume=%s url=%s\n", device, volume, url)
	fmt.Println("Ctrl-C to eject. The volume goes away when the grant ends.")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	var why error
	select {
	case <-sig:
	case <-ended:
		why = errors.New("MESHY_UNMOUNTED " + firstNonEmpty(last.get(), "the device closed the session"))
	}
	if volume != "" {
		finderEject(volume)
	}
	srv.Close()
	if why == nil {
		fmt.Printf("MESHY_UNMOUNTED device=%s\n", device)
	}
	return why
}

// deviceRoots reads the folders a device shares (its meshy.roots record).
func deviceRoots(device enskit.Name) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ens, err := ensClient(ctx)
	if err != nil {
		return nil, err
	}
	r, err := ens.Lookup(ctx, device)
	if err != nil {
		return nil, fmt.Errorf("MESHY_UNKNOWN_RESOURCE %s: %v", device, err)
	}
	raw, _ := r.Text(ctx, ens, "meshy.roots")
	var roots []string
	if json.Unmarshal([]byte(raw), &roots) != nil || len(roots) == 0 {
		return nil, fmt.Errorf("MESHY_NO_FOLDERS %s doesn't share any folders", device)
	}
	return roots, nil
}

// finderMount mounts a WebDAV URL like Go › Connect to Server and returns
// where it landed (/Volumes/<name>).
func finderMount(url string) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("mounting in a file manager needs macOS; the share is at %s (use --no-mount)", url)
	}
	out, err := exec.Command("osascript", "-e", fmt.Sprintf(`mount volume %q`, url)).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("MESHY_MOUNT_FAILED Finder couldn't mount %s: %s", url, strings.TrimSpace(string(out)))
	}
	// `mount volume` returns "file <disk>:Volumes:<name>:"; read the real path from mount(8).
	host := strings.SplitN(strings.TrimPrefix(url, "http://"), "/", 2)[0]
	for i := 0; i < 20; i++ {
		if m, err := exec.Command("mount").Output(); err == nil {
			for _, line := range strings.Split(string(m), "\n") {
				if strings.Contains(line, host) {
					if a, b, ok := strings.Cut(line, " on "); ok && a != "" {
						return strings.SplitN(b, " (", 2)[0], nil
					}
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	if s := strings.TrimSpace(string(out)); strings.Contains(s, "Volumes:") {
		parts := strings.Split(strings.TrimSuffix(s, ":"), ":")
		return filepath.Join("/Volumes", parts[len(parts)-1]), nil
	}
	return "", errors.New("MESHY_MOUNT_FAILED mounted, but couldn't find the volume")
}

func finderEject(volume string) {
	if exec.Command("diskutil", "unmount", volume).Run() != nil {
		exec.Command("diskutil", "unmount", "force", volume).Run()
	}
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// lastLine keeps the last MESHY_* line written to it.
type lastLine struct {
	mu   sync.Mutex
	line string
	buf  string
}

func (l *lastLine) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf += string(p)
	sc := bufio.NewScanner(strings.NewReader(l.buf))
	for sc.Scan() {
		if t := strings.TrimSpace(sc.Text()); strings.HasPrefix(t, "MESHY_") {
			l.line = t
		}
	}
	if i := strings.LastIndexByte(l.buf, '\n'); i >= 0 {
		l.buf = l.buf[i+1:]
	}
	return len(p), nil
}

func (l *lastLine) get() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.line
}
