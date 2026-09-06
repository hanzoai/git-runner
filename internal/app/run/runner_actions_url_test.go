// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package run

import (
	"testing"

	forge "github.com/hanzoai/git/modules/actions/runner"
	"github.com/stretchr/testify/assert"

	"github.com/hanzoai/git-runner/internal/pkg/config"
)

// taskWithActionsURL builds a task whose context carries the given
// gitea_default_actions_url. Passing present=false omits the field entirely,
// which is what a server that does not send it produces.
func taskWithActionsURL(t *testing.T, url string, present bool) *forge.Task {
	t.Helper()
	fields := map[string]any{}
	if present {
		fields["gitea_default_actions_url"] = url
	}
	return &forge.Task{Context: fields}
}

func TestGetDefaultActionsURL(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		present bool
		mirror  string
		want    string
	}{
		// ⛔ The regression this test exists for. An absent field yielded "",
		// and act then built "https:///actions/checkout" — no host — so every
		// job died before its first step unless the action was already cached
		// locally. Four of our ten runners had an empty cache, which made CI
		// pass or fail depending on which runner claimed the job.
		{
			name:    "absent field falls back rather than returning empty",
			present: false,
			want:    "https://github.com",
		},
		{
			name:    "empty value falls back rather than returning empty",
			url:     "",
			present: true,
			want:    "https://github.com",
		},
		{
			name:    "absent field prefers the configured mirror",
			present: false,
			mirror:  "https://ghproxy.example.com",
			want:    "https://ghproxy.example.com",
		},
		// Pre-existing behaviour, kept intact.
		{
			name:    "github with a mirror configured uses the mirror",
			url:     "https://github.com",
			present: true,
			mirror:  "https://ghproxy.example.com",
			want:    "https://ghproxy.example.com",
		},
		{
			name:    "github with no mirror stays github",
			url:     "https://github.com",
			present: true,
			want:    "https://github.com",
		},
		{
			name:    "a self-hosted instance is passed through untouched",
			url:     "https://git.hanzo.ai",
			present: true,
			mirror:  "https://ghproxy.example.com",
			want:    "https://git.hanzo.ai",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &Runner{cfg: &config.Config{}}
			r.cfg.Runner.GithubMirror = tc.mirror

			got := r.getDefaultActionsURL(taskWithActionsURL(t, tc.url, tc.present))

			assert.Equal(t, tc.want, got)
			// Whatever else is true, the result must never be empty: an empty
			// value is what produces "https:///…" and "no Host in request URL".
			assert.NotEmpty(t, got, "actions URL must never resolve to empty")
		})
	}
}
