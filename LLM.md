# Hanzo Git Runner — `github.com/hanzoai/git-runner`

The agent that executes Actions jobs for `git.hanzo.ai`. A fork of Gitea's
`act_runner`, which is itself a daemon wrapped around a vendored `nektos/act`
(here `act/`, our fork of it) that runs a workflow's steps in containers.

## Shape

- `main.go` → `internal/app/cmd` — two commands that matter: `register` (trade a
  registration token for an identity, write `.runner`) and `daemon` (declare,
  then poll).
- `internal/app/poll` — one poller; asks for work, hands each task to
  `internal/app/run`, bounded by `runner.capacity`.
- `internal/app/run` — turns a task into an `act` workflow and runs it.
- `internal/pkg/report` — streams the job's log and step state back.
- `internal/pkg/client` — the forge protocol. See below.
- `act/` — the vendored executor. Everything above talks to it; it does not
  talk back to the forge.

## The forge protocol

Five operations under `/v1/runner` on the forge — `register`, `declare`, `task`,
`state`, `log` — spoken as ZAP, the house wire. This is not JSON over HTTP with
a house-shaped URL: it is `zip.Call`, the op-call plane, so the body carries the
same bytes both ends hold in memory.

The forge paths the runner names are `/v1/runner/<operation>` and zip's own
`/.well-known/zip/op/<name>` for its traffic, and `/v1/artifact/` for a job's
(`ACTIONS_RESULTS_URL` is the bare forge origin — see artifacts, below). None of
them carries an `/api` segment.

- **The address IS the operation, and the name is derived, not spelled.** The
  forge registers `POST /v1/runner/<name>`; zip derives `post_runner_<name>`
  from it (the leading version segment drops out, so `/v1/` costs nothing in the
  name) and the client calls that. `TestOpNames` pins the client's five literals
  against `zip.ID`, so the two cannot drift.
- The messages are `github.com/hanzoai/git/modules/actions/runner`, a nested
  module inside the forge whose `require` block is empty. It is the ONE
  definition of the protocol: the forge's handlers declare these very types.
  Requiring the whole forge module instead does not work — it pins
  `go.yaml.in/yaml/v4` forward with a `replace` that a dependent does not
  inherit, and `actionlint` then fails to compile.
- **Two rules govern every field on this wire, and both were measured.** A `map`
  is REFUSED at encode ("map cannot cross the plane"), so name/value data
  travels as `[]Pair`. A `time.Time` is accepted and then arrives as the ZERO
  time with no error at all — every field of `time.Time` is unexported, and the
  encoder skips those — so every timestamp is an `int64` of unix NANOSECONDS.
  Nanoseconds because the stored log format keeps 100ns ticks.
- The credential is an embedded `forge.Credential` on the four authenticated
  inputs, declared once and serving both faces: HTTP headers over REST, ordinary
  arguments over the call plane. `client.Forge` stamps it, so no caller carries
  it. `register` is the one operation without one, and it doubles as the
  reachability check — there is no ping.
- A refusal arrives as the `*zip.HTTPError` the forge returned, status intact,
  so a caller branches on a value.
- `client.take` copies every string and `[]byte` out of a reply. **This is not
  belt-and-braces.** ZAP reads text zero-copy (`zap.Object.Text` is an
  `unsafe.String` over the message) and `zip.Call` returns the response to
  fasthttp's pool as it returns, so a reply's strings point at memory the next
  call overwrites. Measured on this wire: a token registered as `runner-secret`
  read back `1.2.3r-secret` one call later, the version string of the call in
  between laid over its first five bytes, and the forge answered 401 to a runner
  that was registered. `TestOwn` pins it. When zip copies on decode, delete this.
- `client.dial` registers its own transport scheme (`forge`) rather than taking
  `http`/`https`: registering a scheme REPLACES what is under that name, and
  zip's built-in entries carry a Serve half this one does not. It exists because
  a `zip.Call` cannot be cancelled once it is on the wire — a context deadline
  fails it before the first byte and never after — so the wait for an answer has
  to be a ceiling on the transport. That ceiling is `runner.fetch_timeout`, and
  it now bounds every call, not only the one that asks for a job.
- **There is no long poll and there never was.** `task` answers immediately,
  always. `TaskIn.Queue` is the queue version this runner last saw; a forge
  whose version still matches has queued nothing and answers without opening the
  assignment transaction. Cadence belongs to the poller (`fetch_interval`,
  jitter, capped backoff). A `TaskOut` with a nil `Task` is an idle forge; an
  error is an error. Those were once the same thing, and a forge answering in
  six seconds against a five second wait was therefore indistinguishable from an
  idle one in every log line and every metric — while resetting the error
  counter that would have raised the alarm. `TestPoller_TimeoutIsAnError`.
- `Task.Context` is a struct, not a map. It has to be — a map is refused — and
  it should be: as a map, the forge sent `git_runtime_token` while the runner
  read `gitea_runtime_token` and silently fell back to the task token, so
  `ACTIONS_RUNTIME_TOKEN` was the wrong secret and the mask covered nothing.
  A field is a compile error; a key is a shrug.
- **The forge and this fleet share the wire, so they are deployed together.**
  Neither side negotiates a version and there is no shim: a protocol change
  lands in both repos or in neither, and a rollout takes the forge first and the
  runners second. In the gap a runner's operations fail, the poller backs off,
  and jobs queue on the forge until the fleet catches up.
- `.runner` is unchanged by any of this. Not one of its fields came from the old
  wire, and the two that matter — `uuid` and `token` — are exactly the
  credential the new one sends, so a registration made before the cutover keeps
  working after it.

## Artifacts belong to the JavaScript, not to us

The runner does not implement artifact upload; it only tells a job where to
send it, and the two protocols place that differently:

- `ACTIONS_RUNTIME_URL` — `actions/upload-artifact@v3` and its download
  counterpart concatenate `_apis/pipelines/…` onto this value verbatim, so the
  whole base is ours. It is `<forge>/v1/artifact/`, and the trailing slash is
  part of the contract.
- `ACTIONS_RESULTS_URL` — `@actions/artifact` v2 (upload-artifact@v4+) reduces
  this to `new URL(…).origin` and appends its own
  `twirp/github.actions.results.api.v1.ArtifactService/…`, so nothing about
  that path is ours to place.
- That same library throws `GHESNotSupportedError` unless the host is
  `github.com`, `*.ghe.com` or `*.localhost`. On `git.hanzo.ai` every v4 step
  therefore fails before opening a connection. Workflows that need artifacts
  pin `@v3`.

## Working here

- `make help` lists the development targets.
- `make fmt` formats `.go` files, `make lint-go` lints them, `make tidy` after
  any `go.mod` change.
- One test: `go test -run '^TestName$' ./modulepath/`.
- A new `.go` file carries the current year in its copyright header.
- No trailing whitespace in edited files.
- Preserve existing code comments; do not remove or rewrite one that is still
  relevant.
- `go test ./internal/...` is fast and covers the poller, the reporter, workflow
  generation and the wire itself: `internal/pkg/client` stands a real `zip.App`
  up on a socket and exercises all five operations against it, so an encode that
  refuses or a value that does not survive the wire fails here rather than in
  production. `internal/pkg/client/mocks` is a hand-written testify double —
  every operation has the same shape, so `answer` does the work once.
- Do not add a second way to reach the forge. `client.Client` is the interface;
  everything goes through it.

### Conventions inherited from upstream act_runner

- Never force-push, amend, or squash unless asked. Use new commits and normal
  push for pull request updates.
- Include authorship attribution in issue and pull request comments.
- Commit attribution is set by the git hook; don't add co-author trailers.
