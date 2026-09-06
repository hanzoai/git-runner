// Copyright 2026 Hanzo AI, Inc. All rights reserved.
// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package client

import (
	"context"

	forge "github.com/hanzoai/git/modules/actions/runner"
)

// A Client is the runner's half of the protocol the forge serves at /v1/runner:
// one method per operation, taking and returning the very types the forge
// declares.
type Client interface {
	// Register trades a registration token for a runner identity.
	Register(ctx context.Context, in *forge.RegisterIn) (*forge.RegisterOut, error)
	// Declare republishes what this runner can do, and reports what the forge
	// understands.
	Declare(ctx context.Context, in *forge.DeclareIn) (*forge.DeclareOut, error)
	// Task asks for work.
	Task(ctx context.Context, in *forge.TaskIn) (*forge.TaskOut, error)
	// State reports a task's progress.
	State(ctx context.Context, in *forge.StateIn) (*forge.StateOut, error)
	// Logs appends console output to a task's log.
	Logs(ctx context.Context, in *forge.LogsIn) (*forge.LogsOut, error)

	Address() string
	Insecure() bool
}
