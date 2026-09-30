// Copyright 2024 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package release

import (
	"testing"
)

func TestNew(t *testing.T) {
	cmd := New()
	if cmd == nil {
		t.Fatalf("New() returned nil")
	}
	if cmd.Use != "release" {
		t.Errorf("cmd.Use = %q, want %q", cmd.Use, "release")
	}
	meshnetCmd, _, err := cmd.Find([]string{"meshnet"})
	if err != nil {
		t.Fatalf("cmd.Find([\"meshnet\"]) failed: %v", err)
	}
	if meshnetCmd == nil {
		t.Fatalf("meshnet subcommand not found")
	}
	bridgeCmd, _, err := cmd.Find([]string{"bridge"})
	if err != nil {
		t.Fatalf("cmd.Find([\"bridge\"]) failed: %v", err)
	}
	if bridgeCmd == nil {
		t.Fatalf("bridge subcommand not found")
	}
}

func TestParseLsRemoteTagSHA(t *testing.T) {
	tests := []struct {
		desc   string
		output string
		tag    string
		want   string
	}{
		{
			desc:   "empty output",
			output: "",
			tag:    "v1.0.0",
			want:   "",
		},
		{
			desc:   "lightweight tag",
			output: "e51b7265984e34d3bb7965a24e331fee8d6949d1\trefs/tags/third_party/meshnet/v0.5.3\n",
			tag:    "third_party/meshnet/v0.5.3",
			want:   "e51b7265984e34d3bb7965a24e331fee8d6949d1",
		},
		{
			desc: "annotated tag with peeled ref",
			output: "1111111111111111111111111111111111111111\trefs/tags/v0.3.2\n" +
				"961d4e5c5120384b74dc431beac3107dc2ea8fdb\trefs/tags/v0.3.2^{}\n",
			tag:  "v0.3.2",
			want: "961d4e5c5120384b74dc431beac3107dc2ea8fdb",
		},
		{
			desc: "matches exact tag not prefix match",
			output: "aaaa111111111111111111111111111111111111\trefs/tags/v0.3.2-alpha\n" +
				"bbbb222222222222222222222222222222222222\trefs/tags/v0.3.2\n",
			tag:  "v0.3.2",
			want: "bbbb222222222222222222222222222222222222",
		},
		{
			desc:   "tag not in output",
			output: "aaaa111111111111111111111111111111111111\trefs/tags/v0.3.2-alpha\n",
			tag:    "v0.3.2",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := parseLsRemoteTagSHA(tt.output, tt.tag)
			if got != tt.want {
				t.Errorf("parseLsRemoteTagSHA() = %q, want %q", got, tt.want)
			}
		})
	}
}
