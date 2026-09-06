// Copyright 2026 Hanzo AI, Inc. All rights reserved.
// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// Package client is the runner's half of the protocol the forge serves at
// /v1/runner: five operations, each one zip.Call by name, each carrying the
// forge's own input type and decoding the forge's own output type. There is one
// definition of the protocol and nothing to generate.
package client

import (
	"context"

	forge "github.com/hanzoai/git/modules/actions/runner"
)

// A Client is the five operations, plus where the forge is — a job's steps are
// told that, and it is not derivable from anything else they hold.
type Client interface {
	// Register trades a registration token for a runner identity. It is the one
	// operation with no credential: a runner has none until this answers.
	Register(ctx context.Context, in *forge.RegisterIn) (*forge.RegisterOut, error)
	// Declare republishes what this runner can do, and reports what the forge
	// understands.
	Declare(ctx context.Context, in *forge.DeclareIn) (*forge.DeclareOut, error)
	// Task asks for work.
	Task(ctx context.Context, in *forge.TaskIn) (*forge.TaskOut, error)
	// State reports a task's progress, and learns whether it was cancelled.
	State(ctx context.Context, in *forge.StateIn) (*forge.StateOut, error)
	// Log appends console output to a task's log.
	Log(ctx context.Context, in *forge.LogIn) (*forge.LogOut, error)

	// Address is where this forge is, as a job is told it.
	Address() string
}
