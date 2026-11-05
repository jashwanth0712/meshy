//go:build darwin

package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/jashwanth0712/meshy/meshy/meshyd"
	"github.com/jashwanth0712/meshy/meshy/sandbox"
)

func platformSandbox(ctx context.Context, roots []string, state string) (meshyd.Sandbox, error) {
	sb := &sandbox.Seatbelt{}
	if os.Geteuid() == 0 {
		mon := &sandbox.ESMonitor{}
		if err := mon.Start(ctx); err != nil {
			slog.Warn("eslogger unavailable: allowed reads will not be audited (grant Full Disk Access to meshyd)", "err", err)
		} else {
			sb.Monitor = mon
		}
	} else {
		slog.Warn("not running as root: allowed reads will not be audited on macOS (blocks still are)")
	}
	return sb, nil
}
