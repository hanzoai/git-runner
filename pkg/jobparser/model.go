package jobparser

import (
	"fmt"

	"github.com/nektos/act/pkg/model"
	"gopkg.in/yaml.v3"
)

// SingleWorkflow is a workflow with single job and single matrix
type SingleWorkflow struct {
	Name     string            `yaml:"name,omitempty"`
	RawOn    yaml.Node         `yaml:"on,omitempty"`
	Env      map[string]string `yaml:"env,omitempty"`
	Jobs     map[string]*Job   `yaml:"jobs,omitempty"`
	Defaults Defaults          `yaml:"defaults,omitempty"`
}

func (w *SingleWorkflow) Job() (string, *Job) {
	for k, v := range w.Jobs {
		return k, v
	}
	return "", nil
}

func (w *SingleWorkflow) Marshal() ([]byte, error) {
	return yaml.Marshal(w)
}

type Job struct {
	Name           string                    `yaml:"name,omitempty"`
	RawNeeds       yaml.Node                 `yaml:"needs,omitempty"`
	RawRunsOn      yaml.Node                 `yaml:"runs-on,omitempty"`
	Env            yaml.Node                 `yaml:"env,omitempty"`
	If             yaml.Node                 `yaml:"if,omitempty"`
	Steps          []*Step                   `yaml:"steps,omitempty"`
	TimeoutMinutes string                    `yaml:"timeout-minutes,omitempty"`
	Services       map[string]*ContainerSpec `yaml:"services,omitempty"`
	Strategy       Strategy                  `yaml:"strategy,omitempty"`
	RawContainer   yaml.Node                 `yaml:"container,omitempty"`
	Defaults       Defaults                  `yaml:"defaults,omitempty"`
	Outputs        map[string]string         `yaml:"outputs,omitempty"`
	Uses           string                    `yaml:"uses,omitempty"`
}

func (j *Job) Clone() *Job {
	if j == nil {
		return nil
	}
	return &Job{
		Name:           j.Name,
		RawNeeds:       j.RawNeeds,
		RawRunsOn:      j.RawRunsOn,
		Env:            j.Env,
		If:             j.If,
		Steps:          j.Steps,
		TimeoutMinutes: j.TimeoutMinutes,
		Services:       j.Services,
		Strategy:       j.Strategy,
		RawContainer:   j.RawContainer,
		Defaults:       j.Defaults,
		Outputs:        j.Outputs,
		Uses:           j.Uses,
	}
}

func (j *Job) Needs() []string {
	return (&model.Job{RawNeeds: j.RawNeeds}).Needs()
}

func (j *Job) EraseNeeds() {
	j.RawNeeds = yaml.Node{}
}

func (j *Job) RunsOn() []string {
	return (&model.Job{RawRunsOn: j.RawRunsOn}).RunsOn()
}

type Step struct {
	ID               string            `yaml:"id,omitempty"`
	If               yaml.Node         `yaml:"if,omitempty"`
	Name             string            `yaml:"name,omitempty"`
	Uses             string            `yaml:"uses,omitempty"`
	Run              string            `yaml:"run,omitempty"`
	WorkingDirectory string            `yaml:"working-directory,omitempty"`
	Shell            string            `yaml:"shell,omitempty"`
	Env              yaml.Node         `yaml:"env,omitempty"`
	With             map[string]string `yaml:"with,omitempty"`
	ContinueOnError  bool              `yaml:"continue-on-error,omitempty"`
	TimeoutMinutes   string            `yaml:"timeout-minutes,omitempty"`
}

// String gets the name of step
func (s *Step) String() string {
	return (&model.Step{
		ID:   s.ID,
		Name: s.Name,
		Uses: s.Uses,
		Run:  s.Run,
	}).String()
}

type ContainerSpec struct {
	Image       string            `yaml:"image,omitempty"`
	Env         map[string]string `yaml:"env,omitempty"`
	Ports       []string          `yaml:"ports,omitempty"`
	Volumes     []string          `yaml:"volumes,omitempty"`
	Options     string            `yaml:"options,omitempty"`
	Credentials map[string]string `yaml:"credentials,omitempty"`
}

type Strategy struct {
	FailFastString    string    `yaml:"fail-fast,omitempty"`
	MaxParallelString string    `yaml:"max-parallel,omitempty"`
	RawMatrix         yaml.Node `yaml:"matrix,omitempty"`
}

type Defaults struct {
	Run RunDefaults `yaml:"run,omitempty"`
}

type RunDefaults struct {
	Shell            string `yaml:"shell,omitempty"`
	WorkingDirectory string `yaml:"working-directory,omitempty"`
}

type Event struct {
	Name      string
	acts      map[string][]string
	schedules []map[string]string
}

func (evt *Event) IsSchedule() bool {
	return evt.schedules != nil
}

func (evt *Event) Acts() map[string][]string {
	return evt.acts
}

func (evt *Event) Schedules() []map[string]string {
	return evt.schedules
}

func ParseRawOn(rawOn *yaml.Node) ([]*Event, error) {
	switch rawOn.Kind {
	case yaml.ScalarNode:
		var val string
		err := rawOn.Decode(&val)
		if err != nil {
			return nil, err
		}
		return []*Event{
			{Name: val},
		}, nil
	case yaml.SequenceNode:
		var val []interface{}
		err := rawOn.Decode(&val)
		if err != nil {
			return nil, err
		}
		res := make([]*Event, 0, len(val))
		for _, v := range val {
			switch t := v.(type) {
			case string:
				res = append(res, &Event{Name: t})
			default:
				return nil, fmt.Errorf("invalid type %T", t)
			}
		}
		return res, nil
	case yaml.MappingNode:
		var val map[string]interface{}
		err := rawOn.Decode(&val)
		if err != nil {
			return nil, err
		}
		res := make([]*Event, 0, len(val))
		for k, v := range val {
			if v == nil {
				res = append(res, &Event{
					Name: k,
					Acts: map[string][]string{},
				})
				continue
			}
			switch t := v.(type) {
			case string:
				res = append(res, &Event{
					Name: k,
					acts: map[string][]string{},
				})
			case []string:
				res = append(res, &Event{
					Name: k,
					acts: map[string][]string{},
				})
			case map[string]interface{}:
				acts := make(map[string][]string, len(t))
				for act, branches := range t {
					switch b := branches.(type) {
					case string:
						acts[act] = []string{b}
					case []string:
						acts[act] = b
					case []interface{}:
						acts[act] = make([]string, len(b))
						for i, v := range b {
							var ok bool
							if acts[act][i], ok = v.(string); !ok {
								return nil, fmt.Errorf("unknown on type: %#v", branches)
							}
						}
					default:
						return nil, fmt.Errorf("unknown on type: %#v", branches)
					}
				}
				res = append(res, &Event{
					Name: k,
					acts: acts,
				})
			case []interface{}:
				if k != "schedule" {
					return nil, fmt.Errorf("unknown on type: %#v", v)
				}
				schedules := make([]map[string]string, len(t))
				for i, tt := range t {
					vv, ok := tt.(map[string]interface{})
					if !ok {
						return nil, fmt.Errorf("unknown on type: %#v", v)
					}
					schedules[i] = make(map[string]string, len(vv))
					for k, vvv := range vv {
						var ok bool
						if schedules[i][k], ok = vvv.(string); !ok {
							return nil, fmt.Errorf("unknown on type: %#v", v)
						}
					}
				}
				res = append(res, &Event{
					Name:      k,
					schedules: schedules,
				})
			default:
				return nil, fmt.Errorf("unknown on type: %#v", v)
			}
		}
		return res, nil
	default:
		return nil, fmt.Errorf("unknown on type: %v", rawOn.Kind)
	}
}
