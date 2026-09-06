// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package run

import (
	"testing"

	"github.com/hanzoai/git-runner/act/model"

	forge "github.com/hanzoai/git/modules/actions/runner"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"
	"gotest.tools/v3/assert"
)

func Test_generateWorkflow(t *testing.T) {
	type args struct {
		task *forge.Task
	}
	tests := []struct {
		name    string
		args    args
		assert  func(t *testing.T, wf *model.Workflow)
		want1   string
		wantErr bool
	}{
		{
			name: "has needs",
			args: args{
				task: &forge.Task{
					Workflow: []byte(`
name: Build and deploy
on: push

jobs:
  job9:
    needs: build
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - run: ./deploy --build ${{ needs.job1.outputs.output1 }}
      - run: ./deploy --build ${{ needs.job2.outputs.output2 }}
`),
					Needs: []forge.Need{
						{
							Job:     "job1",
							Outputs: []forge.Pair{{Name: "output1", Value: "output1 value"}},
							Result:  forge.Success,
						},
						{
							Job:     "job2",
							Outputs: []forge.Pair{{Name: "output2", Value: "output2 value"}},
							Result:  forge.Success,
						},
					},
				},
			},
			assert: func(t *testing.T, wf *model.Workflow) {
				assert.DeepEqual(t, wf.GetJob("job9").Needs(), []string{"job1", "job2"})
			},
			want1:   "job9",
			wantErr: false,
		},
		{
			name: "no needs",
			args: args{
				task: &forge.Task{
					Workflow: []byte(`
name: Simple workflow
on: push

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - run: echo "hello"
`),
					Needs: nil,
				},
			},
			assert: func(t *testing.T, wf *model.Workflow) {
				job := wf.GetJob("test")
				assert.DeepEqual(t, job.Needs(), []string{})
				assert.Equal(t, len(job.Steps), 2)
			},
			want1:   "test",
			wantErr: false,
		},
		{
			name: "needs list",
			args: args{
				task: &forge.Task{
					Workflow: []byte(`
name: Workflow with list needs
on: push

jobs:
  deploy:
    needs: [build, test, lint]
    runs-on: ubuntu-latest
    steps:
      - run: echo "deploying"
`),
					Needs: []forge.Need{
						{Job: "build", Result: forge.Success},
						{
							Job:     "test",
							Outputs: []forge.Pair{{Name: "coverage", Value: "80%"}},
							Result:  forge.Success,
						},
						{Job: "lint", Result: forge.Failure},
					},
				},
			},
			assert: func(t *testing.T, wf *model.Workflow) {
				job := wf.GetJob("deploy")
				needs := job.Needs()
				assert.DeepEqual(t, needs, []string{"build", "lint", "test"})
				assert.Equal(t, wf.Jobs["test"].Outputs["coverage"], "80%")
				assert.Equal(t, wf.Jobs["lint"].Result, "failure")
			},
			want1:   "deploy",
			wantErr: false,
		},
		{
			name: "workflow env and defaults",
			args: args{
				task: &forge.Task{
					Workflow: []byte(`
name: Complex workflow
on:
  push:
    branches: [main, develop]
  pull_request:
    types: [opened, synchronize]

env:
  NODE_ENV: production
  CI: "true"

jobs:
  build:
    runs-on: ubuntu-latest
    env:
      BUILD_TYPE: release
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-node@v3
        with:
          node-version: "18"
      - run: npm ci
      - run: npm run build
`),
					Needs: nil,
				},
			},
			assert: func(t *testing.T, wf *model.Workflow) {
				assert.Equal(t, wf.Name, "Complex workflow")
				assert.Equal(t, wf.Env["NODE_ENV"], "production")
				assert.Equal(t, wf.Env["CI"], "true")
				job := wf.GetJob("build")
				assert.Equal(t, len(job.Steps), 4)
			},
			want1:   "build",
			wantErr: false,
		},
		{
			name: "job with container and services",
			args: args{
				task: &forge.Task{
					Workflow: []byte(`
name: Integration tests
on: push

jobs:
  integration:
    runs-on: ubuntu-latest
    container:
      image: node:18
    services:
      postgres:
        image: postgres:15
    steps:
      - uses: actions/checkout@v3
      - run: npm test
`),
					Needs: nil,
				},
			},
			assert: func(t *testing.T, wf *model.Workflow) {
				job := wf.GetJob("integration")
				container := job.Container()
				assert.Equal(t, container.Image, "node:18")
				assert.Equal(t, job.Services["postgres"].Image, "postgres:15")
			},
			want1:   "integration",
			wantErr: false,
		},
		{
			name: "job with matrix strategy",
			args: args{
				task: &forge.Task{
					Workflow: []byte(`
name: Matrix build
on: push

jobs:
  test:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        go-version: ["1.21", "1.22"]
    steps:
      - uses: actions/checkout@v3
      - run: go test ./...
`),
					Needs: nil,
				},
			},
			assert: func(t *testing.T, wf *model.Workflow) {
				job := wf.GetJob("test")
				matrixes, err := job.GetMatrixes()
				require.NoError(t, err)
				assert.Equal(t, len(matrixes), 2)
			},
			want1:   "test",
			wantErr: false,
		},
		{
			name: "special yaml characters in values",
			args: args{
				task: &forge.Task{
					Workflow: []byte("name: \"Special: characters & test\"\non: push\n\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: 'echo \"hello: world\"'\n      - run: 'echo \"quotes & ampersands\"'\n      - run: |\n          echo \"multiline\"\n          echo \"script\"\n"),
					Needs:    nil,
				},
			},
			assert: func(t *testing.T, wf *model.Workflow) {
				assert.Equal(t, wf.Name, "Special: characters & test")
				job := wf.GetJob("test")
				assert.Equal(t, len(job.Steps), 3)
			},
			want1:   "test",
			wantErr: false,
		},
		{
			name: "invalid yaml",
			args: args{
				task: &forge.Task{
					Workflow: []byte(`
name: Bad workflow
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
    - run: echo "ok"
     bad-indent: true
`),
					Needs: nil,
				},
			},
			assert:  nil,
			want1:   "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1, err := generateWorkflow(tt.args.task)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			tt.assert(t, got)
			assert.Equal(t, got1, tt.want1)
		})
	}
}

func Test_yamlV4NodeRoundTrip(t *testing.T) {
	t.Run("marshal sequence node", func(t *testing.T) {
		node := yaml.Node{
			Kind: yaml.SequenceNode,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "a"},
				{Kind: yaml.ScalarNode, Value: "b"},
				{Kind: yaml.ScalarNode, Value: "c"},
			},
		}

		out, err := yaml.Marshal(&node)
		require.NoError(t, err)
		assert.Equal(t, string(out), "- a\n- b\n- c\n")
	})

	t.Run("unmarshal and re-marshal workflow", func(t *testing.T) {
		input := []byte("name: test\non: push\njobs:\n    build:\n        runs-on: ubuntu-latest\n        steps:\n            - run: echo hello\n")

		var wf map[string]any
		err := yaml.Unmarshal(input, &wf)
		require.NoError(t, err)
		assert.Equal(t, wf["name"], "test")

		out, err := yaml.Marshal(wf)
		require.NoError(t, err)

		var wf2 map[string]any
		err = yaml.Unmarshal(out, &wf2)
		require.NoError(t, err)
		assert.Equal(t, wf2["name"], "test")
	})

	t.Run("node kind constants", func(t *testing.T) {
		// Verify yaml/v4 node kind constants are usable (same API as v3)
		require.NotEqual(t, yaml.ScalarNode, yaml.SequenceNode)
		require.NotEqual(t, yaml.SequenceNode, yaml.MappingNode)
	})
}
