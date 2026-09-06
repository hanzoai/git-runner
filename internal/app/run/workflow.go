// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package run

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/hanzoai/git-runner/act/model"

	forge "github.com/hanzoai/git/modules/actions/runner"
	"go.yaml.in/yaml/v4"
)

func generateWorkflow(task *forge.Task) (*model.Workflow, string, error) {
	workflow, err := model.ReadWorkflow(bytes.NewReader(task.Workflow))
	if err != nil {
		return nil, "", err
	}

	jobIDs := workflow.GetJobIDs()
	if len(jobIDs) != 1 {
		return nil, "", fmt.Errorf("multiple jobs found: %v", jobIDs)
	}
	jobID := jobIDs[0]

	needJobIDs := make([]string, 0, len(task.Needs))
	for _, need := range task.Needs {
		needJobIDs = append(needJobIDs, need.Job)
		workflow.Jobs[need.Job] = &model.Job{
			Outputs: index(need.Outputs),
			Result:  string(need.Result),
		}
	}
	sort.Strings(needJobIDs)

	rawNeeds := yaml.Node{
		Kind:    yaml.SequenceNode,
		Content: make([]*yaml.Node, 0, len(needJobIDs)),
	}
	for _, id := range needJobIDs {
		rawNeeds.Content = append(rawNeeds.Content, &yaml.Node{
			Kind:  yaml.ScalarNode,
			Value: id,
		})
	}

	workflow.Jobs[jobID].RawNeeds = rawNeeds

	return workflow, jobID, nil
}
