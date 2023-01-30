package jobparser

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/nektos/act/pkg/model"
)

func Parse(content []byte, options ...ParseOption) ([]*SingleWorkflow, error) {
	origin, err := model.ReadWorkflow(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("model.ReadWorkflow: %w", err)
	}

	workflow := &SingleWorkflow{}
	if err := yaml.Unmarshal(content, workflow); err != nil {
		return nil, fmt.Errorf("yaml.Unmarshal: %w", err)
	}

	pc := &parseContext{}
	for _, o := range options {
		o(pc)
	}
	results := map[string]*JobResult{}
	for id, job := range origin.Jobs {
		results[id] = &JobResult{
			Needs:   job.Needs(),
			Result:  pc.jobResults[id],
			Outputs: nil, // not supported yet
		}
	}

	var ret []*SingleWorkflow
	for id, job := range workflow.Jobs {
		for _, matrix := range getMatrixes(origin.GetJob(id)) {
			job := job.Clone()
			if job.Name == "" {
				job.Name = id
			}
			job.Name = nameWithMatrix(job.Name, matrix)
			job.Strategy.RawMatrix = encodeMatrix(matrix)
			evaluator := NewExpressionEvaluator(NewInterpeter(id, origin.GetJob(id), matrix, pc.gitContext, results))
			runsOn := origin.GetJob(id).RunsOn()
			for i, v := range runsOn {
				runsOn[i] = evaluator.Interpolate(v)
			}
			job.RawRunsOn = encodeRunsOn(runsOn)
			job.EraseNeeds() // there will be only one job in SingleWorkflow, it cannot have needs
			ret = append(ret, &SingleWorkflow{
				Name:     workflow.Name,
				RawOn:    workflow.RawOn,
				Env:      workflow.Env,
				Jobs:     map[string]*Job{id: job},
				Defaults: workflow.Defaults,
			})
		}
	}
	sortWorkflows(ret)
	return ret, nil
}

func WithJobResults(results map[string]string) ParseOption {
	return func(c *parseContext) {
		c.jobResults = results
	}
}

func WithGitContext(context *model.GithubContext) ParseOption {
	return func(c *parseContext) {
		c.gitContext = context
	}
}

type parseContext struct {
	jobResults map[string]string
	gitContext *model.GithubContext
}

type ParseOption func(c *parseContext)

func getMatrixes(job *model.Job) []map[string]interface{} {
	ret := job.GetMatrixes()
	sort.Slice(ret, func(i, j int) bool {
		return matrixName(ret[i]) < matrixName(ret[j])
	})
	return ret
}

func encodeMatrix(matrix map[string]interface{}) yaml.Node {
	if len(matrix) == 0 {
		return yaml.Node{}
	}
	value := map[string][]interface{}{}
	for k, v := range matrix {
		value[k] = []interface{}{v}
	}
	node := yaml.Node{}
	_ = node.Encode(value)
	return node
}

func encodeRunsOn(runsOn []string) yaml.Node {
	node := yaml.Node{}
	if len(runsOn) == 1 {
		_ = node.Encode(runsOn[0])
	} else {
		_ = node.Encode(runsOn)
	}
	return node
}

func nameWithMatrix(name string, m map[string]interface{}) string {
	if len(m) == 0 {
		return name
	}

	return name + " " + matrixName(m)
}

func matrixName(m map[string]interface{}) string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	vs := make([]string, 0, len(m))
	for _, v := range ks {
		vs = append(vs, fmt.Sprint(m[v]))
	}

	return fmt.Sprintf("(%s)", strings.Join(vs, ", "))
}

func sortWorkflows(wfs []*SingleWorkflow) {
	sort.Slice(wfs, func(i, j int) bool {
		ki := ""
		for k := range wfs[i].Jobs {
			ki = k
			break
		}
		kj := ""
		for k := range wfs[j].Jobs {
			kj = k
			break
		}
		return ki < kj
	})
}
