// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package report

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hanzoai/git-runner/internal/pkg/client"
	"github.com/hanzoai/git-runner/internal/pkg/config"
	"github.com/hanzoai/git-runner/internal/pkg/metrics"

	"github.com/avast/retry-go/v4"
	forge "github.com/hanzoai/git/modules/actions/runner"
	log "github.com/sirupsen/logrus"
)

type Reporter struct {
	ctx    context.Context
	cancel context.CancelFunc

	closed  bool
	client  client.Client
	clientM sync.Mutex

	logOffset   int
	logLines    []forge.Line
	logReplacer *strings.Replacer
	oldnew      []string

	// lastLogBufferRows is the last value written to the ReportLogBufferRows
	// gauge; guarded by clientM (the same lock held around each ReportLog call)
	// so the gauge skips no-op Set calls when the buffer size is unchanged.
	lastLogBufferRows int

	state        *forge.State
	stateChanged bool
	stateMu      sync.RWMutex
	outputs      sync.Map
	daemon       chan struct{}

	// Unix-nanos of the last successful UpdateTask. Atomic so the heartbeat
	// guard in ReportState reads it without contending stateMu.
	lastReportedAtNanos atomic.Int64

	// Adaptive batching control
	logReportInterval   time.Duration
	logReportMaxLatency time.Duration
	logBatchSize        int
	stateReportInterval time.Duration

	// Event notification channels (non-blocking, buffered 1)
	logNotify   chan struct{} // signal: new log rows arrived
	stateNotify chan struct{} // signal: step transition (start/stop)

	debugOutputEnabled  bool
	stopCommandEndToken string
}

func NewReporter(ctx context.Context, cancel context.CancelFunc, client client.Client, task *forge.Task, cfg *config.Config) *Reporter {
	var oldnew []string
	for _, v := range []string{task.Context.Token, task.Context.RuntimeToken} {
		if v != "" {
			oldnew = append(oldnew, v, "***")
		}
	}
	debug := false
	for _, secret := range task.Secrets {
		oldnew = append(oldnew, secret.Value, "***")
		if secret.Name == "ACTIONS_STEP_DEBUG" && secret.Value == "true" {
			debug = true
		}
	}

	rv := &Reporter{
		ctx:                 ctx,
		cancel:              cancel,
		client:              client,
		oldnew:              oldnew,
		logReplacer:         strings.NewReplacer(oldnew...),
		logReportInterval:   cfg.Runner.LogReportInterval,
		logReportMaxLatency: cfg.Runner.LogReportMaxLatency,
		logBatchSize:        cfg.Runner.LogReportBatchSize,
		stateReportInterval: cfg.Runner.StateReportInterval,
		logNotify:           make(chan struct{}, 1),
		stateNotify:         make(chan struct{}, 1),
		state:               &forge.State{ID: task.ID},
		daemon:              make(chan struct{}),
	}

	rv.debugOutputEnabled = debug

	return rv
}

// Result returns the final job result. Safe to call after Close() returns.
func (r *Reporter) Result() forge.Result {
	r.stateMu.RLock()
	defer r.stateMu.RUnlock()
	return r.state.Result
}

func (r *Reporter) ResetSteps(l int) {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	for i := range l {
		r.state.Steps = append(r.state.Steps, forge.Step{ID: int64(i)})
	}
}

func (r *Reporter) Levels() []log.Level {
	return log.AllLevels
}

// isJobStepEntry is used to not report composite step results incorrectly as step result
// returns true if the logentry is on job level
// returns false for composite action step messages
func isJobStepEntry(entry *log.Entry) bool {
	if v, ok := entry.Data["stepID"]; ok {
		if v, ok := v.([]string); ok && len(v) > 1 {
			return false
		}
	}
	return true
}

// notifyLog sends a non-blocking signal that new log rows are available.
func (r *Reporter) notifyLog() {
	select {
	case r.logNotify <- struct{}{}:
	default:
	}
}

// notifyState sends a non-blocking signal that a UX-critical state change occurred (step start/stop, job result).
func (r *Reporter) notifyState() {
	select {
	case r.stateNotify <- struct{}{}:
	default:
	}
}

// unlockAndNotify releases stateMu and sends channel notifications.
// Must be called with stateMu held.
func (r *Reporter) unlockAndNotify(urgentState bool) {
	r.stateMu.Unlock()
	r.notifyLog()
	if urgentState {
		r.notifyState()
	}
}

func (r *Reporter) Fire(entry *log.Entry) error {
	urgentState := false

	r.stateMu.Lock()

	r.stateChanged = true

	if log.IsLevelEnabled(log.TraceLevel) {
		log.WithFields(entry.Data).Trace(entry.Message)
	}

	timestamp := entry.Time.UnixNano()
	if r.state.Started == 0 {
		r.state.Started = timestamp
	}

	stage := entry.Data["stage"]

	if stage != "Main" {
		if v, ok := entry.Data["jobResult"]; ok {
			if jobResult, ok := r.parseResult(v); ok {
				// We need to ensure log upload before this upload
				r.state.Result = jobResult
				r.state.Stopped = timestamp
				for i := range r.state.Steps {
					if r.state.Steps[i].Result != forge.Pending {
						continue
					}
					r.state.Steps[i].Result = forge.Cancelled
					if jobResult == forge.Skipped {
						r.state.Steps[i].Result = forge.Skipped
					}
				}
				urgentState = true
			}
		}
		if !r.duringSteps() {
			if line, ok := r.parseLine(entry); ok {
				r.logLines = append(r.logLines, line)
			}
		}
		r.unlockAndNotify(urgentState)
		return nil
	}

	var step *forge.Step
	if v, ok := entry.Data["stepNumber"]; ok {
		if v, ok := v.(int); ok && len(r.state.Steps) > v {
			step = &r.state.Steps[v]
		}
	}
	if step == nil {
		if !r.duringSteps() {
			if line, ok := r.parseLine(entry); ok {
				r.logLines = append(r.logLines, line)
			}
		}
		r.unlockAndNotify(false)
		return nil
	}

	if step.Started == 0 {
		step.Started = timestamp
		urgentState = true
	}

	// Force reporting log errors as raw output to prevent silent failures
	if entry.Level == log.ErrorLevel {
		entry.Data["raw_output"] = true
	}

	if v, ok := entry.Data["raw_output"]; ok {
		if rawOutput, ok := v.(bool); ok && rawOutput {
			if line, ok := r.parseLine(entry); ok {
				if step.LogLength == 0 {
					step.LogIndex = int64(r.logOffset + len(r.logLines))
				}
				step.LogLength++
				r.logLines = append(r.logLines, line)
			}
		}
	} else if !r.duringSteps() {
		if line, ok := r.parseLine(entry); ok {
			r.logLines = append(r.logLines, line)
		}
	}
	if v, ok := entry.Data["stepResult"]; ok && isJobStepEntry(entry) {
		if stepResult, ok := r.parseResult(v); ok {
			if step.LogLength == 0 {
				step.LogIndex = int64(r.logOffset + len(r.logLines))
			}
			step.Result = stepResult
			step.Stopped = timestamp
			urgentState = true
		}
	}

	r.unlockAndNotify(urgentState)
	return nil
}

func (r *Reporter) RunDaemon() {
	go r.runDaemonLoop()
}

func (r *Reporter) stopLatencyTimer(active *bool, timer *time.Timer) {
	if *active {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		*active = false
	}
}

func (r *Reporter) runDaemonLoop() {
	logTicker := time.NewTicker(r.logReportInterval)
	stateTicker := time.NewTicker(r.stateReportInterval)

	// maxLatencyTimer ensures the first buffered log row is sent within logReportMaxLatency.
	// Start inactive — it is armed when the first log row arrives in an empty buffer.
	maxLatencyTimer := time.NewTimer(0)
	if !maxLatencyTimer.Stop() {
		<-maxLatencyTimer.C
	}
	maxLatencyActive := false

	defer logTicker.Stop()
	defer stateTicker.Stop()
	defer maxLatencyTimer.Stop()

	for {
		select {
		case <-logTicker.C:
			_ = r.ReportLog(false)
			r.stopLatencyTimer(&maxLatencyActive, maxLatencyTimer)

		case <-stateTicker.C:
			_ = r.ReportState(false)

		case <-r.logNotify:
			r.stateMu.RLock()
			n := len(r.logLines)
			r.stateMu.RUnlock()

			if n >= r.logBatchSize {
				_ = r.ReportLog(false)
				r.stopLatencyTimer(&maxLatencyActive, maxLatencyTimer)
			} else if !maxLatencyActive && n > 0 {
				maxLatencyTimer.Reset(r.logReportMaxLatency)
				maxLatencyActive = true
			}

		case <-r.stateNotify:
			// Step transition or job result — flush both immediately for frontend UX.
			_ = r.ReportLog(false)
			_ = r.ReportState(false)
			r.stopLatencyTimer(&maxLatencyActive, maxLatencyTimer)

		case <-maxLatencyTimer.C:
			maxLatencyActive = false
			_ = r.ReportLog(false)

		case <-r.ctx.Done():
			close(r.daemon)
			return
		}

		r.stateMu.RLock()
		closed := r.closed
		r.stateMu.RUnlock()
		if closed {
			close(r.daemon)
			return
		}
	}
}

func (r *Reporter) Logf(format string, a ...any) {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()

	r.logf(format, a...)
}

func (r *Reporter) logf(format string, a ...any) {
	if !r.duringSteps() {
		r.logLines = append(r.logLines, forge.Line{
			Time:    time.Now().UnixNano(),
			Content: fmt.Sprintf(format, a...),
		})
	}
}

func (r *Reporter) SetOutputs(outputs map[string]string) {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()

	for k, v := range outputs {
		if len(k) > 255 {
			r.logf("ignore output because the key is too long: %q", k)
			continue
		}
		if l := len(v); l > 1024*1024 {
			log.Println("ignore output because the value is too long:", k, l)
			r.logf("ignore output because the value %q is too long: %d", k, l)
		}
		if _, ok := r.outputs.Load(k); ok {
			continue
		}
		r.outputs.Store(k, v)
	}
}

func (r *Reporter) Close(lastWords string) error {
	r.stateMu.Lock()
	r.closed = true
	if r.state.Result == forge.Pending {
		if lastWords == "" {
			lastWords = "Early termination"
		}
		for i := range r.state.Steps {
			if r.state.Steps[i].Result == forge.Pending {
				r.state.Steps[i].Result = forge.Cancelled
			}
		}
		r.state.Result = forge.Failure
		r.logLines = append(r.logLines, forge.Line{
			Time:    time.Now().UnixNano(),
			Content: lastWords,
		})
		r.state.Stopped = time.Now().UnixNano()
	} else if lastWords != "" {
		r.logLines = append(r.logLines, forge.Line{
			Time:    time.Now().UnixNano(),
			Content: lastWords,
		})
	}
	r.stateMu.Unlock()

	// Wake up the daemon loop so it detects closed promptly.
	r.notifyLog()

	// Wait for Acknowledge
	select {
	case <-r.daemon:
	case <-time.After(60 * time.Second):
		close(r.daemon)
		log.Error("No Response from RunDaemon for 60s, continue best effort")
	}

	// Report the job outcome even when all log upload retry attempts have been exhausted
	return errors.Join(
		retry.Do(func() error {
			return r.ReportLog(true)
		}, retry.Context(r.ctx)),
		retry.Do(func() error {
			return r.ReportState(true)
		}, retry.Context(r.ctx)),
	)
}

func (r *Reporter) ReportLog(last bool) error {
	r.clientM.Lock()
	defer r.clientM.Unlock()

	r.stateMu.RLock()
	lines := r.logLines
	r.stateMu.RUnlock()

	if !last && len(lines) == 0 {
		return nil
	}

	start := time.Now()
	out, err := r.client.Log(r.ctx, &forge.LogIn{
		Task:  r.state.ID,
		Index: int64(r.logOffset),
		Lines: lines,
		Last:  last,
	})
	metrics.ReportLogDuration.Observe(time.Since(start).Seconds())
	if err != nil {
		metrics.ReportLogTotal.WithLabelValues(metrics.LabelResultError).Inc()
		metrics.ClientErrors.WithLabelValues(metrics.LabelOpLog).Inc()
		return err
	}
	metrics.ReportLogTotal.WithLabelValues(metrics.LabelResultSuccess).Inc()

	ack := int(out.Ack)
	if ack < r.logOffset {
		return errors.New("submitted logs are lost")
	}

	r.stateMu.Lock()
	r.logLines = r.logLines[ack-r.logOffset:]
	submitted := r.logOffset + len(lines)
	r.logOffset = ack
	remaining := len(r.logLines)
	r.stateMu.Unlock()
	if remaining != r.lastLogBufferRows {
		metrics.ReportLogBufferRows.Set(float64(remaining))
		r.lastLogBufferRows = remaining
	}

	if last && ack < submitted {
		return errors.New("not all logs are submitted")
	}

	return nil
}

// ReportState only reports the job result if reportResult is true
// RunDaemon never reports results even if result is set
func (r *Reporter) ReportState(reportResult bool) error {
	r.clientM.Lock()
	defer r.clientM.Unlock()

	// The outputs still to send, gathered once. A key whose value is no longer a
	// string is one the forge has already stored.
	var outputs []forge.Pair
	r.outputs.Range(func(k, v any) bool {
		if val, ok := v.(string); ok {
			outputs = append(outputs, forge.Pair{Name: k.(string), Value: val})
		}
		return true
	})

	// Consume stateChanged atomically with the snapshot; restored on error
	// below so a concurrent Fire() during UpdateTask isn't silently lost.
	// Heartbeat at stateReportInterval even when nothing changed, so the server
	// doesn't time out long-running silent jobs as orphaned (#826).
	last := r.lastReportedAtNanos.Load()
	withinHeartbeatInterval := last != 0 && time.Since(time.Unix(0, last)) < r.stateReportInterval
	r.stateMu.Lock()
	if !reportResult && !r.stateChanged && len(outputs) == 0 && withinHeartbeatInterval {
		r.stateMu.Unlock()
		return nil
	}
	// A copy, so the steps this report describes cannot change under it while the
	// call is in flight. Step holds no references, so cloning the slice is deep.
	state := *r.state
	state.Steps = slices.Clone(r.state.Steps)
	r.stateChanged = false
	r.stateMu.Unlock()

	if !reportResult {
		state.Result = forge.Pending
	}

	start := time.Now()
	out, err := r.client.State(r.ctx, &forge.StateIn{
		State:   state,
		Outputs: outputs,
	})
	metrics.ReportStateDuration.Observe(time.Since(start).Seconds())
	if err != nil {
		metrics.ReportStateTotal.WithLabelValues(metrics.LabelResultError).Inc()
		metrics.ClientErrors.WithLabelValues(metrics.LabelOpState).Inc()
		r.stateMu.Lock()
		r.stateChanged = true
		r.stateMu.Unlock()
		return err
	}
	metrics.ReportStateTotal.WithLabelValues(metrics.LabelResultSuccess).Inc()
	r.lastReportedAtNanos.Store(time.Now().UnixNano())

	for _, k := range out.Stored {
		r.outputs.Store(k, struct{}{})
	}

	if out.State.Result == forge.Cancelled {
		r.cancel()
	}

	var noSent []string
	r.outputs.Range(func(k, v any) bool {
		if _, ok := v.(string); ok {
			noSent = append(noSent, k.(string))
		}
		return true
	})
	if len(noSent) > 0 {
		return fmt.Errorf("there are still outputs that have not been sent: %v", noSent)
	}

	return nil
}

func (r *Reporter) duringSteps() bool {
	if steps := r.state.Steps; len(steps) == 0 {
		return false
	} else if first := steps[0]; first.Result == forge.Pending && first.LogLength == 0 {
		return false
	} else if last := steps[len(steps)-1]; last.Result != forge.Pending {
		return false
	}
	return true
}

var stringToResult = map[string]forge.Result{
	"success":   forge.Success,
	"failure":   forge.Failure,
	"skipped":   forge.Skipped,
	"cancelled": forge.Cancelled,
}

func (r *Reporter) parseResult(result any) (forge.Result, bool) {
	str := ""
	if v, ok := result.(string); ok { // for jobResult
		str = v
	} else if v, ok := result.(fmt.Stringer); ok { // for stepResult
		str = v.String()
	}

	ret, ok := stringToResult[str]
	return ret, ok
}

var cmdRegex = regexp.MustCompile(`^::([^ :]+)( .*)?::(.*)$`)

func (r *Reporter) handleCommand(originalContent, command, value string) *string {
	if r.stopCommandEndToken != "" && command != r.stopCommandEndToken {
		return &originalContent
	}

	switch command {
	case "add-mask":
		r.addMask(value)
		return nil
	case "debug":
		if r.debugOutputEnabled {
			return &value
		}
		return nil

	case "notice":
		// Not implemented yet, so just return the original content.
		return &originalContent
	case "warning":
		// Not implemented yet, so just return the original content.
		return &originalContent
	case "error":
		// Not implemented yet, so just return the original content.
		return &originalContent
	case "group":
		// Returning the original content, because I think the frontend
		// will use it when rendering the output.
		return &originalContent
	case "endgroup":
		// Ditto
		return &originalContent
	case "stop-commands":
		r.stopCommandEndToken = value
		return nil
	case r.stopCommandEndToken:
		r.stopCommandEndToken = ""
		return nil
	}
	return &originalContent
}

// parseLine renders one log entry as a line to send. It reports false for an
// entry that carried a workflow command and no output of its own: there is
// nothing to show, and a zero Line would print a blank line.
func (r *Reporter) parseLine(entry *log.Entry) (forge.Line, bool) {
	content := strings.TrimRightFunc(entry.Message, func(r rune) bool { return r == '\r' || r == '\n' })

	matches := cmdRegex.FindStringSubmatch(content)
	if matches != nil {
		if output := r.handleCommand(content, matches[1], matches[3]); output != nil {
			content = *output
		} else {
			return forge.Line{}, false
		}
	}

	content = r.logReplacer.Replace(content)

	return forge.Line{
		Time:    entry.Time.UnixNano(),
		Content: strings.ToValidUTF8(content, "?"),
	}, true
}

func (r *Reporter) addMask(msg string) {
	r.oldnew = append(r.oldnew, msg, "***")
	r.logReplacer = strings.NewReplacer(r.oldnew...)
}
