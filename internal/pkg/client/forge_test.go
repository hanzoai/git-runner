// Copyright 2026 Hanzo AI, Inc. All rights reserved.
// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"net"
	"reflect"
	"testing"
	"time"
	"unsafe"

	forge "github.com/hanzoai/git/modules/actions/runner"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zap-proto/zip"
)

// TestOpNames pins the five names this client sends against zip's own
// derivation from the route the forge registers. The address and the name are
// one registration on the forge's side; this is what keeps them one on ours.
func TestOpNames(t *testing.T) {
	for name, op := range map[string]string{
		"register": "post_runner_register",
		"declare":  "post_runner_declare",
		"task":     "post_runner_task",
		"state":    "post_runner_state",
		"log":      "post_runner_log",
	} {
		assert.Equal(t, op, zip.ID("POST", "/v1/runner/"+name))
	}
}

// forgeDouble serves the five ops the way the forge does — typed, on the zip
// app's op plane — so the exchange under test is a real encode, a real socket
// and a real decode rather than a stand-in for one.
func forgeDouble(t *testing.T) string {
	t.Helper()

	app := zip.New(zip.Config{})
	zip.Post(app, "/v1/runner/register", func(_ context.Context, in *forge.RegisterIn) (*forge.RegisterOut, error) {
		if in.Token != "registration-secret" {
			return nil, zip.Errorf(401, "unregistered runner")
		}
		return &forge.RegisterOut{Runner: forge.Identity{
			ID: 3, UUID: "3c921fce", Token: "runner-secret", Name: in.Name, Labels: in.Labels,
		}}, nil
	})
	zip.Post(app, "/v1/runner/declare", func(_ context.Context, in *forge.DeclareIn) (*forge.DeclareOut, error) {
		if err := seen(in.Credential); err != nil {
			return nil, err
		}
		return &forge.DeclareOut{Runner: forge.Identity{UUID: in.UUID, Version: in.Version, Labels: in.Labels}}, nil
	})
	zip.Post(app, "/v1/runner/task", func(_ context.Context, in *forge.TaskIn) (*forge.TaskOut, error) {
		if err := seen(in.Credential); err != nil {
			return nil, err
		}
		if in.Queue == 41 { // the queue has not moved: no work, and say where it stands
			return &forge.TaskOut{Queue: 41}, nil
		}
		return &forge.TaskOut{Queue: in.Queue, Task: &forge.Task{
			ID:       17,
			Workflow: []byte("on: push"),
			Context:  forge.Context{Repository: "hanzoai/git", Event: []byte(`{"ref":"refs/heads/main"}`)},
			Secrets:  []forge.Pair{{Name: "GIT_TOKEN", Value: "t"}},
			Needs:    []forge.Need{{Job: "build", Result: forge.Success, Outputs: []forge.Pair{{Name: "o", Value: "v"}}}},
		}}, nil
	})
	zip.Post(app, "/v1/runner/state", func(_ context.Context, in *forge.StateIn) (*forge.StateOut, error) {
		if err := seen(in.Credential); err != nil {
			return nil, err
		}
		stored := make([]string, 0, len(in.Outputs))
		for _, o := range in.Outputs {
			stored = append(stored, o.Name)
		}
		return &forge.StateOut{State: in.State, Stored: stored}, nil
	})
	zip.Post(app, "/v1/runner/log", func(_ context.Context, in *forge.LogIn) (*forge.LogOut, error) {
		if err := seen(in.Credential); err != nil {
			return nil, err
		}
		// Echoed back, so the caller can prove a timestamp survived the wire.
		return &forge.LogOut{Ack: in.Lines[0].Time}, nil
	})

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())

	stopped := make(chan error, 1)
	go func() { stopped <- app.Listen("http://" + addr) }()
	t.Cleanup(func() { _ = app.Shutdown() })

	// Wait for the listener rather than sleeping a guess, and say why if it
	// never arrives instead of only that it did not.
	require.Eventually(t, func() bool {
		select {
		case err := <-stopped:
			require.NoError(t, err, "the forge double stopped before it answered")
		default:
		}
		c, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err != nil {
			return false
		}
		return c.Close() == nil
	}, 5*time.Second, 20*time.Millisecond)

	return "http://" + addr
}

func seen(c forge.Credential) error {
	if c.UUID != "3c921fce" || c.Token != "runner-secret" {
		return zip.Errorf(401, "unregistered runner")
	}
	return nil
}

func TestForge(t *testing.T) {
	address := forgeDouble(t)
	ctx := context.Background()

	// Registering has no credential to present: minting one is what it is for.
	blank, err := New(address, forge.Credential{}, false, 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, address, blank.Address())

	out, err := blank.Register(ctx, &forge.RegisterIn{
		Name: "evo", Token: "registration-secret", Labels: []string{"self-hosted"},
	})
	require.NoError(t, err)
	assert.Equal(t, "3c921fce", out.Runner.UUID)
	assert.Equal(t, "runner-secret", out.Runner.Token)

	cred := forge.Credential{UUID: out.Runner.UUID, Token: out.Runner.Token}
	f, err := New(address, cred, false, 5*time.Second)
	require.NoError(t, err)

	t.Run("the credential is presented without a caller filling it in", func(t *testing.T) {
		got, err := f.Declare(ctx, &forge.DeclareIn{Version: "1.2.3", Labels: []string{"rocm"}})
		require.NoError(t, err)
		assert.Equal(t, "3c921fce", got.Runner.UUID)
		assert.Equal(t, "1.2.3", got.Runner.Version)
	})

	t.Run("a task crosses whole", func(t *testing.T) {
		got, err := f.Task(ctx, &forge.TaskIn{Queue: 42})
		require.NoError(t, err)
		require.NotNil(t, got.Task)
		assert.Equal(t, int64(17), got.Task.ID)
		assert.Equal(t, "hanzoai/git", got.Task.Context.Repository)
		assert.JSONEq(t, `{"ref":"refs/heads/main"}`, string(got.Task.Context.Event))
		assert.Equal(t, []forge.Pair{{Name: "GIT_TOKEN", Value: "t"}}, got.Task.Secrets)
		require.Len(t, got.Task.Needs, 1)
		assert.Equal(t, "build", got.Task.Needs[0].Job)
		assert.Equal(t, forge.Success, got.Task.Needs[0].Result)
		assert.Equal(t, []forge.Pair{{Name: "o", Value: "v"}}, got.Task.Needs[0].Outputs)
	})

	// A forge with nothing queued answers, and a nil Task is how it says so.
	// The wire rebuilds an all-zero struct pointer as nil, so a task must never
	// be recognised by anything but this.
	t.Run("no work is a nil task and not an error", func(t *testing.T) {
		got, err := f.Task(ctx, &forge.TaskIn{Queue: 41})
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Nil(t, got.Task)
		assert.Equal(t, int64(41), got.Queue)
	})

	t.Run("state carries outputs and reports what was stored", func(t *testing.T) {
		started := time.Now().UnixNano()
		got, err := f.State(ctx, &forge.StateIn{
			State:   forge.State{ID: 17, Result: forge.Success, Started: started},
			Outputs: []forge.Pair{{Name: "digest", Value: "sha256:1"}},
		})
		require.NoError(t, err)
		assert.Equal(t, forge.Success, got.State.Result)
		assert.Equal(t, started, got.State.Started, "a timestamp must survive the wire exactly")
		assert.Equal(t, []string{"digest"}, got.Stored)
	})

	// Nanoseconds, because the stored log format keeps 100ns ticks: a
	// millisecond timestamp would round every line the log viewer renders.
	t.Run("a log line keeps its nanosecond", func(t *testing.T) {
		at := time.Date(2026, 9, 6, 12, 0, 0, 123456789, time.UTC).UnixNano()
		got, err := f.Log(ctx, &forge.LogIn{Task: 17, Lines: []forge.Line{{Time: at, Content: "hello"}}, Last: true})
		require.NoError(t, err)
		assert.Equal(t, at, got.Ack)
	})

	t.Run("a refusal arrives as the status the forge chose", func(t *testing.T) {
		wrong, err := New(address, forge.Credential{UUID: "3c921fce", Token: "stale"}, false, 5*time.Second)
		require.NoError(t, err)
		_, err = wrong.Task(ctx, &forge.TaskIn{})
		require.Error(t, err)
		var fault *zip.HTTPError
		require.True(t, errors.As(err, &fault))
		assert.Equal(t, 401, fault.Status)
		assert.Equal(t, "unregistered runner", fault.Msg)
	})
}

func TestNewRefusesAnAddressItCannotDial(t *testing.T) {
	for _, address := range []string{"", "git.hanzo.ai", "://nope"} {
		_, err := New(address, forge.Credential{}, false, time.Second)
		assert.Error(t, err, "address %q", address)
	}
}

// TestOwn pins the copy that take() makes, without depending on when a pool
// happens to hand the same buffer out twice.
//
// It builds the value zap builds — strings that are views onto the message —
// and then writes over the message, which is what the next call does with a
// pooled response. A reply the caller owns is unchanged; one that still points
// at the wire is not.
func TestOwn(t *testing.T) {
	msg := []byte("runner-secretrefs/heads/main")
	view := func(at, n int) string { return unsafe.String(&msg[at], n) }

	out := &forge.TaskOut{Task: &forge.Task{
		Context: forge.Context{Ref: view(13, 15), Event: msg[13:]},
		Secrets: []forge.Pair{{Name: "GIT_TOKEN", Value: view(0, 13)}},
		Needs:   []forge.Need{{Job: view(0, 6), Result: forge.Success}},
	}}
	own(reflect.ValueOf(out).Elem())

	copy(msg, "1.2.31.2.31.2.31.2.31.2.31.2.3")

	assert.Equal(t, "refs/heads/main", out.Task.Context.Ref)
	assert.Equal(t, "refs/heads/main", string(out.Task.Context.Event))
	assert.Equal(t, "runner-secret", out.Task.Secrets[0].Value)
	assert.Equal(t, "runner", out.Task.Needs[0].Job)
}
