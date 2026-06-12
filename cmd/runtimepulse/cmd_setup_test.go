package main

import (
	"reflect"
	"testing"
)

var validAgents = []string{"claude", "cursor", "codex", "opencode"}

func TestParseAgentFlag(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    []string
		wantErr bool
	}{
		{"single", "claude", []string{"claude"}, false},
		{"list", "claude,opencode", []string{"claude", "opencode"}, false},
		{"whitespace", " claude , opencode ", []string{"claude", "opencode"}, false},
		{"unknown", "claude,nope", nil, true},
		{"empty token", "claude,,opencode", nil, true},
		{"duplicate collapsed", "claude,claude", []string{"claude"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAgentFlag(tt.value, validAgents)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// parseSelection contract: ("", cands) -> all candidates; "q"/"n" -> (nil, nil)
// meaning quit; otherwise comma-separated 1-based numbers and/or names.
func TestParseSelection(t *testing.T) {
	cands := []string{"claude", "cursor", "opencode"}
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{"empty = all", "", []string{"claude", "cursor", "opencode"}, false},
		{"whitespace empty = all", "  ", []string{"claude", "cursor", "opencode"}, false},
		{"quit q", "q", nil, false},
		{"quit n", "n", nil, false},
		{"single number", "2", []string{"cursor"}, false},
		{"number list", "1,3", []string{"claude", "opencode"}, false},
		{"names", "claude,opencode", []string{"claude", "opencode"}, false},
		{"mixed", "1, opencode", []string{"claude", "opencode"}, false},
		{"whitespace tolerant", " 1 , 3 ", []string{"claude", "opencode"}, false},
		{"dedupe", "1,claude", []string{"claude"}, false},
		{"out of range", "4", nil, true},
		{"zero", "0", nil, true},
		{"unknown name", "vim", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSelection(tt.input, cands)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
