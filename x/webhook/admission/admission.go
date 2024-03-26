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
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/openconfig/kne/x/webhook/mutations/mutations"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	log "k8s.io/klog/v2"
)

// mutator is an entity capable of mutating pods. A mutation is any addition or removal from a
// Pod configuration. This could be adding containers or simply added envVars.
type mutator interface {
	MutatePod(pod *corev1.Pod) ([]byte, error)
}

// Admitter admits a pod into the review process.
type Admitter struct {
	request *admissionv1.AdmissionRequest
	mutator mutator
}

// New builds a new Admitter.
func New(request *admissionv1.AdmissionRequest) *Admitter {
	return &Admitter{
		request: request,
		mutator: mutations.New(),
	}
}

// Review filters for pod that should be mutated by this mutating webhook. Specifically, any pod who
// has the label `"webhook":"enabled"`, will be mutated by this webhook.
func (a Admitter) Review() (*admissionv1.AdmissionReview, error) {
	pod, err := toPod(a.request)
	if err != nil {
		return reviewResponse(a.request.UID, false, http.StatusBadRequest, err.Error()), err
	}

	labels := pod.GetLabels()
	if labels["webhook"] != "enabled" {
		log.Info("webhook mutations not requested for this container")
		return patchReviewResponse(a.request.UID, nil)
	}

	patch, err := a.mutator.MutatePod(pod)
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

// toPod extracts a pod from an admission request
func toPod(req *admissionv1.AdmissionRequest) (*corev1.Pod, error) {
	if req.Kind.Kind != "Pod" {
		return nil, fmt.Errorf("only pods are supported here")
	}

	p := corev1.Pod{}
	if err := json.Unmarshal(req.Object.Raw, &p); err != nil {
		return nil, err
	}

	return &p, nil
}
