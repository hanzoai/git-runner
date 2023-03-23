package runtime

import "testing"

func TestParseLabel(t *testing.T) {
	tests := []struct {
		args       string
		wantLabel  string
		wantSchema string
		wantArg    string
		wantErr    bool
	}{
		{
			args:       "ubuntu:docker://node:18",
			wantLabel:  "ubuntu",
			wantSchema: "docker",
			wantArg:    "//node:18",
			wantErr:    false,
		},
		{
			args:       "ubuntu:host",
			wantLabel:  "ubuntu",
			wantSchema: "host",
			wantArg:    "",
			wantErr:    false,
		},
		{
			args:       "ubuntu",
			wantLabel:  "ubuntu",
			wantSchema: "host",
			wantArg:    "",
			wantErr:    false,
		},
		{
			args:       "ubuntu:vm:ubuntu-18.04",
			wantLabel:  "",
			wantSchema: "",
			wantArg:    "",
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.args, func(t *testing.T) {
			gotLabel, gotSchema, gotArg, err := ParseLabel(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseLabel() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotLabel != tt.wantLabel {
				t.Errorf("parseLabel() gotLabel = %v, want %v", gotLabel, tt.wantLabel)
			}
			if gotSchema != tt.wantSchema {
				t.Errorf("parseLabel() gotSchema = %v, want %v", gotSchema, tt.wantSchema)
			}
			if gotArg != tt.wantArg {
				t.Errorf("parseLabel() gotArg = %v, want %v", gotArg, tt.wantArg)
			}
		})
	}
}
