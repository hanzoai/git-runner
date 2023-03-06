// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package runtime

import (
	"context"
	"strings"

	runnerv1 "code.gitea.io/actions-proto-go/runner/v1"

	"gitea.com/gitea/act_runner/artifactcache"
	"gitea.com/gitea/act_runner/client"
)

// Runner runs the pipeline.
type Runner struct {
	Machine       string
	ForgeInstance string
	Environ       map[string]string
	Client        client.Client
	Labels        []string
	CacheHandler  *artifactcache.Handler
}

// Run runs the pipeline stage.
func (s *Runner) Run(ctx context.Context, task *runnerv1.Task) error {
	env := map[string]string{}
	for k, v := range s.Environ {
		env[k] = v
	}
	env["ACTIONS_CACHE_URL"] = s.CacheHandler.ExternalURL() + "/"
	return NewTask(s.ForgeInstance, task.Id, s.Client, env, s.platformPicker).Run(ctx, task, s.Machine)
}

func (s *Runner) platformPicker(labels []string) string {
	// "ubuntu-18.04:docker://node:16-buster"
	// "self-hosted"

	platforms := make(map[string]string, len(labels))
	for _, l := range s.Labels {
		// "ubuntu-18.04:docker://node:16-buster"
		splits := strings.SplitN(l, ":", 2)
		if len(splits) == 1 {
			// identifier for non docker execution environment
			platforms[splits[0]] = "-self-hosted"
			continue
		}
		// ["ubuntu-18.04", "docker://node:16-buster"]
		k, v := splits[0], splits[1]

		if prefix := "docker://"; !strings.HasPrefix(v, prefix) {
			continue
		} else {
			v = strings.TrimPrefix(v, prefix)
		}
		// ubuntu-18.04 => node:16-buster
		platforms[k] = v
	}

	for _, label := range labels {
		if v, ok := platforms[label]; ok {
			return v
		}
	}

	// TODO: support multiple labels
	// like:
	//   ["ubuntu-22.04"] => "ubuntu:22.04"
	//   ["with-gpu"] => "linux:with-gpu"
	//   ["ubuntu-22.04", "with-gpu"] => "ubuntu:22.04_with-gpu"

	// return default
	return "node:16-bullseye"
}
