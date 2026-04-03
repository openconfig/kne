// Copyright 2025 Ciena Corporation
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
package ciena

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	cpb "github.com/openconfig/kne/proto/ciena"
	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kfake "k8s.io/client-go/kubernetes/fake"
)

func TestNew(t *testing.T) {
	tests := []struct {
		desc    string
		nImpl   *node.Impl
		want    *tpb.Node
		wantErr string
	}{
		{
			desc:    "nil nodeImpl",
			nImpl:   nil,
			wantErr: "nodeImpl cannot be nil",
		},
		{
			desc:    "nil nodeImpl.Proto",
			nImpl:   &node.Impl{},
			wantErr: "nodeImpl.Proto cannot be nil",
		},
		{
			desc: "empty proto, expect defaults",
			nImpl: &node.Impl{
				Proto: &tpb.Node{},
			},
			want: &tpb.Node{
				// Name:  "default_waverouter_node",
				Model: "5132",
				Os:    "saos",
				Labels: map[string]string{
					"vendor":       tpb.Vendor_CIENA.String(),
					"ondatra-role": "DUT",
					"model":        "5132",
					"os":           "saos",
				},
				Config: &tpb.Config{
					ConfigPath: "config/",
					ConfigFile: "config.cfg",
					Image:      "artifactory.ciena.com/psa/saos-containerlab:latest",
					Env: map[string]string{
						"CLAB_LABEL_CLAB_NODE_NAME": "",
						"CLAB_LABEL_CLAB_NODE_TYPE": "5132",
					},
				},
				Services: map[uint32]*tpb.Service{
					22:    {Names: []string{"ssh"}, Inside: 22},
					179:   {Names: []string{"bgp"}, Inside: 179},
					225:   {Names: []string{"debug"}, Inside: 225},
					443:   {Names: []string{"ssl"}, Inside: 443},
					830:   {Names: []string{"netconf"}, Inside: 830},
					4243:  {Names: []string{"docker-daemon"}, Inside: 4243},
					9339:  {Names: []string{"gnmi"}, Inside: 9339},
					9340:  {Names: []string{"gribi"}, Inside: 9340},
					9559:  {Names: []string{"p4rt"}, Inside: 9559},
					10161: {Names: []string{"gnmi-gnoi-alternate"}, Inside: 10161},
				},
				Interfaces: map[string]*tpb.Interface{},
			},
		},
		{
			desc: "proto with interfaces, expect rename",
			nImpl: &node.Impl{
				Proto: &tpb.Node{
					Interfaces: map[string]*tpb.Interface{
						"1/2/3": {IntName: "1/2/3"},
						"5":     {IntName: "5"},
						"eth7":  {IntName: "eth7"},
						"mgmt":  {IntName: "mgmt"},
					},
				},
			},
			want: &tpb.Node{
				// Name:  "default_waverouter_node",
				Model: "5132",
				Os:    "saos",
				Labels: map[string]string{
					"vendor":       tpb.Vendor_CIENA.String(),
					"ondatra-role": "DUT",
					"model":        "5132",
					"os":           "saos",
				},
				Config: &tpb.Config{
					ConfigPath: "config/",
					ConfigFile: "config.cfg",
					Image:      "artifactory.ciena.com/psa/saos-containerlab:latest",
					Env: map[string]string{
						"CLAB_LABEL_CLAB_NODE_NAME": "",
						"CLAB_LABEL_CLAB_NODE_TYPE": "5132",
					},
				},
				Services: map[uint32]*tpb.Service{
					22:    {Names: []string{"ssh"}, Inside: 22},
					179:   {Names: []string{"bgp"}, Inside: 179},
					225:   {Names: []string{"debug"}, Inside: 225},
					443:   {Names: []string{"ssl"}, Inside: 443},
					830:   {Names: []string{"netconf"}, Inside: 830},
					4243:  {Names: []string{"docker-daemon"}, Inside: 4243},
					9339:  {Names: []string{"gnmi"}, Inside: 9339},
					9340:  {Names: []string{"gribi"}, Inside: 9340},
					9559:  {Names: []string{"p4rt"}, Inside: 9559},
					10161: {Names: []string{"gnmi-gnoi-alternate"}, Inside: 10161},
				},
				Interfaces: map[string]*tpb.Interface{
					"eth3": {IntName: "1/2/3"},
					"eth5": {IntName: "5"},
					"eth7": {IntName: "eth7"},
					"mgmt": {IntName: "mgmt"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			n, err := New(tt.nImpl)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("got err %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := n.GetProto()
			if !proto.Equal(got, tt.want) {
				t.Errorf("proto mismatch (-want +got):\nwant: %v\ngot: %v", tt.want, got)
			}
		})
	}
}
func TestRenameInterfaces(t *testing.T) {
	tests := []struct {
		desc       string
		input      map[string]*tpb.Interface
		want       map[string]*tpb.Interface
	}{
		{
			desc:  "empty interfaces",
			input: map[string]*tpb.Interface{},
			want:  map[string]*tpb.Interface{},
		},
		{
			desc: "single full format",
			input: map[string]*tpb.Interface{
				"1/5/2": {IntName: "1/5/2"},
			},
			want: map[string]*tpb.Interface{
				"eth2": {IntName: "1/5/2"},
			},
		},
		{
			desc: "single digit format",
			input: map[string]*tpb.Interface{
				"3": {IntName: "3"},
			},
			want: map[string]*tpb.Interface{
				"eth3": {IntName: "3"},
			},
		},
		{
			desc: "already eth format",
			input: map[string]*tpb.Interface{
				"eth1": {IntName: "eth1"},
			},
			want: map[string]*tpb.Interface{
				"eth1": {IntName: "eth1"},
			},
		},
		{
			desc: "mgmt interface",
			input: map[string]*tpb.Interface{
				"mgmt": {IntName: "mgmt"},
			},
			want: map[string]*tpb.Interface{
				"mgmt": {IntName: "mgmt"},
			},
		},
		{
			desc: "multiple mixed formats",
			input: map[string]*tpb.Interface{
				"1/2/3": {IntName: "1/2/3"},
				"5":     {IntName: "5"},
				"eth7":  {IntName: "eth7"},
				"mgmt":  {IntName: "mgmt"},
			},
			want: map[string]*tpb.Interface{
				"eth3":  {IntName: "1/2/3"},
				"eth5":  {IntName: "5"},
				"eth7":  {IntName: "eth7"},
				"mgmt":  {IntName: "mgmt"},
			},
		},
		{
			desc: "non-matching string",
			input: map[string]*tpb.Interface{
				"foo": {IntName: "foo"},
			},
			want: map[string]*tpb.Interface{
				"foo": {IntName: "foo"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := renameInterfaces(tt.input)
			if diff := cmp.Diff(tt.want, got, cmp.Comparer(proto.Equal)); diff != "" {
				t.Errorf("renameInterfaces() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNode_CreatePod_EquipmentFile(t *testing.T) {
	tmpDir := t.TempDir()
	equipmentFile := "equipment.json"
	equipmentContent := `{"equipment": "test"}`
	equipmentPath := filepath.Join(tmpDir, equipmentFile)
	if err := os.WriteFile(equipmentPath, []byte(equipmentContent), 0644); err != nil {
		t.Fatalf("failed to write equipment file: %v", err)
	}

	vendorData, err := anypb.New(&cpb.CienaConfig{
		SystemEquipment: &cpb.SystemEquipment{
			MountDir:     "/equipment",
			EquipmentJson: equipmentFile,
		},
	})
	if err != nil {
		t.Fatalf("failed to marshal vendor data: %v", err)
	}

	nodeProto := &tpb.Node{
		Name: "wr-1",
		Config: &tpb.Config{
			Image:      "vrnetlab/ciena_waverouter:config",
			VendorData: vendorData,
		},
	}

	n := &Node{
		Impl: &node.Impl{
			Namespace:  "testns",
			KubeClient: kfake.NewSimpleClientset(),
			Proto:      nodeProto,
			BasePath:   tmpDir,
		},
	}

	err = n.CreatePod(context.Background())
	if err != nil {
		t.Fatalf("CreatePod() failed: %v", err)
	}

	pod, err := n.KubeClient.CoreV1().Pods(n.Namespace).Get(context.Background(), n.Name(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Could not get the pod: %v", err)
	}

	// Check container
	if len(pod.Spec.Containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(pod.Spec.Containers))
	}
	ctr := pod.Spec.Containers[0]
	if ctr.Name != "wr-1" {
		t.Errorf("container name: got %q, want %q", ctr.Name, "wr-1")
	}
	if ctr.Image != "vrnetlab/ciena_waverouter:config" {
		t.Errorf("container image: got %q, want %q", ctr.Image, "vrnetlab/ciena_waverouter:config")
	}
	foundMount := false
	for _, m := range ctr.VolumeMounts {
		if m.Name == "equipment-file" && m.MountPath == "/equipment/setup.json" && m.SubPath == "setup.json" && m.ReadOnly {
			foundMount = true
		}
	}
	if !foundMount {
		t.Errorf("expected equipment-file mount not found in container: %+v", ctr.VolumeMounts)
	}

	// Check volumes
	foundVol := false
	for _, v := range pod.Spec.Volumes {
		if v.Name == "equipment-file" && v.VolumeSource.ConfigMap != nil && v.VolumeSource.ConfigMap.Name == "wr-1-file" {
			foundVol = true
		}
	}
	if !foundVol {
		t.Errorf("expected equipment-file volume not found in pod: %+v", pod.Spec.Volumes)
	}
}

func TestNode_CreatePod_MissingEquipmentFile(t *testing.T) {
	tmpDir := t.TempDir()
	vendorData, err := anypb.New(&cpb.CienaConfig{
		SystemEquipment: &cpb.SystemEquipment{
			MountDir:     "/equipment",
			EquipmentJson: "nonexistent.json",
		},
	})
	if err != nil {
		t.Fatalf("failed to marshal vendor data: %v", err)
	}

	nodeProto := &tpb.Node{
		Name: "wr-2",
		Config: &tpb.Config{
			Image:      "vrnetlab/ciena_waverouter:config",
			VendorData: vendorData,
		},
	}

	n := &Node{
		Impl: &node.Impl{
			Namespace:  "testns",
			KubeClient: kfake.NewSimpleClientset(),
			Proto:      nodeProto,
			BasePath:   tmpDir,
		},
	}

	err = n.CreatePod(context.Background())
	if err == nil {
		t.Fatalf("expected error due to missing equipment file, got nil")
	}
}

func TestNode_CreatePod_NoVendorData(t *testing.T) {
	nodeProto := &tpb.Node{
		Name: "wr-3",
		Config: &tpb.Config{
			Image: "vrnetlab/ciena_waverouter:config",
		},
	}

	n := &Node{
		Impl: &node.Impl{
			Namespace:  "testns",
			KubeClient: kfake.NewSimpleClientset(),
			Proto:      nodeProto,
		},
	}

	err := n.CreatePod(context.Background())
	if err != nil {
		t.Fatalf("CreatePod() failed: %v", err)
	}

	pod, err := n.KubeClient.CoreV1().Pods(n.Namespace).Get(context.Background(), n.Name(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Could not get the pod: %v", err)
	}

	if len(pod.Spec.Containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(pod.Spec.Containers))
	}
	ctr := pod.Spec.Containers[0]
	if ctr.Name != "wr-3" {
		t.Errorf("container name: got %q, want %q", ctr.Name, "wr-3")
	}
	if ctr.Image != "vrnetlab/ciena_waverouter:config" {
		t.Errorf("container image: got %q, want %q", ctr.Image, "vrnetlab/ciena_waverouter:config")
	}
	if len(ctr.VolumeMounts) != 0 {
		t.Errorf("expected no volume mounts, got %+v", ctr.VolumeMounts)
	}
}