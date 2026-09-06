// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package run

import (
	"testing"

	forge "github.com/hanzoai/git/modules/actions/runner"
	"github.com/stretchr/testify/assert"

	"github.com/hanzoai/git-runner/internal/pkg/config"
)

func TestGetDefaultActionsURL(t *testing.T) {
	cases := []struct {
		name   string
		url    string
		mirror string
		want   string
	}{
		// ⛔ The regression this test exists for. An unset value yielded "",
		// and act then built "https:///actions/checkout" — no host — so every
		// job died before its first step unless the action was already cached
		// locally. Four of our ten runners had an empty cache, which made CI
		// pass or fail depending on which runner claimed the job.
		{
			name: "unset falls back rather than resolving to empty",
			want: "https://github.com",
		},
		{
			name:   "unset prefers the configured mirror",
			mirror: "https://ghproxy.example.com",
			want:   "https://ghproxy.example.com",
		},
		// Pre-existing behaviour, kept intact.
		{
			name:   "github with a mirror configured uses the mirror",
			url:    "https://github.com",
			mirror: "https://ghproxy.example.com",
			want:   "https://ghproxy.example.com",
		},
		{
			name: "github with no mirror stays github",
			url:  "https://github.com",
			want: "https://github.com",
		},
		{
			name:   "a self-hosted instance is passed through untouched",
			url:    "https://git.hanzo.ai",
			mirror: "https://ghproxy.example.com",
			want:   "https://git.hanzo.ai",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &Runner{cfg: &config.Config{}}
			r.cfg.Runner.GithubMirror = tc.mirror

			task := &forge.Task{Context: forge.Context{ActionsURL: tc.url}}
			got := r.getDefaultActionsURL(task)

			assert.Equal(t, tc.want, got)
			// Whatever else is true, the result must never be empty: an empty
			// value is what produces "https:///…" and "no Host in request URL".
			assert.NotEmpty(t, got, "actions URL must never resolve to empty")
		})
	}
}
