//go:build linux

package main

import (
	"context"

	"github.com/jashwanth0712/meshy/meshy/meshyd"
	"github.com/jashwanth0712/meshy/meshy/sandbox"
)

func platformSandbox(_ context.Context, roots []string, state string) (meshyd.Sandbox, error) {
	return &sandbox.Bwrap{Roots: roots, RunDir: "/run/meshy"}, nil
}
