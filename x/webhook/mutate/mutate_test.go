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

package mutate

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/openconfig/gnmi/errdiff"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	log "k8s.io/klog/v2"
)

func fakeMutation(obj runtime.Object) (runtime.Object, error) {
	p, ok := obj.(*corev1.Pod)
	if !ok {
		log.Infof("Ignoring object of type %v, not a pod", obj.GetObjectKind())
		return obj, nil
	}
	if p.Name == "bad-pod" {
		return nil, fmt.Errorf("cannot mutate bad-pod")
	}
	if p.Labels == nil {
		p.Labels = make(map[string]string)
	}
	p.Labels["fake"] = "fake"

	return p, nil
}

func TestMutatePod(t *testing.T) {
	tests := []struct {
		name      string
		in        runtime.Object
		wantErr   string
		wantPatch []byte
	}{
		{
			name: "basic-pod",
			in: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "basic-pod",
				},
			},
			wantPatch: []byte(`[{"value":{"fake":"fake"},"op":"add","path":"/metadata/labels"}]`),
		},
		{
			name: "bad-pod",
			in: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "bad-pod",
				},
			},
			wantErr: "cannot mutate",
		},
		{
			name: "not a pod",
			in: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "basic-ns",
				},
			},
			wantPatch: []byte("null"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New([]MutationFunc{fakeMutation})

			patch, err := m.MutateObject(tt.in)
			if s := errdiff.Check(err, tt.wantErr); s != "" {
				t.Errorf("MutateObject() failed: %s", s)
			}

			if diff := cmp.Diff(patch, tt.wantPatch); diff != "" {
				t.Errorf("MutateObject returned diff (-got, +want):\n%s", diff)
			}
		})
	}
}
