// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/hanzoai/git-runner/internal/app/cmd"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	// run the command
	cmd.Execute(ctx)
}
