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
package cisco

import (
	"context"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/openconfig/gnmi/errdiff"
	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	scrapliopts "github.com/scrapli/scrapligo/driver/options"
	scraplitransport "github.com/scrapli/scrapligo/transport"
	scrapliutil "github.com/scrapli/scrapligo/util"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/testing/protocmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func init() {
	skipValidation = true
}

func defaultNode(pb *tpb.Node) *tpb.Node {
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
		want: defaultNode(&tpb.Node{
			Name: "pod1",
		}),
	}, {
		desc: "node cisco test invalid interface",
		ni: &node.Impl{
			KubeClient: fake.NewSimpleClientset(),
			Namespace:  "test",
			Proto: &tpb.Node{
				Name:  "pod1",
				Model: ModelXRD,
				Interfaces: map[string]*tpb.Interface{
					"eeth": {},
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
			Model: ModelXRD,
			Os:    "ios-xr",
			Constraints: map[string]string{
				"cpu":    "2",
				"memory": "2Gi",
			},
			Services: map[uint32]*tpb.Service{
				22: {
					Names:  []string{"ssh"},
					Inside: 22,
				},
				9339: {
					Names:  []string{"gnmi", "gnoi", "gnsi"},
					Inside: 57400,
				},
				9340: {
					Names:  []string{"gribi"},
					Inside: 57400,
				},
				9559: {
					Names:  []string{"p4rt"},
					Inside: 57400,
				},
			},
			Labels: map[string]string{
				"vendor":       tpb.Vendor_CISCO.String(),
				"ondatra-role": "DUT",
				"model":        ModelXRD,
				"os":           "ios-xr",
			},
			Config: &tpb.Config{
				Image: "xrd:latest",
				Env: map[string]string{
					"XR_INTERFACES":        "test/interface",
					"XR_EVERY_BOOT_CONFIG": "/foo",
					"XR_MGMT_INTERFACES":   "linux:eth0,xr_name=MgmtEth0/RP0/CPU0/0,chksum,snoop_v4,snoop_v6",
				},
				EntryCommand: "kubectl exec -it pod1 -- bash",
				ConfigPath:   "/",
				ConfigFile:   "foo",
				ConfigData: &tpb.Config_Data{
					Data: []byte("config file data"),
				},
				Cert: &tpb.CertificateCfg{
					Config: &tpb.CertificateCfg_SelfSigned{
						SelfSigned: &tpb.SelfSignedCertCfg{
							CertName: "ems.pem",
							KeyName:  "ems.key",
							KeySize:  2048,
						},
					},
				},
			},
			HostConstraints: []*tpb.HostConstraint{
				{
					Constraint: &tpb.HostConstraint_KernelConstraint{
						KernelConstraint: &tpb.KernelParam{
							Name:           "fs.inotify.max_user_instances",
							ConstraintType: &tpb.KernelParam_BoundedInteger{BoundedInteger: &tpb.BoundedInteger{MinValue: 64000}},
						},
					},
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
				Model: ModelXRD,
				Interfaces: map[string]*tpb.Interface{
					"eth1": {},
					"eth2": {
						Name: "GIG1",
					},
					"eth3": {},
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
			Model: ModelXRD,
			Os:    "ios-xr",
			Interfaces: map[string]*tpb.Interface{
				"eth1": {},
				"eth2": {
					Name: "GIG1",
				},
				"eth3": {},
			},
			Constraints: map[string]string{
				"cpu":    "1",
				"memory": "2Gi",
			},
			Services: map[uint32]*tpb.Service{
				22: {
					Names:  []string{"ssh"},
					Inside: 22,
				},
				9339: {
					Names:  []string{"gnmi", "gnoi", "gnsi"},
					Inside: 57400,
				},
				9340: {
					Names:  []string{"gribi"},
					Inside: 57400,
				},
				9559: {
					Names:  []string{"p4rt"},
					Inside: 57400,
				},
			},
			Labels: map[string]string{
				"vendor":       tpb.Vendor_CISCO.String(),
				"ondatra-role": "DUT",
				"model":        ModelXRD,
				"os":           "ios-xr",
			},
			Config: &tpb.Config{
				Image: "xrd:latest",
				Env: map[string]string{
					"XR_INTERFACES":        "linux:eth1,xr_name=GigabitEthernet0/0/0/0;linux:eth2,xr_name=GIG1;linux:eth3,xr_name=GigabitEthernet0/0/0/2",
					"XR_EVERY_BOOT_CONFIG": "/foo",
					"XR_MGMT_INTERFACES":   "linux:eth0,xr_name=MgmtEth0/RP0/CPU0/0,chksum,snoop_v4,snoop_v6",
				},
				EntryCommand: "kubectl exec -it pod1 -- bash",
				ConfigPath:   "/",
				ConfigFile:   "foo",
				ConfigData: &tpb.Config_Data{
					Data: []byte("config file data"),
				},
				Cert: &tpb.CertificateCfg{
					Config: &tpb.CertificateCfg_SelfSigned{
						SelfSigned: &tpb.SelfSignedCertCfg{
							CertName: "ems.pem",
							KeyName:  "ems.key",
							KeySize:  2048,
						},
					},
				},
			},
			HostConstraints: []*tpb.HostConstraint{
				{
					Constraint: &tpb.HostConstraint_KernelConstraint{
						KernelConstraint: &tpb.KernelParam{
							Name:           "fs.inotify.max_user_instances",
							ConstraintType: &tpb.KernelParam_BoundedInteger{BoundedInteger: &tpb.BoundedInteger{MinValue: 64000}},
						},
					},
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
					"eth1": {},
					"eth2": {
						Name: "GIG1",
					},
					"eth24": {},
					"eth25": {},
					"eth36": {},
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
			Os:    "ios-xr",
			Interfaces: map[string]*tpb.Interface{
				"eth1": {},
				"eth2": {
					Name: "GIG1",
				},
				"eth24": {},
				"eth25": {},
				"eth36": {},
			},
			Constraints: map[string]string{
				"cpu":    "4",
				"memory": "20Gi",
			},
			Services: map[uint32]*tpb.Service{
				22: {
					Names:  []string{"ssh"},
					Inside: 22,
				},
				9339: {
					Names:  []string{"gnmi", "gnoi", "gnsi"},
					Inside: 57400,
				},
				9340: {
					Names:  []string{"gribi"},
					Inside: 57400,
				},
				9559: {
					Names:  []string{"p4rt"},
					Inside: 57400,
				},
			},
			Labels: map[string]string{
				"vendor":       tpb.Vendor_CISCO.String(),
				"ondatra-role": "DUT",
				"model":        "8201",
				"os":           "ios-xr",
			},
			Config: &tpb.Config{
				Image: "8000e:latest",
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
				Cert: &tpb.CertificateCfg{
					Config: &tpb.CertificateCfg_SelfSigned{
						SelfSigned: &tpb.SelfSignedCertCfg{
							CertName: "ems.pem",
							KeyName:  "ems.key",
							KeySize:  2048,
						},
					},
				},
			},
			HostConstraints: []*tpb.HostConstraint{
				{
					Constraint: &tpb.HostConstraint_KernelConstraint{
						KernelConstraint: &tpb.KernelParam{
							Name:           "kernel.pid_max",
							ConstraintType: &tpb.KernelParam_BoundedInteger{BoundedInteger: &tpb.BoundedInteger{MaxValue: 1048575}},
						},
					},
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
					"eth37": {},
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
					"eth1": {},
					"eth2": {
						Name: "GIG1",
					},
					"eth48": {},
					"eth49": {},
					"eth60": {},
					"eth61": {},
					"eth72": {},
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
			Os:    "ios-xr",
			Interfaces: map[string]*tpb.Interface{
				"eth1": {},
				"eth2": {
					Name: "GIG1",
				},
				"eth48": {},
				"eth49": {},
				"eth60": {},
				"eth61": {},
				"eth72": {},
			},
			Constraints: map[string]string{
				"cpu":    "4",
				"memory": "20Gi",
			},
			Services: map[uint32]*tpb.Service{
				22: {
					Names:  []string{"ssh"},
					Inside: 22,
				},
				9339: {
					Names:  []string{"gnmi", "gnoi", "gnsi"},
					Inside: 57400,
				},
				9340: {
					Names:  []string{"gribi"},
					Inside: 57400,
				},
				9559: {
					Names:  []string{"p4rt"},
					Inside: 57400,
				},
			},
			Labels: map[string]string{
				"vendor":       tpb.Vendor_CISCO.String(),
				"ondatra-role": "DUT",
				"model":        "8202",
				"os":           "ios-xr",
			},
			Config: &tpb.Config{
				Image: "8000e:latest",
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
				Cert: &tpb.CertificateCfg{
					Config: &tpb.CertificateCfg_SelfSigned{
						SelfSigned: &tpb.SelfSignedCertCfg{
							CertName: "ems.pem",
							KeyName:  "ems.key",
							KeySize:  2048,
						},
					},
				},
			},
			HostConstraints: []*tpb.HostConstraint{
				{
					Constraint: &tpb.HostConstraint_KernelConstraint{
						KernelConstraint: &tpb.KernelParam{
							Name:           "kernel.pid_max",
							ConstraintType: &tpb.KernelParam_BoundedInteger{BoundedInteger: &tpb.BoundedInteger{MaxValue: 1048575}},
						},
					},
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
					"eth73": {},
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
					"eth33": {},
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
					"eth1": {},
					"eth2": {
						Name: "GIG1",
					},
					"eth32": {},
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
			Os:    "ios-xr",
			Interfaces: map[string]*tpb.Interface{
				"eth1": {},
				"eth2": {
					Name: "GIG1",
				},
				"eth32": {},
			},
			Constraints: map[string]string{
				"cpu":    "4",
				"memory": "20Gi",
			},
			Services: map[uint32]*tpb.Service{
				22: {
					Names:  []string{"ssh"},
					Inside: 22,
				},
				9339: {
					Names:  []string{"gnmi", "gnoi", "gnsi"},
					Inside: 57400,
				},
				9340: {
					Names:  []string{"gribi"},
					Inside: 57400,
				},
				9559: {
					Names:  []string{"p4rt"},
					Inside: 57400,
				},
			},
			Labels: map[string]string{
				"vendor":       tpb.Vendor_CISCO.String(),
				"ondatra-role": "DUT",
				"model":        "8201-32FH",
				"os":           "ios-xr",
			},
			Config: &tpb.Config{
				Image: "8000e:latest",
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
				Cert: &tpb.CertificateCfg{
					Config: &tpb.CertificateCfg_SelfSigned{
						SelfSigned: &tpb.SelfSignedCertCfg{
							CertName: "ems.pem",
							KeyName:  "ems.key",
							KeySize:  2048,
						},
					},
				},
			},
			HostConstraints: []*tpb.HostConstraint{
				{
					Constraint: &tpb.HostConstraint_KernelConstraint{
						KernelConstraint: &tpb.KernelParam{
							Name:           "kernel.pid_max",
							ConstraintType: &tpb.KernelParam_BoundedInteger{BoundedInteger: &tpb.BoundedInteger{MaxValue: 1048575}},
						},
					},
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
					"eth1": {},
					"eth2": {
						Name: "GIG1",
					},
					"eth32": {},
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
			Os:    "ios-xr",
			Interfaces: map[string]*tpb.Interface{
				"eth1": {},
				"eth2": {
					Name: "GIG1",
				},
				"eth32": {},
			},
			Constraints: map[string]string{
				"cpu":    "4",
				"memory": "20Gi",
			},
			Services: map[uint32]*tpb.Service{
				22: {
					Names:  []string{"ssh"},
					Inside: 22,
				},
				9339: {
					Names:  []string{"gnmi", "gnoi", "gnsi"},
					Inside: 57400,
				},
				9340: {
					Names:  []string{"gribi"},
					Inside: 57400,
				},
				9559: {
					Names:  []string{"p4rt"},
					Inside: 57400,
				},
			},
			Labels: map[string]string{
				"vendor":       tpb.Vendor_CISCO.String(),
				"ondatra-role": "DUT",
				"model":        "8101-32H",
				"os":           "ios-xr",
			},
			Config: &tpb.Config{
				Image: "8000e:latest",
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
				Cert: &tpb.CertificateCfg{
					Config: &tpb.CertificateCfg_SelfSigned{
						SelfSigned: &tpb.SelfSignedCertCfg{
							CertName: "ems.pem",
							KeyName:  "ems.key",
							KeySize:  2048,
						},
					},
				},
			},
			HostConstraints: []*tpb.HostConstraint{
				{
					Constraint: &tpb.HostConstraint_KernelConstraint{
						KernelConstraint: &tpb.KernelParam{
							Name:           "kernel.pid_max",
							ConstraintType: &tpb.KernelParam_BoundedInteger{BoundedInteger: &tpb.BoundedInteger{MaxValue: 1048575}},
						},
					},
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
					"eth33": {},
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
					"eth65": {},
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
					"eth1": {},
					"eth2": {
						Name: "GIG1",
					},
					"eth64": {},
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
			Os:    "ios-xr",
			Interfaces: map[string]*tpb.Interface{
				"eth1": {},
				"eth2": {
					Name: "GIG1",
				},
				"eth64": {},
			},
			Constraints: map[string]string{
				"cpu":    "4",
				"memory": "20Gi",
			},
			Services: map[uint32]*tpb.Service{
				22: {
					Names:  []string{"ssh"},
					Inside: 22,
				},
				9339: {
					Names:  []string{"gnmi", "gnoi", "gnsi"},
					Inside: 57400,
				},
				9340: {
					Names:  []string{"gribi"},
					Inside: 57400,
				},
				9559: {
					Names:  []string{"p4rt"},
					Inside: 57400,
				},
			},
			Labels: map[string]string{
				"vendor":       tpb.Vendor_CISCO.String(),
				"ondatra-role": "DUT",
				"model":        "8102-64H",
				"os":           "ios-xr",
			},
			Config: &tpb.Config{
				Image: "8000e:latest",
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
				Cert: &tpb.CertificateCfg{
					Config: &tpb.CertificateCfg_SelfSigned{
						SelfSigned: &tpb.SelfSignedCertCfg{
							CertName: "ems.pem",
							KeyName:  "ems.key",
							KeySize:  2048,
						},
					},
				},
			},
			HostConstraints: []*tpb.HostConstraint{
				{
					Constraint: &tpb.HostConstraint_KernelConstraint{
						KernelConstraint: &tpb.KernelParam{
							Name:           "kernel.pid_max",
							ConstraintType: &tpb.KernelParam_BoundedInteger{BoundedInteger: &tpb.BoundedInteger{MaxValue: 1048575}},
						},
					},
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
			if s := cmp.Diff(tt.want, n.GetProto(), protocmp.Transform(), protocmp.IgnoreFields(&tpb.Service{}, "node_port")); s != "" {
				t.Fatalf("New() failed: diff (-want, +got): %v\nwant\n\n %s\ngot\n\n%s", s, prototext.Format(tt.want), prototext.Format(n.GetProto()))
			}
			err = n.Create(context.Background())
			if s := errdiff.Check(err, tt.cErr); s != "" {
				t.Fatalf("Unexpected error: %s", s)
			}
		})
	}
}

var (
	ki = fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "8000e",
				Namespace: "test",
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "xrd",
				Namespace: "test",
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
			},
		},
	)
	node8000e = &node.Impl{
		KubeClient: ki,
		Namespace:  "test",
		Proto: &tpb.Node{
			Name:   "8000e",
			Vendor: tpb.Vendor_CISCO,
			Config: &tpb.Config{},
			Model:  "8201-32FH",
		},
	}

	nodeXRD = &node.Impl{
		KubeClient: ki,
		Namespace:  "test",
		Proto: &tpb.Node{
			Name:   "xrd",
			Vendor: tpb.Vendor_CISCO,
			Config: &tpb.Config{},
			Model:  ModelXRD,
		},
	}
)

func TestNodeStatus(t *testing.T) {
	tests := []struct {
		desc      string
		status    node.Status
		ni        *node.Impl
		podLogErr bool
	}{
		{
			desc:   "Status test for 8000e Node",
			status: node.StatusRunning,
			ni:     node8000e,
		},
		{
			desc:      "Negative Status test for 8000e Node",
			status:    node.StatusPending,
			ni:        node8000e,
			podLogErr: true,
		},
		{
			desc:   "Status test for XRD Node",
			status: node.StatusRunning,
			ni:     nodeXRD,
		},
		{
			desc:      "Status test for XRD Node, pod logs do not matter",
			status:    node.StatusRunning,
			ni:        nodeXRD,
			podLogErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			ctx := context.Background()
			if !tt.podLogErr {
				origPodIsUpRegex := podIsUpRegex
				defer func() {
					podIsUpRegex = origPodIsUpRegex
				}()
				podIsUpRegex = regexp.MustCompile("fake log") // this is the expected log from a fake pod
			}
			nImpl, _ := New(tt.ni)
			n, _ := nImpl.(*Node)
			status, err := n.Status(ctx)
			if err != nil {
				t.Errorf("Error is not expected for Node Status")
			}
			if status != tt.status {
				t.Errorf("node.Status() = %v, want %v", status, tt.status)
			}
		})
	}
}

func TestResetCfg(t *testing.T) {
	tests := []struct {
		desc     string
		wantErr  bool
		ni       *node.Impl
		testFile string
	}{
		{
			// device returns error when the startup config is not initialized.
			desc:     "failed reset for XRd (not initialized)",
			wantErr:  true,
			ni:       nodeXRD,
			testFile: "testdata/xrd_reset_config_failure",
		},
		{
			// device returns error when the startup config is invalid.
			desc:     "failed reset for XRd (invalid)",
			wantErr:  true,
			ni:       nodeXRD,
			testFile: "testdata/xrd_reset_config_failure_invalid",
		},
		{
			// device returns success after applying the startup config
			desc:    "successful reset for xrd",
			wantErr: false,
			ni:      nodeXRD,
			testFile: "testdata/xrd_reset_config_success",
		},
		{
			// device returns error when the startup config is not initialized.
			desc:     "failed reset for 8000e (not initialized)",
			wantErr:  true,
			ni:       node8000e,
			testFile: "testdata/reset_config_failure",
		},
		{
			// device returns error when the startup config is invalid.
			desc:     "failed reset for 8000e (invalid)",
			wantErr:  true,
			ni:       node8000e,
			testFile: "testdata/reset_config_failure_invalid",
		},
		{
			// device returns success after applying the startup config
			desc:     "successful reset for 8000e",
			wantErr:  false,
			ni:       node8000e,
			testFile: "testdata/reset_config_success",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			nImpl, err := New(tt.ni)
			if err != nil {
				t.Fatalf("failed creating cisco node")
			}
			n, _ := nImpl.(*Node)
			n.testOpts = []scrapliutil.Option{
				scrapliopts.WithTransportType(scraplitransport.FileTransport),
				scrapliopts.WithFileTransportFile(tt.testFile),
				scrapliopts.WithTimeoutOps(2 * time.Second),
				scrapliopts.WithTransportReadSize(1),
				scrapliopts.WithReadDelay(0),
				scrapliopts.WithDefaultLogger(),
			}
			ctx := context.Background()
			err = n.ResetCfg(ctx)
			if tt.wantErr && err == nil {
				t.Fatal("Expecting an error, but no error is raised \n")
			}

			if !tt.wantErr && err != nil {
				t.Fatalf("Not expecting an error, but received an error: %v \n", err)
			}
		})
	}
}

func TestPushCfg(t *testing.T) {
	tests := []struct {
		desc     string
		wantErr  bool
		ni       *node.Impl
		testFile string
		testConf string
	}{
		{
			desc:     "successful push config for xrd",
			wantErr:  false,
			ni:       nodeXRD,
			testFile: "testdata/xrd_push_config_success",
			testConf: "testdata/valid_config",
		},
		{
			desc:     "failed push config for xrd",
			wantErr:  true,
			ni:       nodeXRD,
			testFile: "testdata/xrd_push_config_failure",
			testConf: "testdata/invalid_config",
		},
		{
			desc:     "successful push config for 8000e",
			wantErr:  false,
			ni:       node8000e,
			testFile: "testdata/push_config_success",
			testConf: "testdata/valid_config",
		},
		{
			desc:     "failed push config for 8000e",
			wantErr:  true,
			ni:       node8000e,
			testFile: "testdata/push_config_failure",
			testConf: "testdata/invalid_config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			nImpl, err := New(tt.ni)
			if err != nil {
				t.Fatalf("failed creating cisco node")
			}
			n, _ := nImpl.(*Node)
			n.testOpts = []scrapliutil.Option{
				scrapliopts.WithTransportType(scraplitransport.FileTransport),
				scrapliopts.WithFileTransportFile(tt.testFile),
				scrapliopts.WithTimeoutOps(2 * time.Second),
				scrapliopts.WithTransportReadSize(1),
				scrapliopts.WithReadDelay(0),
				scrapliopts.WithDefaultLogger(),
			}
			fp, err := os.Open(tt.testConf)
			if err != nil {
				t.Fatalf("unable to open file, error: %+v\n", err)
			}
			defer fp.Close()
			err = n.ConfigPush(context.Background(), fp)
			if tt.wantErr && err == nil {
				t.Fatal("Expecting an error, but no error is raised \n")
			}

			if !tt.wantErr && err != nil {
				t.Fatalf("Not expecting an error, but received an error: %v \n", err)
			}
		})
	}
}

func TestGenerateSelfSigned(t *testing.T) {
	n := &Node{}
	err := n.GenerateSelfSigned(context.Background())
	want := codes.Unimplemented
	if s, ok := status.FromError(err); !ok || s.Code() != want {
		t.Fatalf("GenerateSelfSigned() unexpected error get %v, want %v", s, want)
	}
}
