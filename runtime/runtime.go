// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package runtime

import (
	"context"
	"strings"

	runnerv1 "code.gitea.io/actions-proto-go/runner/v1"
	log "github.com/sirupsen/logrus"

	"gitea.com/gitea/act_runner/artifactcache"
	"gitea.com/gitea/act_runner/client"
)

// Runner runs the pipeline.
type Runner struct {
	Machine       string
	Version       string
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
	if s.CacheHandler != nil {
		env["ACTIONS_CACHE_URL"] = s.CacheHandler.ExternalURL() + "/"
	}
	return NewTask(s.ForgeInstance, task.Id, s.Client, env, s.platformPicker).Run(ctx, task, s.Machine, s.Version)
}

func (s *Runner) platformPicker(labels []string) string {
	platforms := make(map[string]string, len(s.Labels))
	for _, l := range s.Labels {
		label, schema, arg, err := ParseLabel(l)
		if err != nil {
			log.Errorf("invaid label %q: %v", l, err)
			continue
		}

		switch schema {
		case "docker":
			// TODO "//" will be ignored, maybe we should use 'ubuntu-18.04:docker:node:16-buster' instead
			platforms[label] = strings.TrimPrefix(arg, "//")
		case "host":
			platforms[label] = "-self-hosted"
		default:
			// It should not happen, because ParseLabel has checked it.
			continue
		}
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

	// return default.
	// So the runner receives a task with a label that the runner doesn't have,
	// it happens when the user have edited the label of the runner in the web UI.
	return "node:16-bullseye" // TODO: it may be not correct, what if the runner is used as host mode only?
}
