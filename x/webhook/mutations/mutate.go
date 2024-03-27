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

// Package mutations contains individual mutation to apply to a pod config.
package mutations

import (
	"encoding/json"

	"github.com/wI2L/jsondiff"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	log "k8s.io/klog/v2"
)

type mutationFunc func(*corev1.Pod) (*corev1.Pod, error)

// Mutor contains a set of mutation functions.
type Mutor struct {
	mutationFuncs []mutationFunc
}

// New build a new Mutor
func New() *Mutor {
	return &Mutor{
		mutationFuncs: []mutationFunc{addContainer},
	}
}

// MutatePod applies all mutations to a copy of the provided pod. It returns a json patch
// (rfc6902).
func (m *Mutor) MutatePod(pod *corev1.Pod) ([]byte, error) {
	log.Infof("Mutating pod %s", pod.Name)

	// list of all mutations to be applied to the pod

	mpod := pod.DeepCopy()

	// apply all mutations
	for _, mutation := range m.mutationFuncs {
		var err error
		mpod, err = mutation(mpod)
		if err != nil {
			return nil, err
		}
	}

	mpod.Spec.AutomountServiceAccountToken = proto.Bool(false)

	// generate json patch
	patch, err := jsondiff.Compare(pod, mpod)
	if err != nil {
		return nil, err
	}

	patchb, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}

	return patchb, nil
}
