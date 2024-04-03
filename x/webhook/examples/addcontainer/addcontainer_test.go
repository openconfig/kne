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

package addcontainer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
)

func parsePod(t *testing.T, name string) *corev1.Pod {
	t.Helper()
	buf, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}

	p := &corev1.Pod{}
	if err := json.Unmarshal(buf, p); err != nil {
		t.Fatal(err)
	}

	return p
}

func TestAddContainer(t *testing.T) {
	tests := []struct {
		name    string
		inPod   *corev1.Pod
		wantPod *corev1.Pod
	}{
		{
			name:    "valid pod",
			inPod:   parsePod(t, "in.json"),
			wantPod: parsePod(t, "out.json"),
		},
		{
			name:    "valid pod - nomod",
			inPod:   parsePod(t, "in_nomod.json"),
			wantPod: parsePod(t, "out_nomod.json"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPod, err := AddContainer(tt.inPod)
			if err != nil {
				t.Fatalf("AddContainer() returned unexpected error: %v", err)
			}

			if diff := cmp.Diff(gotPod, tt.wantPod); diff != "" {
				t.Errorf("AddContainer() returned diff (-got, +want):\n%s", diff)
			}
		})
	}

}
