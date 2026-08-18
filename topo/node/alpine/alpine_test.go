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
package alpine

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/openconfig/gnmi/errdiff"
	apb "github.com/openconfig/kne/proto/alpine"
	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
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
				Command:      []string{"go", "run", "main.go"},
				EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", ""),
				Image:        "alpine:latest",
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
		desc: "provided alpine container",
		nImpl: &node.Impl{
			Proto: &tpb.Node{
				Config: &tpb.Config{
					Image:   "alpine:latest",
					Command: []string{"go", "run", "main.go"},
				},
			},
		},
		want: &tpb.Node{
			Config: &tpb.Config{
				Image:        "alpine:latest",
				Command:      []string{"go", "run", "main.go"},
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
					Image:   "alpine:latest",
					Command: []string{"go", "run", "main.go"},
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
				Image:        "alpine:latest",
				Command:      []string{"go", "run", "main.go"},
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
	vendorData, err := anypb.New(&apb.AlpineConfig{
		Containers: []*apb.Container{{
			Name:    "dp",
			Image:   "dpImage",
			Command: []string{"dpCommand"},
			Args:    []string{"dpArgs"},
		}},
		Files: &apb.Files{
			MountDir: "/files",
			Files: map[string]*apb.Files_FileData{
				"test.config": {FileData: &apb.Files_FileData_Data{
					Data: []byte{'t', 'e', 's', 't'},
				}},
			},
		},
	})
	if err != nil {
		t.Fatalf("cannot marshal AlpineConfig into \"any\" protobuf: %v", err)
	}
	tests := []struct {
		desc          string
		nImpl         *node.Impl
		wantAlpineCtr corev1.Container
		wantDpCtr     corev1.Container
		wantErr       string
	}{{
		desc: "get all containers",
		nImpl: &node.Impl{
			Proto: &tpb.Node{
				Name: "alpine",
				Config: &tpb.Config{
					Image:      "alpineImage",
					Command:    []string{"alpineCommand"},
					Args:       []string{"alpineArgs"},
					VendorData: vendorData,
				},
			},
		},
		wantAlpineCtr: corev1.Container{
			Name:    "alpine",
			Image:   "alpineImage",
			Command: []string{"alpineCommand"},
			Args:    []string{"alpineArgs"},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{}},
			ImagePullPolicy: "IfNotPresent",
			SecurityContext: &corev1.SecurityContext{
				Privileged: pointer.Bool(true),
			},
		},
		wantDpCtr: corev1.Container{
			Name:    "dp",
			Image:   "dpImage",
			Command: []string{"dpCommand"},
			Args:    []string{"dpArgs"},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{}},
			ImagePullPolicy: "IfNotPresent",
			SecurityContext: &corev1.SecurityContext{
				Privileged: pointer.Bool(true),
			},
			VolumeMounts: []corev1.VolumeMount{{Name: "files", MountPath: "/files"}},
		},
	}, {
		desc: "get all containers with ports",
		nImpl: &node.Impl{
			Proto: &tpb.Node{
				Name: "alpine",
				Config: &tpb.Config{
					Image:      "alpineImage",
					Command:    []string{"alpineCommand"},
					Args:       []string{"alpineArgs"},
					VendorData: vendorData,
					ConfigData: &tpb.Config_Data{Data: []byte{'h', 'i'}},
					ConfigPath: "/etc/sonic",
					ConfigFile: "config_db.json",
				},
				Interfaces: map[string]*tpb.Interface{
					"eth1": {
						IntName: "Ethernet1/1/1",
					},
				},
			},
		},
		wantAlpineCtr: corev1.Container{
			Name:    "alpine",
			Image:   "alpineImage",
			Command: []string{"alpineCommand"},
			Args:    []string{"alpineArgs", "--config_file=/etc/sonic/config_db.json"},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{}},
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
		wantDpCtr: corev1.Container{
			Name:    "dp",
			Image:   "dpImage",
			Command: []string{"dpCommand"},
			Args:    []string{"dpArgs", "--config_file=/etc/sonic/config_db.json"},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{}},
			ImagePullPolicy: "IfNotPresent",
			SecurityContext: &corev1.SecurityContext{
				Privileged: pointer.Bool(true),
			},
			VolumeMounts: []corev1.VolumeMount{{
				Name:      "files",
				MountPath: "/files",
			}, {
				Name:      "startup-config-volume",
				ReadOnly:  true,
				MountPath: "/etc/sonic/config_db.json",
				SubPath:   "config_db.json",
			}},
		},
	}, {
		desc: "get only alpine containers",
		nImpl: &node.Impl{
			Proto: &tpb.Node{
				Name: "alpine",
				Config: &tpb.Config{
					Image:   "alpineImage",
					Command: []string{"alpineCommand"},
					Args:    []string{"alpineArgs"},
				},
			},
		},
		wantAlpineCtr: corev1.Container{
			Name:    "alpine",
			Image:   "alpineImage",
			Command: []string{"alpineCommand"},
			Args:    []string{"alpineArgs"},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{}},
			ImagePullPolicy: "IfNotPresent",
			SecurityContext: &corev1.SecurityContext{
				Privileged: pointer.Bool(true),
			},
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
			pod, err := n.KubeClient.CoreV1().Pods(n.Namespace).Get(context.Background(), n.Name(), metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Could not get the pod: %v", err)
			}
			containers := pod.Spec.Containers
			if len(containers) < 1 || len(containers) > 2 {
				t.Fatalf("Num containers mismatch: want: 1 or 2 got:%v", len(containers))
			}
			alpineCtr := containers[0]
			if s := cmp.Diff(tt.wantAlpineCtr, alpineCtr); s != "" {
				t.Fatalf("Alpine Container mismatch: %s,\n got:\n%v \n want:\n%v\n", s, alpineCtr, tt.wantAlpineCtr)
			}
			if len(containers) == 2 {
				dpCtr := containers[1]
				if s := cmp.Diff(tt.wantDpCtr, dpCtr); s != "" {
					t.Fatalf("DP Container mismatch: %s,\n got:\n%v \n want:\n%v\n", s, dpCtr, tt.wantDpCtr)
				}
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
