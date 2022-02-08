// Copyright 2021 Google LLC
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
package cisco

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/kne/topo/node"
	"github.com/h-fam/errdiff"
	"google.golang.org/protobuf/testing/protocmp"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"

	tpb "github.com/google/kne/proto/topo"
)

type fakeWatch struct {
	e []watch.Event
}

func (f *fakeWatch) Stop() {}

func (f *fakeWatch) ResultChan() <-chan watch.Event {
	eCh := make(chan watch.Event)
	go func() {
		for len(f.e) != 0 {
			e := f.e[0]
			f.e = f.e[1:]
			eCh <- e
		}
	}()
	return eCh
}
func deafultNode(pb *tpb.Node) *tpb.Node {
	node, _ := defaults(pb)
	return node
}

func TestNew(t *testing.T) {
	tests := []struct {
		desc    string
		ni      *node.Impl
		want    *tpb.Node
		wantErr string
		cErr    string
	}{{
		desc:    "nil node impl",
		wantErr: "nodeImpl cannot be nil",
	}, {
		desc: "empty proto",
		ni: &node.Impl{
			KubeClient: fake.NewSimpleClientset(),
			Namespace:  "test",
			Proto: &tpb.Node{
				Name: "pod1",
			},
		},
		want: deafultNode(&tpb.Node{
			Name: "pod1",
		}),
	}, {
		desc: "node cisco test invalid interface",
		ni: &node.Impl{
			KubeClient: fake.NewSimpleClientset(),
			Namespace:  "test",
			Proto: &tpb.Node{
				Name:  "pod1",
				Model: "xrd",
				Interfaces: map[string]*tpb.Interface{
					"eeth": &tpb.Interface{},
				},
			},
		},
		want:    nil,
		wantErr: "interface 'eeth' is invalid",
	}, {
		desc: "full proto",
		ni: &node.Impl{
			KubeClient: fake.NewSimpleClientset(),
			Namespace:  "test",
			Proto: &tpb.Node{
				Name: "pod1",
				Config: &tpb.Config{
					ConfigFile: "foo",
					ConfigPath: "/",
					ConfigData: &tpb.Config_Data{
						Data: []byte("config file data"),
					},
					Env: map[string]string{
						"XR_INTERFACES": "test/interface",
					},
				},
				Constraints: map[string]string{
					"cpu": "2",
				},
			},
		},
		want: &tpb.Node{
			Name:  "pod1",
			Model: "xrd",
			Constraints: map[string]string{
				"cpu":    "2",
				"memory": "2Gi",
			},
			Services: map[uint32]*tpb.Service{
				443: {
					Name:   "ssl",
					Inside: 443,
				},
				22: {
					Name:   "ssh",
					Inside: 22,
				},
				6030: {
					Name:   "gnmi",
					Inside: 57400,
				},
			},
			Labels: map[string]string{
				"vendor": tpb.Vendor_CISCO.String(),
			},
			Config: &tpb.Config{
				Image: "ios-xr:latest",
				Env: map[string]string{
					"XR_INTERFACES":                  "test/interface",
					"XR_CHECKSUM_OFFLOAD_COUNTERACT": "MgmtEther0/RP0/CPU0/0",
					"XR_EVERY_BOOT_CONFIG":           "/foo",
					"XR_SNOOP_IP_INTERFACES":         "MgmtEther0/RP0/CPU0/0",
				},
				EntryCommand: "kubectl exec -it pod1 -- bash",
				ConfigPath:   "/",
				ConfigFile:   "foo",
				ConfigData: &tpb.Config_Data{
					Data: []byte("config file data"),
				},
			},
		},
	}, {
		desc: "node cisco xrd test",
		ni: &node.Impl{
			KubeClient: fake.NewSimpleClientset(),
			Namespace:  "test",
			Proto: &tpb.Node{
				Name:  "pod1",
				Model: "xrd",
				Interfaces: map[string]*tpb.Interface{
					"eth1": &tpb.Interface{},
					"eth2": &tpb.Interface{
						Name: "GIG1",
					},
					"eth3": &tpb.Interface{},
				},
				Config: &tpb.Config{
					ConfigFile: "foo",
					ConfigPath: "/",
					ConfigData: &tpb.Config_Data{
						Data: []byte("config file data"),
					},
				},
			},
		},
		want: &tpb.Node{
			Name:  "pod1",
			Model: "xrd",
			Interfaces: map[string]*tpb.Interface{
				"eth1": &tpb.Interface{},
				"eth2": &tpb.Interface{
					Name: "GIG1",
				},
				"eth3": &tpb.Interface{},
			},
			Constraints: map[string]string{
				"cpu":    "1",
				"memory": "2Gi",
			},
			Services: map[uint32]*tpb.Service{
				443: {
					Name:   "ssl",
					Inside: 443,
				},
				22: {
					Name:   "ssh",
					Inside: 22,
				},
				6030: {
					Name:   "gnmi",
					Inside: 57400,
				},
			},
			Labels: map[string]string{
				"vendor": tpb.Vendor_CISCO.String(),
			},
			Config: &tpb.Config{
				Image: "ios-xr:latest",
				Env: map[string]string{
					"XR_INTERFACES":                  "MgmtEther0/RP0/CPU0/0:eth0,GigabitEthernet0/0/0/0:eth1,GIG1:eth2,GigabitEthernet0/0/0/2:eth3",
					"XR_CHECKSUM_OFFLOAD_COUNTERACT": "MgmtEther0/RP0/CPU0/0,GigabitEthernet0/0/0/0,GIG1,GigabitEthernet0/0/0/2",
					"XR_EVERY_BOOT_CONFIG":           "/foo",
					"XR_SNOOP_IP_INTERFACES":         "MgmtEther0/RP0/CPU0/0",
				},
				EntryCommand: "kubectl exec -it pod1 -- bash",
				ConfigPath:   "/",
				ConfigFile:   "foo",
				ConfigData: &tpb.Config_Data{
					Data: []byte("config file data"),
				},
			},
		},
	}, {
		desc: "Cisco 8201 Proto",
		ni: &node.Impl{
			KubeClient: fake.NewSimpleClientset(),
			Namespace:  "test",
			Proto: &tpb.Node{
				Name:  "pod1",
				Model: "8201",
				Interfaces: map[string]*tpb.Interface{
					"eth1": &tpb.Interface{},
					"eth2": &tpb.Interface{
						Name: "GIG1",
					},
					"eth24": &tpb.Interface{},
					"eth25": &tpb.Interface{},
					"eth36": &tpb.Interface{},
				},
				Config: &tpb.Config{
					ConfigFile: "foo",
					ConfigPath: "/",
					ConfigData: &tpb.Config_Data{
						Data: []byte("config file data"),
					},
				},
			},
		},
		want: &tpb.Node{
			Name:  "pod1",
			Model: "8201",
			Interfaces: map[string]*tpb.Interface{
				"eth1": &tpb.Interface{},
				"eth2": &tpb.Interface{
					Name: "GIG1",
				},
				"eth24": &tpb.Interface{},
				"eth25": &tpb.Interface{},
				"eth36": &tpb.Interface{},
			},
			Constraints: map[string]string{
				"cpu":    "4",
				"memory": "12Gi",
			},
			Services: map[uint32]*tpb.Service{
				443: {
					Name:   "ssl",
					Inside: 443,
				},
				22: {
					Name:   "ssh",
					Inside: 22,
				},
				6030: {
					Name:   "gnmi",
					Inside: 57400,
				},
			},
			Labels: map[string]string{
				"vendor": tpb.Vendor_CISCO.String(),
			},
			Config: &tpb.Config{
				Image: "ios-xr:latest",
				Env: map[string]string{
					"XR_INTERFACES":                  "MgmtEther0/RP0/CPU0/0:eth0,FourHundredGigE0/0/0/0:eth1,GIG1:eth2,FourHundredGigE0/0/0/23:eth24,HundredGigE0/0/0/24:eth25,HundredGigE0/0/0/35:eth36",
					"XR_CHECKSUM_OFFLOAD_COUNTERACT": "MgmtEther0/RP0/CPU0/0,FourHundredGigE0/0/0/0,GIG1,FourHundredGigE0/0/0/23,HundredGigE0/0/0/24,HundredGigE0/0/0/35",
					"XR_EVERY_BOOT_CONFIG":           "/foo",
				},
				EntryCommand: "kubectl exec -it pod1 -- bash",
				ConfigPath:   "/",
				ConfigFile:   "foo",
				ConfigData: &tpb.Config_Data{
					Data: []byte("config file data"),
				},
			},
		},
	}, {
		desc: "Cisco 8201 Proto- Invalid interface id",
		ni: &node.Impl{
			KubeClient: fake.NewSimpleClientset(),
			Namespace:  "test",
			Proto: &tpb.Node{
				Name:  "pod1",
				Model: "8201",
				Interfaces: map[string]*tpb.Interface{
					"eth37": &tpb.Interface{},
				},
			},
		},
		want:    nil,
		wantErr: "interface id 37 can not be mapped to a cisco interface, eth1..eth36 is supported on 8201",
	}, {
		desc: "Cisco 8202 proto",
		ni: &node.Impl{
			KubeClient: fake.NewSimpleClientset(),
			Namespace:  "test",
			Proto: &tpb.Node{
				Name:  "pod1",
				Model: "8202",
				Interfaces: map[string]*tpb.Interface{
					"eth1": &tpb.Interface{},
					"eth2": &tpb.Interface{
						Name: "GIG1",
					},
					"eth48": &tpb.Interface{},
					"eth49": &tpb.Interface{},
					"eth60": &tpb.Interface{},
					"eth61": &tpb.Interface{},
					"eth72": &tpb.Interface{},
				},
				Config: &tpb.Config{
					ConfigFile: "foo",
					ConfigPath: "/",
					ConfigData: &tpb.Config_Data{
						Data: []byte("config file data"),
					},
				},
			},
		},
		want: &tpb.Node{
			Name:  "pod1",
			Model: "8202",
			Interfaces: map[string]*tpb.Interface{
				"eth1": &tpb.Interface{},
				"eth2": &tpb.Interface{
					Name: "GIG1",
				},
				"eth48": &tpb.Interface{},
				"eth49": &tpb.Interface{},
				"eth60": &tpb.Interface{},
				"eth61": &tpb.Interface{},
				"eth72": &tpb.Interface{},
			},
			Constraints: map[string]string{
				"cpu":    "4",
				"memory": "12Gi",
			},
			Services: map[uint32]*tpb.Service{
				443: {
					Name:   "ssl",
					Inside: 443,
				},
				22: {
					Name:   "ssh",
					Inside: 22,
				},
				6030: {
					Name:   "gnmi",
					Inside: 57400,
				},
			},
			Labels: map[string]string{
				"vendor": tpb.Vendor_CISCO.String(),
			},
			Config: &tpb.Config{
				Image: "ios-xr:latest",
				Env: map[string]string{
					"XR_INTERFACES":                  "MgmtEther0/RP0/CPU0/0:eth0,HundredGigE0/0/0/0:eth1,GIG1:eth2,HundredGigE0/0/0/47:eth48,FourHundredGigE0/0/0/48:eth49,FourHundredGigE0/0/0/59:eth60,HundredGigE0/0/0/60:eth61,HundredGigE0/0/0/71:eth72",
					"XR_CHECKSUM_OFFLOAD_COUNTERACT": "MgmtEther0/RP0/CPU0/0,HundredGigE0/0/0/0,GIG1,HundredGigE0/0/0/47,FourHundredGigE0/0/0/48,FourHundredGigE0/0/0/59,HundredGigE0/0/0/60,HundredGigE0/0/0/71",
					"XR_EVERY_BOOT_CONFIG":           "/foo",
				},
				EntryCommand: "kubectl exec -it pod1 -- bash",
				ConfigPath:   "/",
				ConfigFile:   "foo",
				ConfigData: &tpb.Config_Data{
					Data: []byte("config file data"),
				},
			},
		},
	}, {
		desc: "Cisco 8202 Proto- Invalid interface id",
		ni: &node.Impl{
			KubeClient: fake.NewSimpleClientset(),
			Namespace:  "test",
			Proto: &tpb.Node{
				Name:  "pod1",
				Model: "8202",
				Interfaces: map[string]*tpb.Interface{
					"eth73": &tpb.Interface{},
				},
			},
		},
		want:    nil,
		wantErr: "interface id 73 can not be mapped to a cisco interface, eth1..eth72 is supported on 8202",
	}, {
		desc: "Cisco 8201-32FH Proto- Invalid interface id",
		ni: &node.Impl{
			KubeClient: fake.NewSimpleClientset(),
			Namespace:  "test",
			Proto: &tpb.Node{
				Name:  "pod1",
				Model: "8201-32FH",
				Interfaces: map[string]*tpb.Interface{
					"eth33": &tpb.Interface{},
				},
			},
		},
		want:    nil,
		wantErr: "interface id 33 can not be mapped to a cisco interface, eth1..eth32 is supported on 8201-32FH",
	}, {
		desc: "Cisco 8201-32FH proto",
		ni: &node.Impl{
			KubeClient: fake.NewSimpleClientset(),
			Namespace:  "test",
			Proto: &tpb.Node{
				Name:  "pod1",
				Model: "8201-32FH",
				Interfaces: map[string]*tpb.Interface{
					"eth1": &tpb.Interface{},
					"eth2": &tpb.Interface{
						Name: "GIG1",
					},
					"eth32": &tpb.Interface{},
				},
				Config: &tpb.Config{
					ConfigFile: "foo",
					ConfigPath: "/",
					ConfigData: &tpb.Config_Data{
						Data: []byte("config file data"),
					},
				},
			},
		},
		want: &tpb.Node{
			Name:  "pod1",
			Model: "8201-32FH",
			Interfaces: map[string]*tpb.Interface{
				"eth1": &tpb.Interface{},
				"eth2": &tpb.Interface{
					Name: "GIG1",
				},
				"eth32": &tpb.Interface{},
			},
			Constraints: map[string]string{
				"cpu":    "4",
				"memory": "12Gi",
			},
			Services: map[uint32]*tpb.Service{
				443: {
					Name:   "ssl",
					Inside: 443,
				},
				22: {
					Name:   "ssh",
					Inside: 22,
				},
				6030: {
					Name:   "gnmi",
					Inside: 57400,
				},
			},
			Labels: map[string]string{
				"vendor": tpb.Vendor_CISCO.String(),
			},
			Config: &tpb.Config{
				Image: "ios-xr:latest",
				Env: map[string]string{
					"XR_INTERFACES":                  "MgmtEther0/RP0/CPU0/0:eth0,FourHundredGigE0/0/0/0:eth1,GIG1:eth2,FourHundredGigE0/0/0/31:eth32",
					"XR_CHECKSUM_OFFLOAD_COUNTERACT": "MgmtEther0/RP0/CPU0/0,FourHundredGigE0/0/0/0,GIG1,FourHundredGigE0/0/0/31",
					"XR_EVERY_BOOT_CONFIG":           "/foo",
				},
				EntryCommand: "kubectl exec -it pod1 -- bash",
				ConfigPath:   "/",
				ConfigFile:   "foo",
				ConfigData: &tpb.Config_Data{
					Data: []byte("config file data"),
				},
			},
		},
	}, {
		desc: "8101-32H",
		ni: &node.Impl{
			KubeClient: fake.NewSimpleClientset(),
			Namespace:  "test",
			Proto: &tpb.Node{
				Name:  "pod1",
				Model: "8101-32H",
				Interfaces: map[string]*tpb.Interface{
					"eth1": &tpb.Interface{},
					"eth2": &tpb.Interface{
						Name: "GIG1",
					},
					"eth32": &tpb.Interface{},
				},
				Config: &tpb.Config{
					ConfigFile: "foo",
					ConfigPath: "/",
					ConfigData: &tpb.Config_Data{
						Data: []byte("config file data"),
					},
				},
			},
		},
		want: &tpb.Node{
			Name:  "pod1",
			Model: "8101-32H",
			Interfaces: map[string]*tpb.Interface{
				"eth1": &tpb.Interface{},
				"eth2": &tpb.Interface{
					Name: "GIG1",
				},
				"eth32": &tpb.Interface{},
			},
			Constraints: map[string]string{
				"cpu":    "4",
				"memory": "12Gi",
			},
			Services: map[uint32]*tpb.Service{
				443: {
					Name:   "ssl",
					Inside: 443,
				},
				22: {
					Name:   "ssh",
					Inside: 22,
				},
				6030: {
					Name:   "gnmi",
					Inside: 57400,
				},
			},
			Labels: map[string]string{
				"vendor": tpb.Vendor_CISCO.String(),
			},
			Config: &tpb.Config{
				Image: "ios-xr:latest",
				Env: map[string]string{
					"XR_INTERFACES":                  "MgmtEther0/RP0/CPU0/0:eth0,HundredGigE0/0/0/0:eth1,GIG1:eth2,HundredGigE0/0/0/31:eth32",
					"XR_CHECKSUM_OFFLOAD_COUNTERACT": "MgmtEther0/RP0/CPU0/0,HundredGigE0/0/0/0,GIG1,HundredGigE0/0/0/31",
					"XR_EVERY_BOOT_CONFIG":           "/foo",
				},
				EntryCommand: "kubectl exec -it pod1 -- bash",
				ConfigPath:   "/",
				ConfigFile:   "foo",
				ConfigData: &tpb.Config_Data{
					Data: []byte("config file data"),
				},
			},
		},
	}, {
		desc: "Cisco 8101-32H Proto- Invalid interface id",
		ni: &node.Impl{
			KubeClient: fake.NewSimpleClientset(),
			Namespace:  "test",
			Proto: &tpb.Node{
				Name:  "pod1",
				Model: "8101-32H",
				Interfaces: map[string]*tpb.Interface{
					"eth33": &tpb.Interface{},
				},
			},
		},
		want:    nil,
		wantErr: "interface id 33 can not be mapped to a cisco interface, eth1..eth32 is supported on 8101-32H",
	}, {
		desc: "Cisco 8102-64H Proto- Invalid interface id",
		ni: &node.Impl{
			KubeClient: fake.NewSimpleClientset(),
			Namespace:  "test",
			Proto: &tpb.Node{
				Name:  "pod1",
				Model: "8102-64H",
				Interfaces: map[string]*tpb.Interface{
					"eth65": &tpb.Interface{},
				},
			},
		},
		want:    nil,
		wantErr: "interface id 65 can not be mapped to a cisco interface, eth1..eth64 is supported on 8102-64H",
	}, {
		desc: "8102-64H",
		ni: &node.Impl{
			KubeClient: fake.NewSimpleClientset(),
			Namespace:  "test",
			Proto: &tpb.Node{
				Name:  "pod1",
				Model: "8102-64H",
				Interfaces: map[string]*tpb.Interface{
					"eth1": &tpb.Interface{},
					"eth2": &tpb.Interface{
						Name: "GIG1",
					},
					"eth64": &tpb.Interface{},
				},
				Config: &tpb.Config{
					ConfigFile: "foo",
					ConfigPath: "/",
					ConfigData: &tpb.Config_Data{
						Data: []byte("config file data"),
					},
				},
			},
		},
		want: &tpb.Node{
			Name:  "pod1",
			Model: "8102-64H",
			Interfaces: map[string]*tpb.Interface{
				"eth1": &tpb.Interface{},
				"eth2": &tpb.Interface{
					Name: "GIG1",
				},
				"eth64": &tpb.Interface{},
			},
			Constraints: map[string]string{
				"cpu":    "4",
				"memory": "12Gi",
			},
			Services: map[uint32]*tpb.Service{
				443: {
					Name:   "ssl",
					Inside: 443,
				},
				22: {
					Name:   "ssh",
					Inside: 22,
				},
				6030: {
					Name:   "gnmi",
					Inside: 57400,
				},
			},
			Labels: map[string]string{
				"vendor": tpb.Vendor_CISCO.String(),
			},
			Config: &tpb.Config{
				Image: "ios-xr:latest",
				Env: map[string]string{
					"XR_INTERFACES":                  "MgmtEther0/RP0/CPU0/0:eth0,HundredGigE0/0/0/0:eth1,GIG1:eth2,HundredGigE0/0/0/63:eth64",
					"XR_CHECKSUM_OFFLOAD_COUNTERACT": "MgmtEther0/RP0/CPU0/0,HundredGigE0/0/0/0,GIG1,HundredGigE0/0/0/63",
					"XR_EVERY_BOOT_CONFIG":           "/foo",
				},
				EntryCommand: "kubectl exec -it pod1 -- bash",
				ConfigPath:   "/",
				ConfigFile:   "foo",
				ConfigData: &tpb.Config_Data{
					Data: []byte("config file data"),
				},
			},
		},
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			n, err := New(tt.ni)
			if s := errdiff.Check(err, tt.wantErr); s != "" {
				t.Fatalf("Unexpected error: %s", s)
			}
			if err != nil {
				return
			}
			if s := cmp.Diff(n.GetProto(), tt.want, protocmp.Transform(), protocmp.IgnoreFields(&tpb.Service{}, "node_port")); s != "" {
				t.Fatalf("Protos not equal: %s", s)
			}
			err = n.Create(context.Background())
			if s := errdiff.Check(err, tt.cErr); s != "" {
				t.Fatalf("Unexpected error: %s", s)
			}
		})
	}
}
