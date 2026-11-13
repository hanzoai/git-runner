// The runner protocol is its own module so that the two sides of it — this
// forge and github.com/hanzoai/git-runner — share one definition without
// sharing a dependency graph. It requires nothing but the standard library,
// which is the whole point: a client gets the types and none of the server.
module github.com/hanzoai/git/modules/actions/runner

go 1.26
