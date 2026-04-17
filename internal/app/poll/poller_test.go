// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package poll

import (
	"context"
	"errors"
	"testing"
	"time"

	"gitea.com/gitea/act_runner/internal/pkg/client/mocks"
	"gitea.com/gitea/act_runner/internal/pkg/config"

	runnerv1 "code.gitea.io/actions-proto-go/runner/v1"
	connect_go "connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestPoller_PerWorkerCounters verifies that each worker maintains its own
// backoff counters. With a shared counter, N workers each seeing one empty
// response would inflate the counter to N and trigger an unnecessarily long
// backoff. With per-worker state, each worker only sees its own count.
func TestPoller_PerWorkerCounters(t *testing.T) {
	client := mocks.NewClient(t)
	client.On("FetchTask", mock.Anything, mock.Anything).Return(
		func(_ context.Context, _ *connect_go.Request[runnerv1.FetchTaskRequest]) (*connect_go.Response[runnerv1.FetchTaskResponse], error) {
			// Always return an empty response.
			return connect_go.NewResponse(&runnerv1.FetchTaskResponse{}), nil
		},
	)

	cfg, err := config.LoadDefault("")
	require.NoError(t, err)
	p := &Poller{client: client, cfg: cfg}

	ctx := context.Background()
	s1 := &workerState{}
	s2 := &workerState{}

	// Each worker independently observes one empty response.
	_, ok := p.fetchTask(ctx, s1)
	require.False(t, ok)
	_, ok = p.fetchTask(ctx, s2)
	require.False(t, ok)

	assert.Equal(t, int64(1), s1.consecutiveEmpty, "worker 1 should only count its own empty response")
	assert.Equal(t, int64(1), s2.consecutiveEmpty, "worker 2 should only count its own empty response")

	// Worker 1 sees a second empty; worker 2 stays at 1.
	_, ok = p.fetchTask(ctx, s1)
	require.False(t, ok)
	assert.Equal(t, int64(2), s1.consecutiveEmpty)
	assert.Equal(t, int64(1), s2.consecutiveEmpty, "worker 2's counter must not be affected by worker 1's empty fetches")
}

// TestPoller_FetchErrorIncrementsErrorsOnly verifies that a fetch error
// increments only the per-worker error counter, not the empty counter.
func TestPoller_FetchErrorIncrementsErrorsOnly(t *testing.T) {
	client := mocks.NewClient(t)
	client.On("FetchTask", mock.Anything, mock.Anything).Return(
		func(_ context.Context, _ *connect_go.Request[runnerv1.FetchTaskRequest]) (*connect_go.Response[runnerv1.FetchTaskResponse], error) {
			return nil, errors.New("network unreachable")
		},
	)

	cfg, err := config.LoadDefault("")
	require.NoError(t, err)
	p := &Poller{client: client, cfg: cfg}

	s := &workerState{}
	_, ok := p.fetchTask(context.Background(), s)
	require.False(t, ok)
	assert.Equal(t, int64(1), s.consecutiveErrors)
	assert.Equal(t, int64(0), s.consecutiveEmpty)
}

// TestPoller_CalculateInterval verifies the per-worker exponential backoff
// math is correctly driven by the worker's own counters.
func TestPoller_CalculateInterval(t *testing.T) {
	cfg, err := config.LoadDefault("")
	require.NoError(t, err)
	cfg.Runner.FetchInterval = 2 * time.Second
	cfg.Runner.FetchIntervalMax = 60 * time.Second
	p := &Poller{cfg: cfg}

	cases := []struct {
		name         string
		empty, errs  int64
		wantInterval time.Duration
	}{
		{"first poll, no backoff", 0, 0, 2 * time.Second},
		{"single empty, still base", 1, 0, 2 * time.Second},
		{"two empties, doubled", 2, 0, 4 * time.Second},
		{"five empties, capped path", 5, 0, 32 * time.Second},
		{"many empties, capped at max", 20, 0, 60 * time.Second},
		{"errors drive backoff too", 0, 3, 8 * time.Second},
		{"max(empty, errors) wins", 2, 4, 16 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &workerState{consecutiveEmpty: tc.empty, consecutiveErrors: tc.errs}
			assert.Equal(t, tc.wantInterval, p.calculateInterval(s))
		})
	}
}
