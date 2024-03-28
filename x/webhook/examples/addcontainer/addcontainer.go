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
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	log "k8s.io/klog/v2"
)

// AddContainer adds an alpine container to a pod.
func AddContainer(obj runtime.Object) (runtime.Object, error) {
	p, ok := obj.(*corev1.Pod)
	if !ok {
		log.Infof("Ignoring object of type %v, not a pod", obj.GetObjectKind())
		return obj, nil
	}

	if p.GetLabels()["webhook"] != "enabled" {
		log.Infof("Ignoring pod %q, mutation not requested", p.GetName())
		return obj, nil
	}

	mp := p.DeepCopy()
	mp.Spec.Containers = append(mp.Spec.Containers, corev1.Container{
		Name:            "alpine",
		Image:           "alpine:latest",
		Command:         []string{"/bin/sh", "-c", "sleep 2000000000000"},
		ImagePullPolicy: corev1.PullIfNotPresent,
		SecurityContext: &corev1.SecurityContext{
			Privileged: proto.Bool(true),
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
	})
	return mp, nil
}
