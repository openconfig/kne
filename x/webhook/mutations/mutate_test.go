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

package mutations

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func fakeMutation(p *corev1.Pod) (*corev1.Pod, error) {
	if p.Labels == nil {
		p.Labels = make(map[string]string)
	}
	p.Labels["fake"] = "fake"

	return p, nil
}

func TestMutatePod(t *testing.T) {
	tests := []struct {
		name      string
		inPod     *corev1.Pod
		wantPatch []byte
	}{
		{
			name: "basic-pod",
			inPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "basic-pod",
				},
			},
			wantPatch: []byte(`[{"value":{"fake":"fake"},"op":"add","path":"/metadata/labels"},{"value":false,"op":"add","path":"/spec/automountServiceAccountToken"}]`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Mutor{
				mutationFuncs: []mutationFunc{fakeMutation},
			}

			patch, err := m.MutatePod(tt.inPod)
			if err != nil {
				t.Fatalf("MutatePod returned error: %v", err)
			}

			if diff := cmp.Diff(patch, tt.wantPatch); diff != "" {
				t.Errorf("MutatePod returned diff (-got, +want):\n%s", diff)
			}
		})
	}
}
