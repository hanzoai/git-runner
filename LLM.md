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

Five operations, a POST each, under `/v1/runner` on the forge: `register`,
`declare`, `task`, `state`, `logs`. JSON in, JSON out; the address IS the
operation and the body is the whole input. Nothing here is generated: this wire
is spoken only between this runner and `hanzoai/git`, so it is written to suit
the two of them rather than to match somebody else's RPC framework.

The only forge paths the runner names are `/v1/runner/<operation>` for its own
traffic and `/v1/artifact/` for a job's (`ACTIONS_RESULTS_URL` is the bare forge
origin — see artifacts, below). Both are under `/v1`, and neither carries an
`/api` segment.

- The messages are `github.com/hanzoai/git/modules/actions/runner`, a nested
  module inside the forge whose `require` block is empty. It is the ONE
  definition of the protocol: the forge's handlers declare these very types, so
  a client and a server copy cannot drift. Requiring the whole forge module
  instead does not work — it pins `go.yaml.in/yaml/v4` forward with a `replace`
  that a dependent does not inherit, and `actionlint` then fails to compile.
- `client.Conn` is the client: it holds `<forge>/v1/runner/` and `call[In, Out]`
  does the round trip once, so each operation is one line on top of it. A non-2xx
  answer decodes to `client.Fault` — status, code, message — so a caller branches
  on a value, not on a string.
- The credential is `x-runner-uuid` + `x-runner-token` on every operation but
  `register`. `register` is also the reachability check; there is no ping.
- **The forge and this fleet share the wire, so they are deployed together.**
  Neither side negotiates a version and there is no shim: a protocol change
  lands in both repos or in neither, and a rollout takes the forge first and the
  runners second. In the gap a runner's operations fail, the poller backs off,
  and jobs queue on the forge until the fleet catches up.

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

- `make fmt`, `make lint-go`, `make tidy` after any `go.mod` change.
- `go test ./internal/...` is fast and covers the poller, the reporter and
  workflow generation. `internal/pkg/client/mocks` is a hand-written testify
  double — every operation has the same shape, so `answer` does the work once.
- Do not add a second way to reach the forge. `client.Client` is the interface;
  everything goes through it.
