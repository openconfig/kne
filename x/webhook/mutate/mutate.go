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

// Package mutate contains a struct for performing mutations to K8s objects.
package mutate

import (
	"encoding/json"

	"github.com/wI2L/jsondiff"
	"k8s.io/apimachinery/pkg/runtime"
	log "k8s.io/klog/v2"
)

type MutationFunc func(runtime.Object) (runtime.Object, error)

// Mutor contains a set of mutation functions.
type Mutor struct {
	mutationFuncs []MutationFunc
}

// New build a new Mutor
func New(m []MutationFunc) *Mutor {
	return &Mutor{mutationFuncs: m}
}

// MutatePod applies all mutations to a copy of the provided object.
// It returns a json patch (rfc6902).
func (m *Mutor) MutateObject(obj runtime.Object) ([]byte, error) {
	log.Infof("Mutating %s", obj.GetObjectKind())

	cObj := obj.DeepCopyObject()

	// apply all mutations
	for _, mutation := range m.mutationFuncs {
		var err error
		cObj, err = mutation(cObj)
		if err != nil {
			return nil, err
		}
	}

	// mpod.Spec.AutomountServiceAccountToken = proto.Bool(false)

	// generate json patch
	patch, err := jsondiff.Compare(obj, cObj)
	if err != nil {
		return nil, err
	}
	return json.Marshal(patch)
}
