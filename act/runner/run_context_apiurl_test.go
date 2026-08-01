package runner

import (
	"context"
	"testing"

	"github.com/hanzoai/git-runner/act/model"

	"github.com/stretchr/testify/assert"
)

// The server publishes where its API lives via the github context's "api_url".
// The runner used to discard that and hardcode <instance>/api/v1, so a forge
// serving its API anywhere else made every octokit-based action 404 ("Not
// found." out of docker/metadata-action, which takes the whole build with it).
func TestGetGithubContextAPIURLHonoursPreset(t *testing.T) {
	for _, tc := range []struct {
		name      string
		presetAPI string
		instance  string
		want      string
	}{
		{
			name:      "server-supplied api_url wins",
			presetAPI: "https://git.hanzo.ai/v1",
			instance:  "git.hanzo.ai",
			want:      "https://git.hanzo.ai/v1",
		},
		{
			name:      "empty api_url falls back to the upstream Gitea layout",
			presetAPI: "",
			instance:  "gitea.example.com",
			want:      "https://gitea.example.com/api/v1",
		},
		{
			name:      "a scheme already on the instance is not doubled",
			presetAPI: "",
			instance:  "https://gitea.example.com",
			want:      "https://gitea.example.com/api/v1",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rc := &RunContext{
				Config: &Config{
					GitHubInstance: tc.instance,
					PresetGitHubContext: &model.GithubContext{
						APIURL: tc.presetAPI,
					},
				},
				Run: &model.Run{
					JobID: "job",
					Workflow: &model.Workflow{
						Jobs: map[string]*model.Job{"job": {}},
					},
				},
			}

			assert.Equal(t, tc.want, rc.getGithubContext(context.Background()).APIURL)
		})
	}
}

// withGithubEnv must mirror the resolved context. It used to recompute the
// suffix, so $GITHUB_API_URL disagreed with ${{ github.api_url }} — and actions
// read the env one.
func TestWithGithubEnvAPIURLMirrorsContext(t *testing.T) {
	rc := &RunContext{
		Config: &Config{GitHubInstance: "git.hanzo.ai"},
		Run: &model.Run{
			JobID: "job",
			Workflow: &model.Workflow{
				Jobs: map[string]*model.Job{"job": {}},
			},
		},
	}

	gh := &model.GithubContext{APIURL: "https://git.hanzo.ai/v1"}
	env := rc.withGithubEnv(context.Background(), gh, map[string]string{})

	assert.Equal(t, "https://git.hanzo.ai/v1", env["GITHUB_API_URL"])
}

// An empty context still yields a usable default rather than an empty env var.
func TestWithGithubEnvAPIURLFallsBack(t *testing.T) {
	rc := &RunContext{
		Config: &Config{GitHubInstance: "gitea.example.com"},
		Run: &model.Run{
			JobID: "job",
			Workflow: &model.Workflow{
				Jobs: map[string]*model.Job{"job": {}},
			},
		},
	}

	env := rc.withGithubEnv(context.Background(), &model.GithubContext{}, map[string]string{})

	assert.Equal(t, "https://gitea.example.com/api/v1", env["GITHUB_API_URL"])
}
