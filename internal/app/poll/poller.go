// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package poll

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"gitea.com/gitea/act_runner/internal/app/run"
	"gitea.com/gitea/act_runner/internal/pkg/client"
	"gitea.com/gitea/act_runner/internal/pkg/config"
	"gitea.com/gitea/act_runner/internal/pkg/metrics"

	runnerv1 "code.gitea.io/actions-proto-go/runner/v1"
	"connectrpc.com/connect"
	log "github.com/sirupsen/logrus"
)

type Poller struct {
	client       client.Client
	runner       *run.Runner
	cfg          *config.Config
	tasksVersion atomic.Int64 // tasksVersion used to store the version of the last task fetched from the Gitea.

	pollingCtx      context.Context
	shutdownPolling context.CancelFunc

	jobsCtx      context.Context
	shutdownJobs context.CancelFunc

	done chan struct{}
}

// workerState holds per-goroutine polling state. Backoff counters are
// per-worker so that with Capacity > 1, N workers each seeing one empty
// response don't combine into a "consecutive N empty" reading on a shared
// counter and trigger an unnecessarily long backoff.
type workerState struct {
	consecutiveEmpty  int64
	consecutiveErrors int64
	// lastBackoff is the last interval reported to the PollBackoffSeconds gauge
	// from this worker; used to suppress redundant no-op Set calls when the
	// backoff plateaus (e.g. at FetchIntervalMax).
	lastBackoff time.Duration
}

func New(cfg *config.Config, client client.Client, runner *run.Runner) *Poller {
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
	wg := &sync.WaitGroup{}
	for i := 0; i < p.cfg.Runner.Capacity; i++ {
		wg.Add(1)
		go p.poll(wg)
	}
	wg.Wait()

	// signal that we shutdown
	close(p.done)
}

func (p *Poller) PollOnce() {
	p.pollOnce(&workerState{})

	// signal that we're done
	close(p.done)
}

func (p *Poller) Shutdown(ctx context.Context) error {
	p.shutdownPolling()

	select {
	// graceful shutdown completed succesfully
	case <-p.done:
		return nil

	// our timeout for shutting down ran out
	case <-ctx.Done():
		// when both the timeout fires and the graceful shutdown
		// completed succsfully, this branch of the select may
		// fire. Do a non-blocking check here against the graceful
		// shutdown status to avoid sending an error if we don't need to.
		_, ok := <-p.done
		if !ok {
			return nil
		}

		// force a shutdown of all running jobs
		p.shutdownJobs()

		// wait for running jobs to report their status to Gitea
		<-p.done

		return ctx.Err()
	}
}

func (p *Poller) poll(wg *sync.WaitGroup) {
	defer wg.Done()
	s := &workerState{}
	for {
		p.pollOnce(s)

		select {
		case <-p.pollingCtx.Done():
			return
		default:
			continue
		}
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

func (p *Poller) pollOnce(s *workerState) {
	for {
		task, ok := p.fetchTask(p.pollingCtx, s)
		if !ok {
			base := p.calculateInterval(s)
			if base != s.lastBackoff {
				metrics.PollBackoffSeconds.Set(base.Seconds())
				s.lastBackoff = base
			}
			timer := time.NewTimer(addJitter(base))
			select {
			case <-timer.C:
			case <-p.pollingCtx.Done():
				timer.Stop()
				return
			}
			continue
		}

		// Got a task — reset backoff counters for fast subsequent polling.
		s.consecutiveEmpty = 0
		s.consecutiveErrors = 0

		p.runTaskWithRecover(p.jobsCtx, task)
		return
	}
}

func (p *Poller) runTaskWithRecover(ctx context.Context, task *runnerv1.Task) {
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

func (p *Poller) fetchTask(ctx context.Context, s *workerState) (*runnerv1.Task, bool) {
	reqCtx, cancel := context.WithTimeout(ctx, p.cfg.Runner.FetchTimeout)
	defer cancel()

	// Load the version value that was in the cache when the request was sent.
	v := p.tasksVersion.Load()
	start := time.Now()
	resp, err := p.client.FetchTask(reqCtx, connect.NewRequest(&runnerv1.FetchTaskRequest{
		TasksVersion: v,
	}))

	// DeadlineExceeded is the designed idle path for a long-poll: the server
	// found no work within FetchTimeout. Treat it as an empty response and do
	// not record the duration — the timeout value would swamp the histogram.
	if errors.Is(err, context.DeadlineExceeded) {
		s.consecutiveEmpty++
		s.consecutiveErrors = 0 // timeout is a healthy idle response
		metrics.PollFetchTotal.WithLabelValues(metrics.LabelResultEmpty).Inc()
		return nil, false
	}
	metrics.PollFetchDuration.Observe(time.Since(start).Seconds())

	if err != nil {
		log.WithError(err).Error("failed to fetch task")
		s.consecutiveErrors++
		metrics.PollFetchTotal.WithLabelValues(metrics.LabelResultError).Inc()
		metrics.ClientErrors.WithLabelValues(metrics.LabelMethodFetchTask).Inc()
		return nil, false
	}

	// Successful response — reset error counter.
	s.consecutiveErrors = 0

	if resp == nil || resp.Msg == nil {
		s.consecutiveEmpty++
		metrics.PollFetchTotal.WithLabelValues(metrics.LabelResultEmpty).Inc()
		return nil, false
	}

	if resp.Msg.TasksVersion > v {
		p.tasksVersion.CompareAndSwap(v, resp.Msg.TasksVersion)
	}

	if resp.Msg.Task == nil {
		s.consecutiveEmpty++
		metrics.PollFetchTotal.WithLabelValues(metrics.LabelResultEmpty).Inc()
		return nil, false
	}

	// got a task, set `tasksVersion` to zero to force query db in next request.
	p.tasksVersion.CompareAndSwap(resp.Msg.TasksVersion, 0)

	metrics.PollFetchTotal.WithLabelValues(metrics.LabelResultTask).Inc()
	return resp.Msg.Task, true
}
