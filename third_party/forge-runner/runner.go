// Copyright 2026 Hanzo AI, Inc. All rights reserved.
// SPDX-License-Identifier: MIT

// Package runner holds the values a runner and the forge exchange: the input
// and output of each of the five ops the forge serves under /v1/runner.
//
// It is a leaf on purpose. The runner is a separate module and imports this
// package to get the very types the handlers declare, so there is one
// definition of the protocol rather than a server copy and a client copy that
// drift. Nothing here may import the rest of the forge.
//
// Two rules govern every field, and both come from the wire rather than from
// taste. ZAP gives a field an offset and a width, so:
//
//   - No map. A map has neither, and it is refused outright when the layout is
//     derived. A set of names and values crosses as an ordered []Pair.
//   - No time.Time. Every one of its fields is unexported, so it derives an
//     empty layout: it is accepted, carries nothing, and arrives as the zero
//     time with no error anywhere. Every instant here is an int64 of unix
//     nanoseconds, and 0 means unset. Nanoseconds rather than milliseconds
//     because the stored log format keeps 100ns ticks.
//
// Field ORDER is part of the wire: a field is found at its offset, not by its
// name. Append at the end, and only at the end.
package runner

// Result is how a job, a step or a task finished. The empty value means it has
// not finished, so a zero Result is "still going" rather than a fabricated
// outcome. The four names are the same words the forge's Status uses, so the
// wire needs no numbering to stay in step with the database.
type Result string

const (
	Pending   Result = ""
	Success   Result = "success"
	Failure   Result = "failure"
	Cancelled Result = "cancelled"
	Skipped   Result = "skipped"
)

// Credential is what a registered runner presents on every op but register.
//
// It is embedded rather than repeated so it is declared once and serves both
// faces of the same op. Over HTTP the two values arrive as headers, which is
// what the `header:` tags say and what `json:"-"` makes the ONLY way to present
// them: a body carrying a uuid and a token reaches the handler with both fields
// empty. Over the call plane there are no headers, so they travel as ordinary
// arguments in their own slots.
type Credential struct {
	// UUID is the handle the forge minted for this runner at registration.
	UUID string `json:"-" header:"x-runner-uuid"`
	// Token is the secret minted alongside that handle. It is presented on
	// every call and is not readable back from anywhere.
	Token string `json:"-" header:"x-runner-token"`
}

// Pair is one name and the value under it, and it is how every set of named
// values on this protocol crosses.
type Pair struct {
	// Name is what the value is filed under.
	Name string `json:"name"`
	// Value is the value itself, always a string on this wire.
	Value string `json:"value"`
}

// Identity is what the forge knows about a runner. The token is minted at
// registration and is the runner's credential for every later op.
type Identity struct {
	// ID is the forge's own row id for this runner.
	ID int64 `json:"id"`
	// UUID is the handle the runner presents on every later op.
	UUID string `json:"uuid"`
	// Token is the secret that authenticates that handle. It is in cleartext
	// here and nowhere else; the forge stores only a digest of it.
	Token string `json:"token"`
	// Name is what a person calls this runner in the forge's UI.
	Name string `json:"name"`
	// Version is the runner build the forge last heard from.
	Version string `json:"version"`
	// Labels are what a workflow's `runs-on:` selects this runner by.
	Labels []string `json:"labels"`
	// Ephemeral means the runner takes ONE job and exits, so the forge must not
	// expect it back.
	Ephemeral bool `json:"ephemeral"`
}

// RegisterIn trades a registration token for a runner identity. It is the one
// input with no Credential, because a runner has none until this answers; the
// token here is the REGISTRATION token, a different secret from the runner
// token the reply carries.
type RegisterIn struct {
	// Name is what to call this runner. Required.
	Name string `json:"name"`
	// Token is the REGISTRATION secret, a different secret from the runner
	// token the reply carries. Required.
	Token string `json:"token"`
	// Version is the runner build asking to register.
	Version string `json:"version"`
	// Labels are what a workflow's `runs-on:` will select this runner by.
	Labels []string `json:"labels"`
	// Ephemeral declares that this runner takes ONE job and exits.
	Ephemeral bool `json:"ephemeral"`
	// Capabilities are the optional protocol behaviours this runner
	// understands, e.g. "cancelling".
	Capabilities []string `json:"capabilities"`
}

// RegisterOut carries the minted runner credential in cleartext, exactly once.
type RegisterOut struct {
	// Runner is the minted identity, carrying the token in cleartext once.
	Runner Identity `json:"runner"`
}

// DeclareIn republishes what a registered runner can do. A runner sends it on
// start and after its configuration changes, so labels and version follow the
// runner without re-registering it.
type DeclareIn struct {
	Credential
	// Version is the runner build now running.
	Version string `json:"version"`
	// Labels replace what the forge holds, so a relabelled runner follows its
	// configuration without re-registering.
	Labels []string `json:"labels"`
	// Capabilities are the optional protocol behaviours this runner
	// understands, e.g. "cancelling".
	Capabilities []string `json:"capabilities"`
}

// DeclareOut answers with the stored identity and the capabilities this forge
// understands, so a runner learns what the other side supports from the reply
// body rather than from a header.
type DeclareOut struct {
	// Runner is the stored identity as the forge now holds it. Its Token is
	// empty: the credential is minted once, by register.
	Runner Identity `json:"runner"`
	// Capabilities are what THIS FORGE understands, so a runner learns what the
	// other side supports from the reply body rather than from a header.
	Capabilities []string `json:"capabilities"`
}

// TaskIn asks for work. Queue is the queue version the runner last saw; when it
// equals the forge's, nothing has been queued since and the forge answers
// without opening the assignment transaction.
type TaskIn struct {
	Credential
	// Queue is the queue version the runner last saw. When it equals the
	// forge's, nothing has been queued since.
	Queue int64 `json:"queue"`
}

// TaskOut carries an assigned task, or no task and the current queue version
// for the runner to ask against next time. A nil Task is the "no work" answer
// and the only one: a present task always has a nonzero ID.
type TaskOut struct {
	// Task is the assigned job, or nil for "no work".
	Task *Task `json:"task,omitempty"`
	// Queue is the forge's current queue version, to ask against next time.
	Queue int64 `json:"queue"`
}

// Task is one job, expanded and ready to execute. ID is unique for all time —
// unlike a run or job id, it is never reused.
type Task struct {
	// ID identifies this task on every later state and log report.
	ID int64 `json:"id"`
	// Workflow is the workflow document verbatim. The forge decides THAT a job
	// runs; act, on the runner, decides what the document means.
	Workflow []byte `json:"workflow"`
	// Context is what the job evaluates github.* against.
	Context Context `json:"context"`
	// Secrets are the values masked out of the log and exposed as secrets.*.
	Secrets []Pair `json:"secrets,omitempty"`
	// Vars are the values exposed as vars.*.
	Vars []Pair `json:"vars,omitempty"`
	// Needs is what the jobs this one depends on produced.
	Needs []Need `json:"needs,omitempty"`
}

// Need is what one job this task depends on produced. Job is the key the
// forge's needs map held, promoted to a field so the set crosses as a list.
type Need struct {
	// Job is the depended-on job's id in the workflow.
	Job string `json:"job"`
	// Result is how it finished.
	Result Result `json:"result"`
	// Outputs are the values it published.
	Outputs []Pair `json:"outputs,omitempty"`
}

// Context is what a job evaluates its expressions against, and what the runner
// hands to act as the github context.
//
// It is a struct, and that is the change with the most behaviour behind it. As
// a map the two sides could disagree in silence, and did: the forge wrote
// git_runtime_token while the runner read gitea_runtime_token and quietly fell
// back to the task token, which nothing anywhere reported. A field is a compile
// error instead.
//
// It carries what the forge computes AND the runner reads, and nothing else.
// The map shipped about thirteen keys that were always the empty string —
// action, action_path, env, path, job's siblings — because act fills those in
// itself; they crossed the wire to be discarded.
type Context struct {
	// Event is the webhook payload that triggered the run, as opaque JSON. It
	// stays bytes because its shape belongs to the event and not to us: the
	// runner hands it to act, which evaluates github.event.* against it.
	Event []byte `json:"event"`
	// EventName is what triggered the run, e.g. "push".
	EventName string `json:"event_name"`
	// Job is which job of the workflow document this task is.
	Job string `json:"job"`
	// RunID identifies the run this task belongs to.
	RunID string `json:"run_id"`
	// RunNumber is the run's position in its repository, counting from 1.
	RunNumber string `json:"run_number"`
	// RunAttempt is which attempt of that run this is, counting from 1.
	RunAttempt string `json:"run_attempt"`
	// Actor is who caused the run.
	Actor string `json:"actor"`
	// Repository is "<owner>/<name>".
	Repository string `json:"repository"`
	// RepositoryOwner is the owner half of it.
	RepositoryOwner string `json:"repository_owner"`
	// Ref is the full ref the run is for, e.g. "refs/heads/main".
	Ref string `json:"ref"`
	// RefName is that ref without its refs/heads/ or refs/tags/ prefix.
	RefName string `json:"ref_name"`
	// RefType is "branch" or "tag".
	RefType string `json:"ref_type"`
	// HeadRef is the source branch of a pull request, empty otherwise.
	HeadRef string `json:"head_ref"`
	// BaseRef is the target branch of a pull request, empty otherwise.
	BaseRef string `json:"base_ref"`
	// Sha is the commit being run, in full.
	Sha string `json:"sha"`
	// ServerURL is the forge's own origin, as a link in a log points at it.
	ServerURL string `json:"server_url"`
	// APIURL is where the job reaches the forge's API.
	APIURL string `json:"api_url"`
	// RetentionDays is how long the run's artifacts are kept, as a number in a
	// string because that is what an expression reads.
	RetentionDays string `json:"retention_days"`
	// Token is the job's credential against the forge's own API — github.token
	// in an expression.
	Token string `json:"token"`
	// RuntimeToken authorizes the job against the actions runtime: artifacts,
	// the cache. The runner passes it as ACTIONS_RUNTIME_TOKEN and masks it out
	// of the log.
	RuntimeToken string `json:"runtime_token"`
	// ActionsURL is where act fetches an action repository from when a step
	// names one. A runner that receives nothing here composes an unusable URL
	// and every job dies before its first step, so the runner supplies its own
	// fallback rather than trusting this to be set.
	ActionsURL string `json:"actions_url"`
}

// StateIn reports progress. Outputs are sent incrementally: the reply names
// every key the forge has stored, so the runner stops resending them.
type StateIn struct {
	Credential
	// State is the task's progress and that of each of its steps.
	State State `json:"state"`
	// Outputs are values the job has published since the last report.
	Outputs []Pair `json:"outputs,omitempty"`
}

// StateOut echoes the task's result as the forge now holds it, which is how a
// runner learns its task was cancelled from elsewhere. Stored names the outputs
// the forge has written down.
type StateOut struct {
	// State is the task's result AS THE FORGE HOLDS IT, which may differ from
	// what was reported: that is how a runner learns it was cancelled.
	State State `json:"state"`
	// Stored names the outputs the forge has written down, so the runner stops
	// resending them.
	Stored []string `json:"stored,omitempty"`
}

// State is a task's progress and that of each of its steps. Started and Stopped
// are unix nanoseconds, 0 for unset.
type State struct {
	// ID is the task this is about.
	ID int64 `json:"id"`
	// Result is how it finished; empty means it has not.
	Result Result `json:"result"`
	// Started is when it began, in unix nanoseconds; 0 is unset.
	Started int64 `json:"started,omitempty"`
	// Stopped is when it finished, in unix nanoseconds; 0 is unset.
	Stopped int64 `json:"stopped,omitempty"`
	// Steps is each step's own progress, in the order the workflow declares.
	Steps []Step `json:"steps,omitempty"`
}

// Step is one step's progress and the slice of the task log it wrote. Started
// and Stopped are unix nanoseconds, 0 for unset.
type Step struct {
	// ID is the step's position in the job, counting from 0.
	ID int64 `json:"id"`
	// Result is how the step finished; empty means it has not.
	Result Result `json:"result"`
	// Started is when it began, in unix nanoseconds; 0 is unset.
	Started int64 `json:"started,omitempty"`
	// Stopped is when it finished, in unix nanoseconds; 0 is unset.
	Stopped int64 `json:"stopped,omitempty"`
	// LogIndex is the first line of the task log this step wrote.
	LogIndex int64 `json:"log_index"`
	// LogLength is how many lines it wrote from there.
	LogLength int64 `json:"log_length"`
}

// LogIn appends console output. Index is the position of the first line in the
// task's log, so a resent batch overlaps rather than duplicates. Last seals the
// log and moves it to storage.
type LogIn struct {
	Credential
	// Task is whose log this is.
	Task int64 `json:"task"`
	// Index is the position of the first line in that log, so a resent batch
	// overlaps rather than duplicates.
	Index int64 `json:"index"`
	// Lines is the batch itself.
	Lines []Line `json:"lines,omitempty"`
	// Last seals the log and moves it to storage. Appending past a seal is an
	// error; resending one is acknowledged.
	Last bool `json:"last,omitempty"`
}

// LogOut reports how far the log is durable: Ack is index + lines accepted, and
// the runner resends from there.
type LogOut struct {
	// Ack is how far the log is durable — index plus lines accepted — and where
	// the runner resends from.
	Ack int64 `json:"ack"`
}

// Line is one line of console output. Time is unix nanoseconds.
type Line struct {
	// Time is when the line was written, in unix nanoseconds.
	Time int64 `json:"time"`
	// Content is the line itself, without its terminator.
	Content string `json:"content"`
}
