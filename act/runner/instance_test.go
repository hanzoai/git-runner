// Copyright 2026 The Hanzo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package runner

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hanzoai/git-runner/act/common"
	"github.com/hanzoai/git-runner/act/common/git"
	"github.com/hanzoai/git-runner/act/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The instance this runner is registered to, reached under both of its names:
// the service it dials from inside the cluster, and the host it publishes.
const (
	dialled   = "http://hanzo-git.hanzo.svc"
	published = "https://git.hanzo.ai/"
	// Not a credential: a marker. Any wire to a foreign host carrying this
	// string is a leak.
	credential = "instance-token-never-leaves-the-instance"
)

func instanceConfig() *Config {
	return &Config{
		GitHubInstance: dialled,
		ServerURL:      published,
		Secrets:        map[string]string{"GITHUB_TOKEN": credential},
	}
}

// A host that is not ours gets nothing. This is the point of the whole change:
// the same code path fetches actions/checkout from github.com on every run of
// every repository, and it must go out anonymous.
func TestGitCloneTokenForeignHostGetsNothing(t *testing.T) {
	foreign := []string{
		"https://github.com/actions/checkout",
		"https://github.com/docker/build-push-action",
		"https://api.github.com/hanzoai/ci",

		// Ours as a suffix of theirs. A DNS name they can register today.
		"https://git.hanzo.ai.evil.tld/hanzoai/ci",
		"https://xgit.hanzo.ai/hanzoai/ci",
		"https://hanzo-git.hanzo.svc.evil.tld/hanzoai/ci",

		// Ours somewhere in the URL that is not the host.
		"https://evil.tld/git.hanzo.ai/hanzoai/ci",
		"https://evil.tld/hanzoai/ci#git.hanzo.ai",
		"https://evil.tld/hanzoai/ci?host=git.hanzo.ai",
		"https://git.hanzo.ai@evil.tld/hanzoai/ci",
		"https://git.hanzo.ai:x@evil.tld/hanzoai/ci",

		// Ours, near enough to read as ours at a glance.
		"https://git.hanzo.ai.com/hanzoai/ci",
		"https://githanzo.ai/hanzoai/ci",
		"https://git-hanzo.ai/hanzoai/ci",

		// Not an address at all.
		"",
		"git.hanzo.ai",
		"git.hanzo.ai/hanzoai/ci",
		"hanzoai/ci",
		"://git.hanzo.ai/hanzoai/ci",
	}

	conf := instanceConfig()
	for _, cloneURL := range foreign {
		t.Run(cloneURL, func(t *testing.T) {
			assert.False(t, conf.isInstance(cloneURL))
			assert.Empty(t, getGitCloneToken(conf, cloneURL),
				"a credential for %s went to a host that is not the instance", cloneURL)
		})
	}
}

// Both of the instance's own addresses are the instance.
func TestGitCloneTokenInstanceGetsCredential(t *testing.T) {
	conf := instanceConfig()
	for _, cloneURL := range []string{
		"http://hanzo-git.hanzo.svc/hanzoai/ci",
		"https://git.hanzo.ai/hanzoai/ci",
		"https://git.hanzo.ai/hanzoai/universe",
	} {
		t.Run(cloneURL, func(t *testing.T) {
			assert.True(t, conf.isInstance(cloneURL))
			assert.Equal(t, credential, getGitCloneToken(conf, cloneURL))
		})
	}
}

// A clone URL that already carries basic auth keeps the credential it was given.
func TestGitCloneTokenLeavesEmbeddedAuthAlone(t *testing.T) {
	conf := instanceConfig()
	assert.Empty(t, getGitCloneToken(conf, "https://someone:secret@git.hanzo.ai/hanzoai/ci"))
}

// A server that publishes nothing is known by the one address the runner dials,
// which is how every act CLI invocation is configured.
func TestGitCloneTokenWithoutPublishedAddress(t *testing.T) {
	conf := &Config{
		GitHubInstance: "https://git.hanzo.ai",
		Secrets:        map[string]string{"GITHUB_TOKEN": credential},
	}
	assert.Equal(t, credential, getGitCloneToken(conf, "https://git.hanzo.ai/hanzoai/ci"))
	assert.Empty(t, getGitCloneToken(conf, "https://github.com/actions/checkout"))

	// A host the server has not claimed is a host we do not know, even when it
	// looks like the one we are standing in front of. Nothing is guessed.
	dialledOnly := &Config{
		GitHubInstance: dialled,
		Secrets:        map[string]string{"GITHUB_TOKEN": credential},
	}
	assert.Empty(t, getGitCloneToken(dialledOnly, "https://git.hanzo.ai/hanzoai/ci"))
}

// The whole step, at the call site that fetches it: what a `uses:` actually puts
// on the wire. A host-pinned action carries the credential; a github.com action
// in the same job carries none.
func TestStepActionRemoteClonesInstanceWithCredential(t *testing.T) {
	for _, tt := range []struct {
		name  string
		uses  string
		url   string
		token string
	}{
		{
			name:  "instance published host",
			uses:  "https://git.hanzo.ai/hanzoai/ci/.hanzo/actions/site@v1",
			url:   "https://git.hanzo.ai/hanzoai/ci",
			token: credential,
		},
		{
			name:  "instance dialled address",
			uses:  "http://hanzo-git.hanzo.svc/hanzoai/ci/.hanzo/actions/site@v1",
			url:   "http://hanzo-git.hanzo.svc/hanzoai/ci",
			token: credential,
		},
		{
			name:  "third party from the default action instance",
			uses:  "actions/checkout@v4",
			url:   "https://github.com/actions/checkout",
			token: "",
		},
		{
			name:  "third party pinned to its own host",
			uses:  "https://github.com/docker/login-action/@v3",
			url:   "https://github.com/docker/login-action",
			token: "",
		},
		{
			name:  "a host that ends in the instance host",
			uses:  "https://git.hanzo.ai.evil.tld/hanzoai/ci/.hanzo/actions/site@v1",
			url:   "https://git.hanzo.ai.evil.tld/hanzoai/ci",
			token: "",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var cloned git.NewGitCloneExecutorInput
			original := stepActionRemoteNewCloneExecutor
			stepActionRemoteNewCloneExecutor = func(input git.NewGitCloneExecutorInput) common.Executor {
				cloned = input
				return func(context.Context) error { return nil }
			}
			defer func() { stepActionRemoteNewCloneExecutor = original }()

			conf := instanceConfig()
			conf.DefaultActionInstance = "https://github.com"
			conf.ActionCacheDir = fmt.Sprintf("/tmp/instance-test-%d", time.Now().UnixNano())
			conf.NoSkipCheckout = true

			sar := &stepActionRemote{
				Step: &model.Step{Uses: tt.uses},
				RunContext: &RunContext{
					Config: conf,
					Run: &model.Run{
						JobID:    "1",
						Workflow: &model.Workflow{Jobs: map[string]*model.Job{"1": {}}},
					},
				},
				readAction: func(context.Context, *model.Step, string, string, actionYamlReader, fileWriter) (*model.Action, error) {
					return &model.Action{}, nil
				},
			}

			require.NoError(t, sar.prepareActionExecutor()(context.Background()))
			assert.Equal(t, tt.url, cloned.URL)
			assert.Equal(t, tt.token, cloned.Token)
		})
	}
}
