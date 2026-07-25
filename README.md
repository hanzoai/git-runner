# git-runner

The Hanzo Git Actions runner — executes `.hanzo/workflows` jobs for
git.hanzo.ai. Our fork of the upstream act-runner lineage: module
`github.com/hanzoai/git-runner`, binary `git-runner`, env contract `GIT_*`
(`GIT_INSTANCE_URL`, `GIT_RUNNER_REGISTRATION_TOKEN`, `GIT_RUNNER_NAME`,
`GIT_RUNNER_LABELS`).

Image: `ghcr.io/hanzoai/git-runner` (dind variant, s6 init) — built by
`.hanzo/workflows/release.yml` on the runners themselves. Deployed by
`universe/infra/k8s/git-runner/statefulset.yaml`.
