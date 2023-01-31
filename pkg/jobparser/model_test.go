package jobparser

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nektos/act/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestParseRawOn(t *testing.T) {
	kases := []struct {
		input  string
		result []*Event
	}{
		{
			input: "on: issue_comment",
			result: []*Event{
				{
					Name: "issue_comment",
				},
			},
		},
		{
			input: "on:\n  push",
			result: []*Event{
				{
					Name: "push",
				},
			},
		},

		{
			input: "on:\n  - push\n  - pull_request",
			result: []*Event{
				{
					Name: "push",
				},
				{
					Name: "pull_request",
				},
			},
		},
		{
			input: "on:\n  push:\n    branches:\n      - master",
			result: []*Event{
				{
					Name: "push",
					Acts: map[string][]string{
						"branches": {
							"master",
						},
					},
				},
			},
		},
		{
			input: "on:\n  branch_protection_rule:\n    types: [created, deleted]",
			result: []*Event{
				{
					Name: "branch_protection_rule",
					Acts: map[string][]string{
						"types": {
							"created",
							"deleted",
						},
					},
				},
			},
		},
		{
			input: "on:\n  project:\n    types: [created, deleted]\n  milestone:\n    types: [opened, deleted]",
			result: []*Event{
				{
					Name: "project",
					Acts: map[string][]string{
						"types": {
							"created",
							"deleted",
						},
					},
				},
				{
					Name: "milestone",
					Acts: map[string][]string{
						"types": {
							"opened",
							"deleted",
						},
					},
				},
			},
		},
		{
			input: "on:\n  pull_request:\n    types:\n      - opened\n    branches:\n      - 'releases/**'",
			result: []*Event{
				{
					Name: "pull_request",
					Acts: map[string][]string{
						"types": {
							"opened",
						},
						"branches": {
							"releases/**",
						},
					},
				},
			},
		},
		{
			input: "on:\n  push:\n    branches:\n      - main\n  pull_request:\n    types:\n      - opened\n    branches:\n      - '**'",
			result: []*Event{
				{
					Name: "push",
					Acts: map[string][]string{
						"branches": {
							"main",
						},
					},
				},
				{
					Name: "pull_request",
					Acts: map[string][]string{
						"types": {
							"opened",
						},
						"branches": {
							"**",
						},
					},
				},
			},
		},
		{
			input: "on:\n  push:\n    branches:\n      - 'main'\n      - 'releases/**'",
			result: []*Event{
				{
					Name: "push",
					Acts: map[string][]string{
						"branches": {
							"main",
							"releases/**",
						},
					},
				},
			},
		},
		{
			input: "on:\n  push:\n    tags:\n      - v1.**",
			result: []*Event{
				{
					Name: "push",
					Acts: map[string][]string{
						"tags": {
							"v1.**",
						},
					},
				},
			},
		},
		{
			input: "on: [pull_request, workflow_dispatch]",
			result: []*Event{
				{
					Name: "pull_request",
				},
				{
					Name: "workflow_dispatch",
				},
			},
		},
	}
	for _, kase := range kases {
		t.Run(kase.input, func(t *testing.T) {
			origin, err := model.ReadWorkflow(strings.NewReader(kase.input))
			assert.NoError(t, err)

			events, err := ParseRawOn(&origin.RawOn)
			assert.NoError(t, err)
			assert.EqualValues(t, kase.result, events, fmt.Sprintf("%#v", events))
		})
	}
}
