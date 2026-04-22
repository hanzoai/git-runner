// Copyright 2022 The Gitea Authors. All rights reserved.
// Copyright 2022 The nektos/act Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package runner

import (
	"bytes"
	"context"
	"io"
	"testing"

	"gitea.com/gitea/act_runner/act/container"
	"gitea.com/gitea/act_runner/act/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestStepDockerMain(t *testing.T) {
	cm := &containerMock{}

	var input *container.NewContainerInput

	// mock the new container call
	origContainerNewContainer := ContainerNewContainer
	ContainerNewContainer = func(containerInput *container.NewContainerInput) container.ExecutionsEnvironment {
		input = containerInput
		return cm
	}
	defer (func() {
		ContainerNewContainer = origContainerNewContainer
	})()

	ctx := context.Background()

	sd := &stepDocker{
		RunContext: &RunContext{
			StepResults: map[string]*model.StepResult{},
			Config:      &Config{},
			Run: &model.Run{
				JobID: "1",
				Workflow: &model.Workflow{
					Jobs: map[string]*model.Job{
						"1": {
							Defaults: model.Defaults{
								Run: model.RunDefaults{
									Shell: "bash",
								},
							},
						},
					},
				},
			},
			JobContainer: cm,
		},
		Step: &model.Step{
			ID:               "1",
			Uses:             "docker://node:14",
			WorkingDirectory: "workdir",
		},
	}
	sd.RunContext.ExprEval = sd.RunContext.NewExpressionEvaluator(ctx)

	cm.On("Pull", false).Return(func(ctx context.Context) error {
		return nil
	})

	cm.On("Remove").Return(func(ctx context.Context) error {
		return nil
	})

	cm.On("Create", []string(nil), []string(nil)).Return(func(ctx context.Context) error {
		return nil
	})

	cm.On("Start", true).Return(func(ctx context.Context) error {
		return nil
	})

	cm.On("Close").Return(func(ctx context.Context) error {
		return nil
	})

	cm.On("Copy", "/var/run/act", mock.AnythingOfType("[]*container.FileEntry")).Return(func(ctx context.Context) error {
		return nil
	})

	cm.On("UpdateFromEnv", "/var/run/act/workflow/envs.txt", mock.AnythingOfType("*map[string]string")).Return(func(ctx context.Context) error {
		return nil
	})

	cm.On("UpdateFromEnv", "/var/run/act/workflow/statecmd.txt", mock.AnythingOfType("*map[string]string")).Return(func(ctx context.Context) error {
		return nil
	})

	cm.On("UpdateFromEnv", "/var/run/act/workflow/outputcmd.txt", mock.AnythingOfType("*map[string]string")).Return(func(ctx context.Context) error {
		return nil
	})

	cm.On("GetContainerArchive", ctx, "/var/run/act/workflow/pathcmd.txt").Return(io.NopCloser(&bytes.Buffer{}), nil)

	err := sd.main()(ctx)
	assert.Nil(t, err) //nolint:testifylint // pre-existing issue from nektos/act

	assert.Equal(t, "node:14", input.Image)

	cm.AssertExpectations(t)
}

func TestStepDockerPrePost(t *testing.T) {
	ctx := context.Background()
	sd := &stepDocker{}

	err := sd.pre()(ctx)
	assert.Nil(t, err) //nolint:testifylint // pre-existing issue from nektos/act

	err = sd.post()(ctx)
	assert.Nil(t, err) //nolint:testifylint // pre-existing issue from nektos/act
}
