// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package poll

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hanzoai/git-runner/internal/pkg/client"
	"github.com/hanzoai/git-runner/internal/pkg/config"
	"github.com/hanzoai/git-runner/internal/pkg/metrics"

	forge "github.com/hanzoai/git/modules/actions/runner"
	log "github.com/sirupsen/logrus"
)

// TaskRunner abstracts task execution so the poller can be tested
// without a real runner.
type TaskRunner interface {
	Run(ctx context.Context, task *forge.Task) error
}

type Poller struct {
	client client.Client
	runner TaskRunner
	cfg    *config.Config
	queue  atomic.Int64 // the queue version this runner last saw; see fetchTask.

	pollingCtx      context.Context
	shutdownPolling context.CancelFunc

	jobsCtx      context.Context
	shutdownJobs context.CancelFunc

	done chan struct{}
}

// workerState holds the single poller's backoff state. Consecutive empty or
// error responses drive exponential backoff; a successful task fetch resets
// both counters so the next poll fires immediately.
type workerState struct {
	consecutiveEmpty  int64
	consecutiveErrors int64
	// lastBackoff is the last interval reported to the PollBackoffSeconds gauge;
	// used to suppress redundant no-op Set calls when the backoff plateaus
	// (e.g. at FetchIntervalMax).
	lastBackoff time.Duration
}

func New(cfg *config.Config, client client.Client, runner TaskRunner) *Poller {
	pollingCtx, shutdownPolling := context.WithCancel(context.Background())

	jobsCtx, shutdownJobs := context.WithCancel(context.Background())

	done := make(chan struct{})

	return &Poller{
		client: client,
		runner: runner,
		cfg:    cfg,

		pollingCtx:      pollingCtx,
		shutdownPolling: shutdownPolling,

		jobsCtx:      jobsCtx,
		shutdownJobs: shutdownJobs,

		done: done,
	}
}

func (p *Poller) Poll() {
	sem := make(chan struct{}, p.cfg.Runner.Capacity)
	wg := &sync.WaitGroup{}
	s := &workerState{}

	defer func() {
		wg.Wait()
		close(p.done)
	}()

	for {
		select {
		case sem <- struct{}{}:
		case <-p.pollingCtx.Done():
			return
		}

		task, ok := p.fetchTask(p.pollingCtx, s)
		if !ok {
			<-sem
			if !p.waitBackoff(s) {
				return
			}
			continue
		}

		s.resetBackoff()

		wg.Add(1)
		go func(t *forge.Task) {
			defer wg.Done()
			defer func() { <-sem }()
			p.runTaskWithRecover(p.jobsCtx, t)
		}(task)
	}
}

func (p *Poller) PollOnce() {
	defer close(p.done)
	s := &workerState{}
	for {
		task, ok := p.fetchTask(p.pollingCtx, s)
		if !ok {
			if !p.waitBackoff(s) {
				return
			}
			continue
		}
		s.resetBackoff()
		p.runTaskWithRecover(p.jobsCtx, task)
		return
	}
}

func (p *Poller) Shutdown(ctx context.Context) error {
	p.shutdownPolling()

	select {
	// graceful shutdown completed succesfully
	case <-p.done:
		return nil

	// our timeout for shutting down ran out
	case <-ctx.Done():
		// Both the timeout and the graceful shutdown may fire
		// simultaneously. Do a non-blocking check to avoid forcing
		// a shutdown when graceful already completed.
		select {
		case <-p.done:
			return nil
		default:
		}

		// force a shutdown of all running jobs
		p.shutdownJobs()

		// wait for running jobs to report their status to the server
		<-p.done

		return ctx.Err()
	}
}

func (s *workerState) resetBackoff() {
	s.consecutiveEmpty = 0
	s.consecutiveErrors = 0
	s.lastBackoff = 0
}

// waitBackoff sleeps for the current backoff interval (with jitter).
// Returns false if the polling context was cancelled during the wait.
func (p *Poller) waitBackoff(s *workerState) bool {
	base := p.calculateInterval(s)
	if base != s.lastBackoff {
		metrics.PollBackoffSeconds.Set(base.Seconds())
		s.lastBackoff = base
	}
	timer := time.NewTimer(addJitter(base))
	select {
	case <-timer.C:
		return true
	case <-p.pollingCtx.Done():
		timer.Stop()
		return false
	}
}

// calculateInterval returns the polling interval with exponential backoff based on
// consecutive empty or error responses. The interval starts at FetchInterval and
// doubles with each consecutive empty/error, capped at FetchIntervalMax.
func (p *Poller) calculateInterval(s *workerState) time.Duration {
	base := p.cfg.Runner.FetchInterval
	maxInterval := p.cfg.Runner.FetchIntervalMax

	n := max(s.consecutiveEmpty, s.consecutiveErrors)
	if n <= 1 {
		return base
	}

	// Capped exponential backoff: base * 2^(n-1), max shift=5 so multiplier <= 32
	shift := min(n-1, 5)
	interval := base * time.Duration(int64(1)<<shift)
	return min(interval, maxInterval)
}

// addJitter adds +/- 20% random jitter to the given duration to avoid thundering herd.
func addJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	// jitter range: [-20%, +20%] of d
	jitterRange := int64(d) * 2 / 5 // 40% total range
	if jitterRange <= 0 {
		return d
	}
	jitter := rand.Int64N(jitterRange) - jitterRange/2
	return d + time.Duration(jitter)
}

func (p *Poller) runTaskWithRecover(ctx context.Context, task *forge.Task) {
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("panic: %v", r)
			log.WithError(err).Error("panic in runTaskWithRecover")
		}
	}()

	if err := p.runner.Run(ctx, task); err != nil {
		log.WithError(err).Error("failed to run task")
	}
}

// fetchTask asks the forge for work once. The forge answers immediately,
// always: queue is the version the runner last saw, and a forge whose version
// still matches has queued nothing since and answers without opening the
// assignment transaction. Cadence is this poller's business — see
// calculateInterval — and the wait for an answer is the wire's, so a forge that
// takes too long produces an error here rather than a silence that reads like
// an idle forge.
func (p *Poller) fetchTask(ctx context.Context, s *workerState) (*forge.Task, bool) {
	// Load the version value that was in the cache when the request was sent.
	v := p.queue.Load()
	start := time.Now()
	out, err := p.client.Task(ctx, &forge.TaskIn{Queue: v})
	metrics.PollFetchDuration.Observe(time.Since(start).Seconds())

	if err != nil {
		log.WithError(err).Error("failed to fetch task")
		s.consecutiveErrors++
		metrics.PollFetchTotal.WithLabelValues(metrics.LabelResultError).Inc()
		metrics.ClientErrors.WithLabelValues(metrics.LabelOpTask).Inc()
		return nil, false
	}

	// Successful response — reset error counter.
	s.consecutiveErrors = 0

	if out == nil {
		s.consecutiveEmpty++
		metrics.PollFetchTotal.WithLabelValues(metrics.LabelResultEmpty).Inc()
		return nil, false
	}

	if out.Queue > v {
		p.queue.CompareAndSwap(v, out.Queue)
	}

	if out.Task == nil {
		s.consecutiveEmpty++
		metrics.PollFetchTotal.WithLabelValues(metrics.LabelResultEmpty).Inc()
		return nil, false
	}

	// got a task, so forget the queue version: the next request must look.
	p.queue.CompareAndSwap(out.Queue, 0)

	metrics.PollFetchTotal.WithLabelValues(metrics.LabelResultTask).Inc()
	return out.Task, true
}
