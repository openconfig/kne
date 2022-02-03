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
package xrd

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
		desc:    "nil pb",
		ni:      &node.Impl{},
		wantErr: "nodeImpl.Proto cannot be nil",
	}, {
		desc: "empty proto",
		ni: &node.Impl{
			KubeClient: fake.NewSimpleClientset(),
			Namespace:  "test",
			Proto: &tpb.Node{
				Name: "pod1",
			},
		},
		want: defaults(&tpb.Node{
			Name: "pod1",
		}),
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
			Name: "pod1",
			Constraints: map[string]string{
				"cpu":    "2",
				"memory": "2Gi",
			},
			Services: map[uint32]*tpb.Service{
				443: {
					Name:     "ssl",
					Inside:   443,
				},
				22: {
					Name:     "ssh",
					Inside:   22,
				},
				6030: {
					Name:     "gnmi",
					Inside:   57400,
				},
			},
			Labels: map[string]string{
				"vendor": tpb.Vendor_CISCO.String(),
			},
			Config: &tpb.Config{
				Image: "ios-xr:latest",
				Env: map[string]string{
					"XR_INTERFACES":                  "test/interface",
					"XR_CHECKSUM_OFFLOAD_COUNTERACT": "GigabitEthernet0/0/0/0",
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
