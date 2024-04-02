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

// Package admission handles kubernetes admissions.
package admission

import (
	"net/http"

	"github.com/openconfig/kne/x/webhook/mutate"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
)

// mutator is an entity capable of mutating pods. A mutation is any addition or removal from a
// Pod configuration. This could be adding containers or simply added envVars.
type mutator interface {
	MutateObject(runtime.Object) ([]byte, error)
}

// Admitter admits a pod into the review process.
type Admitter struct {
	request *admissionv1.AdmissionRequest
	mutator mutator
}

// New builds a new Admitter.
func New(request *admissionv1.AdmissionRequest, mutations []mutate.MutationFunc) *Admitter {
	return &Admitter{
		request: request,
		mutator: mutate.New(mutations),
	}
}

// Review filters for resources that should be mutated by this mutating webhook. Specifically, any resource who
// has the label `"webhook":"enabled"`, will be mutated by this webhook.
func (a Admitter) Review() (*admissionv1.AdmissionReview, error) {
	obj, err := runtime.Decode(scheme.Codecs.UniversalDeserializer(), a.request.Object.Raw)
	if err != nil {
		return reviewResponse(a.request.UID, false, http.StatusBadRequest, err.Error()), err
	}
	patch, err := a.mutator.MutateObject(obj)
	if err != nil {
		return reviewResponse(a.request.UID, false, http.StatusBadRequest, err.Error()), err
	}
	return patchReviewResponse(a.request.UID, patch)
}

// reviewResponse constructs a valid response for the k8s server.
func reviewResponse(uid types.UID, allowed bool, httpCode int32, reason string) *admissionv1.AdmissionReview {
	return &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},
		Response: &admissionv1.AdmissionResponse{
			UID:     uid,
			Allowed: allowed,
			Result: &metav1.Status{
				Code:    httpCode,
				Message: reason,
			},
		},
	}
}

// patchReviewResponse builds an admission review with given json patch
func patchReviewResponse(uid types.UID, patch []byte) (*admissionv1.AdmissionReview, error) {
	patchType := admissionv1.PatchTypeJSONPatch
	resp := &admissionv1.AdmissionResponse{
		UID:     uid,
		Allowed: true,
	}

	if patch != nil {
		resp.PatchType = &patchType
		resp.Patch = patch
	}

	return &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},
		Response: resp,
	}, nil
}
