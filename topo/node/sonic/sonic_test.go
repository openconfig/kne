// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package sonic

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/openconfig/gnmi/errdiff"
	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/pointer"
)

func TestNew(t *testing.T) {
	tests := []struct {
		desc    string
		nImpl   *node.Impl
		want    *tpb.Node
		wantErr string
	}{{
		desc:    "nil impl",
		wantErr: "nodeImpl cannot be nil",
	}, {
		desc:    "nil pb",
		wantErr: "nodeImpl.Proto cannot be nil",
		nImpl:   &node.Impl{},
	}, {
		desc: "empty pb",
		nImpl: &node.Impl{
			Proto: &tpb.Node{},
		},
		want: &tpb.Node{
			Config: &tpb.Config{
				Command:      []string{"bash"},
				EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", ""),
				Image:        "sonic:latest",
			},
			Services: map[uint32]*tpb.Service{
				22: {
					Name:   "ssh",
					Inside: 22,
				},
			},
			Constraints: map[string]string{
				"cpu":    "500m",
				"memory": "1Gi",
			},
		},
	}, {
		desc: "provided sonic container",
		nImpl: &node.Impl{
			Proto: &tpb.Node{
				Config: &tpb.Config{
					Image:   "sonic:custom",
					Command: []string{"sh"},
				},
			},
		},
		want: &tpb.Node{
			Config: &tpb.Config{
				Image:        "sonic:custom",
				Command:      []string{"sh"},
				EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", ""),
			},
			Services: map[uint32]*tpb.Service{
				22: {
					Name:   "ssh",
					Inside: 22,
				},
			},
			Constraints: map[string]string{
				"cpu":    "500m",
				"memory": "1Gi",
			},
		},
	}, {
		desc: "with interfaces",
		nImpl: &node.Impl{
			Proto: &tpb.Node{
				Config: &tpb.Config{
					Image:   "sonic:latest",
					Command: []string{"bash"},
				},
				Interfaces: map[string]*tpb.Interface{
					"Ethernet1/1/1": {
						IntName:     "Ethernet1/1/1",
						PeerName:    "peer",
						PeerIntName: "Ethernet2/2/2",
					},
				},
			},
		},
		want: &tpb.Node{
			Config: &tpb.Config{
				Image:        "sonic:latest",
				Command:      []string{"bash"},
				EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", ""),
			},
			Interfaces: map[string]*tpb.Interface{
				"eth1": {
					IntName:     "Ethernet1/1/1",
					PeerName:    "peer",
					PeerIntName: "Ethernet2/2/2",
				},
			},
			Services: map[uint32]*tpb.Service{
				22: {
					Name:   "ssh",
					Inside: 22,
				},
			},
			Constraints: map[string]string{
				"cpu":    "500m",
				"memory": "1Gi",
			},
		},
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			n, err := New(tt.nImpl)
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: got %v, want %s", err, s)
			}
			if tt.wantErr != "" {
				return
			}
			if !proto.Equal(n.GetProto(), tt.want) {
				t.Fatalf("New() failed: got\n%swant\n%s", prototext.Format(n.GetProto()), prototext.Format(tt.want))
			}
		})
	}
}

func TestCreatePod(t *testing.T) {
	tests := []struct {
		desc         string
		nImpl        *node.Impl
		wantInitCtr  corev1.Container
		wantSonicCtr corev1.Container
		wantErr      string
	}{{
		desc: "simple sonic container",
		nImpl: &node.Impl{
			Proto: &tpb.Node{
				Name: "sonic-node",
				Config: &tpb.Config{
					Image:   "sonicImage",
					Command: []string{"sonicCommand"},
					Args:    []string{"sonicArgs"},
					Sleep:   10,
				},
			},
		},
		wantInitCtr: corev1.Container{
			Name:  "init-sonic-node",
			Image: node.DefaultInitContainerImage,
			Args:  []string{"1", "10", "1"},
			ImagePullPolicy: "IfNotPresent",
			SecurityContext: &corev1.SecurityContext{
				Privileged: pointer.Bool(true),
			},
		},
		wantSonicCtr: corev1.Container{
			Name:    "sonic-node",
			Image:   "sonicImage",
			Command: []string{"sonicCommand"},
			Args:    []string{"sonicArgs"},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{},
			},
			ImagePullPolicy: "IfNotPresent",
			SecurityContext: &corev1.SecurityContext{
				Privileged: pointer.Bool(true),
			},
		},
	}, {
		desc: "sonic container with custom init image and interfaces",
		nImpl: &node.Impl{
			Proto: &tpb.Node{
				Name: "sonic-node",
				Config: &tpb.Config{
					InitImage: "customInitImage",
					Image:     "sonicImage",
					Command:   []string{"sonicCommand"},
					Args:      []string{"sonicArgs"},
					Sleep:     5,
				},
				Interfaces: map[string]*tpb.Interface{
					"eth1": {
						IntName: "Ethernet1/1/1",
					},
					"eth2": {
						IntName: "Ethernet1/1/2",
					},
				},
			},
		},
		wantInitCtr: corev1.Container{
			Name:  "init-sonic-node",
			Image: "customInitImage",
			Args:  []string{"3", "5", "1"},
			ImagePullPolicy: "IfNotPresent",
			SecurityContext: &corev1.SecurityContext{
				Privileged: pointer.Bool(true),
			},
		},
		wantSonicCtr: corev1.Container{
			Name:    "sonic-node",
			Image:   "sonicImage",
			Command: []string{"sonicCommand"},
			Args:    []string{"sonicArgs"},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{},
			},
			ImagePullPolicy: "IfNotPresent",
			SecurityContext: &corev1.SecurityContext{
				Privileged: pointer.Bool(true),
			},
		},
	}, {
		desc: "sonic container with config data",
		nImpl: &node.Impl{
			Proto: &tpb.Node{
				Name: "sonic-node",
				Config: &tpb.Config{
					Image:      "sonicImage",
					Command:    []string{"sonicCommand"},
					Args:       []string{"sonicArgs"},
					ConfigData: &tpb.Config_Data{Data: []byte{'h', 'i'}},
					ConfigPath: "/etc/sonic",
					ConfigFile: "config_db.json",
					Sleep:      10,
				},
			},
		},
		wantInitCtr: corev1.Container{
			Name:  "init-sonic-node",
			Image: node.DefaultInitContainerImage,
			Args:  []string{"1", "10", "1"},
			ImagePullPolicy: "IfNotPresent",
			SecurityContext: &corev1.SecurityContext{
				Privileged: pointer.Bool(true),
			},
		},
		wantSonicCtr: corev1.Container{
			Name:    "sonic-node",
			Image:   "sonicImage",
			Command: []string{"sonicCommand"},
			Args:    []string{"sonicArgs", "--config_file=/etc/sonic/config_db.json"},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{},
			},
			ImagePullPolicy: "IfNotPresent",
			SecurityContext: &corev1.SecurityContext{
				Privileged: pointer.Bool(true),
			},
			VolumeMounts: []corev1.VolumeMount{{
				Name:      "startup-config-volume",
				ReadOnly:  true,
				MountPath: "/etc/sonic/config_db.json",
				SubPath:   "config_db.json",
			}},
		},
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			n := &Node{
				Impl: &node.Impl{
					Namespace:  "test",
					KubeClient: kfake.NewSimpleClientset(),
					Proto:      tt.nImpl.Proto,
				},
			}
			err := n.CreatePod(context.Background())
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: got %v, want %s", err, s)
			}
			if tt.wantErr != "" {
				return
			}
			pod, err := n.KubeClient.CoreV1().Pods(n.Namespace).Get(context.Background(), n.Name(), metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Could not get the pod: %v", err)
			}
			initContainers := pod.Spec.InitContainers
			if len(initContainers) != 1 {
				t.Fatalf("Num init containers mismatch: want: 1 got: %v", len(initContainers))
			}
			if s := cmp.Diff(tt.wantInitCtr, initContainers[0]); s != "" {
				t.Fatalf("Init Container mismatch: %s,\n got:\n%v \n want:\n%v\n", s, initContainers[0], tt.wantInitCtr)
			}

			containers := pod.Spec.Containers
			if len(containers) != 1 {
				t.Fatalf("Num containers mismatch: want: 1 got: %v", len(containers))
			}
			if s := cmp.Diff(tt.wantSonicCtr, containers[0]); s != "" {
				t.Fatalf("Sonic Container mismatch: %s,\n got:\n%v \n want:\n%v\n", s, containers[0], tt.wantSonicCtr)
			}
		})
	}
}

func TestDefaultNodeConstraints(t *testing.T) {
	n := &Node{}
	constraints := n.DefaultNodeConstraints()
	if constraints.CPU != defaultConstraints.CPU {
		t.Errorf("DefaultNodeConstraints() returned unexpected CPU: got %s, want %s", constraints.CPU, defaultConstraints.CPU)
	}

	if constraints.Memory != defaultConstraints.Memory {
		t.Errorf("DefaultNodeConstraints() returned unexpected Memory: got %s, want %s", constraints.Memory, defaultConstraints.Memory)
	}
}
