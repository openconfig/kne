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
	"fmt"

	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// Default values for cpu and memory allocations for added containers.
const (
	// TODO: make these values configurable.
	mem = "1Gi"
	cpu = "500m"
)

var (
	image   = "alpine:latest"
	command = []string{"/bin/sh", "-c", "sleep 2000000000000"}
)

// add adds an alpine container to a pod.
func addContainer(pod *corev1.Pod) (*corev1.Pod, error) {
	mpod := pod.DeepCopy()
	// kne only supports supplying one container
	if len(mpod.Spec.Containers) != 1 {
		return nil, fmt.Errorf("pod %s has %d containers", mpod.Name, len(mpod.Spec.Containers))
	}

	cpuQuantity, err := resource.ParseQuantity(cpu)
	if err != nil {
		return nil, err
	}

	memoryQuantity, err := resource.ParseQuantity(mem)
	if err != nil {
		return nil, err
	}

	mpod.Spec.Containers = append(mpod.Spec.Containers, corev1.Container{
		Name:            "alpine",
		Image:           image,
		Command:         command,
		ImagePullPolicy: corev1.PullIfNotPresent,
		SecurityContext: &corev1.SecurityContext{
			Privileged: proto.Bool(true),
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    cpuQuantity,
				corev1.ResourceMemory: memoryQuantity,
			},
		},
	})

	return mpod, nil
}
