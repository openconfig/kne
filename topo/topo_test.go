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

package topo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	tfake "github.com/networkop/meshnet-cni/api/clientset/v1beta1/fake"
	topologyv1 "github.com/networkop/meshnet-cni/api/types/v1beta1"
	"github.com/openconfig/gnmi/errdiff"
	cpb "github.com/openconfig/kne/proto/controller"
	epb "github.com/openconfig/kne/proto/event"
	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/watch"
	kfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	ktest "k8s.io/client-go/testing"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		desc    string
		path    string
		wantErr bool
	}{{
		desc: "pb",
		path: "testdata/valid_topo.pb.txt",
	}, {
		desc: "yaml",
		path: "testdata/valid_topo.yaml",
	}, {
		desc:    "pb invalid",
		path:    "testdata/invalid_topo.pb.txt",
		wantErr: true,
	}, {
		desc:    "yaml invalid",
		path:    "testdata/invalid_topo.yaml",
		wantErr: true,
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			_, err := Load(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

type configurable struct {
	*node.Impl
}

func (c *configurable) ConfigPush(_ context.Context, r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if string(b) == "error" {
		return fmt.Errorf("error")
	}
	return nil
}

func (c *configurable) ValidateConstraints() error {
	if len(c.Proto.HostConstraints) != 0 {
		return fmt.Errorf("fake error for host constraints")
	}
	return nil
}

func NewConfigurable(impl *node.Impl) (node.Node, error) {
	return &configurable{Impl: impl}, nil
}

type loopbackable struct {
	*node.Impl
}

func (l *loopbackable) BackToBackLoop() bool {
	return true
}

func NewLoopbackable(impl *node.Impl) (node.Node, error) {
	return &loopbackable{Impl: impl}, nil
}

type notConfigurable struct {
	*node.Impl
}

type resettable struct {
	*node.Impl
	rErr string
}

func (r *resettable) ResetCfg(_ context.Context) error {
	if r.rErr != "" {
		return fmt.Errorf(r.rErr)
	}
	return nil
}

type notResettable struct {
	*node.Impl
}

type certable struct {
	*node.Impl
	proto *tpb.Node
	gErr  string
}

func (c *certable) GetProto() *tpb.Node {
	return c.proto
}

func (c *certable) GenerateSelfSigned(_ context.Context) error {
	if c.gErr != "" {
		return fmt.Errorf(c.gErr)
	}
	return nil
}

type notCertable struct {
	*node.Impl
	proto *tpb.Node
}

func (nc *notCertable) GetProto() *tpb.Node {
	return nc.proto
}

func TestNew(t *testing.T) {
	node.Vendor(tpb.Vendor(1001), NewConfigurable)
	node.Vendor(tpb.Vendor(1006), NewLoopbackable)
	tf, err := tfake.NewSimpleClientset()
	if err != nil {
		t.Fatalf("cannot create fake topology clientset: %v", err)
	}
	opts := []Option{
		WithClusterConfig(&rest.Config{}),
		WithKubeClient(kfake.NewSimpleClientset()),
		WithTopoClient(tf),
	}
	tests := []struct {
		desc      string
		topo      *tpb.Topology
		wantNodes []string
		wantErr   string
	}{{
		desc: "success",
		topo: &tpb.Topology{
			Nodes: []*tpb.Node{
				{
					Name:   "r1",
					Vendor: tpb.Vendor(1001),
					Services: map[uint32]*tpb.Service{
						1000: {
							Name: "ssh",
						},
					},
				},
				{
					Name:   "r2",
					Vendor: tpb.Vendor(1001),
					Services: map[uint32]*tpb.Service{
						2000: {
							Name: "grpc",
						},
						3000: {
							Name: "gnmi",
						},
					},
				},
			},
			Links: []*tpb.Link{
				{
					ANode: "r1",
					AInt:  "eth1",
					ZNode: "r2",
					ZInt:  "eth1",
				},
			},
		},
		wantNodes: []string{"r1", "r2"},
	}, {
		desc:    "nil topo",
		wantErr: "topology cannot be nil",
	}, {
		desc: "load err - missing a node",
		topo: &tpb.Topology{
			Nodes: []*tpb.Node{
				{
					Name:   "r2",
					Vendor: tpb.Vendor(1001),
					Services: map[uint32]*tpb.Service{
						2000: {
							Name: "grpc",
						},
						3000: {
							Name: "gnmi",
						},
					},
				},
			},
			Links: []*tpb.Link{
				{
					ANode: "r1",
					AInt:  "eth1",
					ZNode: "r2",
					ZInt:  "eth1",
				},
			},
		},
		wantErr: "missing node",
	}, {
		desc: "load err - missing z node",
		topo: &tpb.Topology{
			Nodes: []*tpb.Node{
				{
					Name:   "r1",
					Vendor: tpb.Vendor(1001),
					Services: map[uint32]*tpb.Service{
						1000: {
							Name: "ssh",
						},
					},
				},
			},
			Links: []*tpb.Link{
				{
					ANode: "r1",
					AInt:  "eth1",
					ZNode: "r2",
					ZInt:  "eth1",
				},
			},
		},
		wantErr: "missing node",
	}, {
		desc: "load err - a node already connected",
		topo: &tpb.Topology{
			Nodes: []*tpb.Node{
				{
					Name:   "r1",
					Vendor: tpb.Vendor(1001),
					Services: map[uint32]*tpb.Service{
						1000: {
							Name: "ssh",
						},
					},
				},
				{
					Name:   "r2",
					Vendor: tpb.Vendor(1001),
					Services: map[uint32]*tpb.Service{
						2000: {
							Name: "grpc",
						},
						3000: {
							Name: "gnmi",
						},
					},
				},
			},
			Links: []*tpb.Link{
				{
					ANode: "r1",
					AInt:  "eth1",
					ZNode: "r2",
					ZInt:  "eth1",
				},
				{
					ANode: "r1",
					AInt:  "eth1",
					ZNode: "r2",
					ZInt:  "eth2",
				},
			},
		},
		wantErr: "already connected",
	}, {
		desc: "load err - z node already connected",
		topo: &tpb.Topology{
			Nodes: []*tpb.Node{
				{
					Name:   "r1",
					Vendor: tpb.Vendor(1001),
					Services: map[uint32]*tpb.Service{
						1000: {
							Name: "ssh",
						},
					},
				},
				{
					Name:   "r2",
					Vendor: tpb.Vendor(1001),
					Services: map[uint32]*tpb.Service{
						2000: {
							Name: "grpc",
						},
						3000: {
							Name: "gnmi",
						},
					},
				},
			},
			Links: []*tpb.Link{
				{
					ANode: "r1",
					AInt:  "eth1",
					ZNode: "r2",
					ZInt:  "eth1",
				},
				{
					ANode: "r1",
					AInt:  "eth2",
					ZNode: "r2",
					ZInt:  "eth1",
				},
			},
		},
		wantErr: "already connected",
	}, {
		desc: "load err - load node",
		topo: &tpb.Topology{
			Nodes: []*tpb.Node{
				{
					Name:   "r1",
					Vendor: tpb.Vendor_UNKNOWN,
					Services: map[uint32]*tpb.Service{
						1000: {
							Name: "ssh",
						},
					},
				},
			},
		},
		wantErr: "failed to load",
	}, {
		desc: "link err - loopback",
		topo: &tpb.Topology{
			Nodes: []*tpb.Node{
				{
					Name:   "r1",
					Vendor: tpb.Vendor(1001),
					Services: map[uint32]*tpb.Service{
						1000: {
							Name: "ssh",
						},
					},
				},
			},
			Links: []*tpb.Link{
				{
					ANode: "r1",
					AInt:  "eth1",
					ZNode: "r1",
					ZInt:  "eth2",
				},
			},
		},
		wantErr: "invalid link",
	}, {
		desc: "loopback supported",
		topo: &tpb.Topology{
			Nodes: []*tpb.Node{
				{
					Name:   "r1",
					Vendor: tpb.Vendor(1006),
					Services: map[uint32]*tpb.Service{
						1000: {
							Name: "ssh",
						},
					},
				},
			},
			Links: []*tpb.Link{
				{
					ANode: "r1",
					AInt:  "eth1",
					ZNode: "r1",
					ZInt:  "eth2",
				},
			},
		},
		wantNodes: []string{"r1"},
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			m, err := New(tt.topo, opts...)
			if s := errdiff.Check(err, tt.wantErr); s != "" {
				t.Errorf("New() unexpected err: %s", s)
			}
			if err != nil {
				return
			}
			var got []string
			for n := range m.nodes {
				got = append(got, n)
			}
			if s := cmp.Diff(tt.wantNodes, got, cmpopts.SortSlices(func(a, b string) bool { return a < b })); s != "" {
				t.Errorf("New() nodes unexpected diff (-want +got):\n%s", s)
			}
		})
	}
}

type fakeMetricsReporter struct {
	reportStartErr, reportEndErr error
}

func (f *fakeMetricsReporter) Close() error {
	return nil
}

func (f *fakeMetricsReporter) ReportCreateTopologyStart(_ context.Context, _ *epb.Topology) (string, error) {
	return "fake-uuid", f.reportStartErr
}

func (f *fakeMetricsReporter) ReportCreateTopologyEnd(_ context.Context, _ string, _ error) error {
	return f.reportEndErr
}

func TestCreate(t *testing.T) {
	ctx := context.Background()

	origKindClusterIsKind := kindClusterIsKind
	defer func() {
		kindClusterIsKind = origKindClusterIsKind
	}()
	kindClusterIsKind = func() (bool, error) {
		return false, nil
	}

	origNewMetricsReporter := newMetricsReporter
	defer func() {
		newMetricsReporter = origNewMetricsReporter
	}()
	newMetricsReporter = func(_ context.Context, _, _ string) (metricsReporter, error) {
		return &fakeMetricsReporter{reportStartErr: errors.New("start err"), reportEndErr: errors.New("end err")}, nil
	}

	node.Vendor(tpb.Vendor(1002), NewConfigurable)
	tests := []struct {
		desc    string
		topo    *tpb.Topology
		timeout time.Duration
		opts    []Option
		wantErr string
	}{{
		desc: "success",
		topo: &tpb.Topology{
			Name: "test",
			Nodes: []*tpb.Node{
				{
					Name:   "r1",
					Vendor: tpb.Vendor(1002),
					Services: map[uint32]*tpb.Service{
						1000: {
							Name: "ssh",
						},
					},
					Config: &tpb.Config{},
				},
				{
					Name:   "r2",
					Vendor: tpb.Vendor(1002),
					Services: map[uint32]*tpb.Service{
						2000: {
							Name: "grpc",
						},
						3000: {
							Name: "gnmi",
						},
					},
					Config: &tpb.Config{},
				},
			},
			Links: []*tpb.Link{
				{
					ANode: "r1",
					AInt:  "eth1",
					ZNode: "r2",
					ZInt:  "eth1",
				},
			},
		},
	}, {
		desc: "success with hanging pod + timeout",
		topo: &tpb.Topology{
			Name: "test",
			Nodes: []*tpb.Node{
				{
					Name:   "hanging",
					Vendor: tpb.Vendor(1002),
					Services: map[uint32]*tpb.Service{
						2000: {
							Name: "grpc",
						},
						3000: {
							Name: "gnmi",
						},
					},
					Config: &tpb.Config{},
				},
			},
		},
		timeout: time.Second,
	}, {
		desc: "pod failed to start",
		topo: &tpb.Topology{
			Name: "test",
			Nodes: []*tpb.Node{
				{
					Name:   "bad",
					Vendor: tpb.Vendor(1002),
					Services: map[uint32]*tpb.Service{
						2000: {
							Name: "grpc",
						},
						3000: {
							Name: "gnmi",
						},
					},
					Config: &tpb.Config{},
				},
			},
		},
		wantErr: `Node "bad" (vendor: "1002", model: ""): Status FAILED`,
	}, {
		desc: "failed to report metrics, create still passes",
		opts: []Option{WithUsageReporting(true, "", "")},
		topo: &tpb.Topology{
			Name: "test",
			Nodes: []*tpb.Node{
				{
					Name:   "r1",
					Vendor: tpb.Vendor(1002),
					Services: map[uint32]*tpb.Service{
						1000: {
							Name: "ssh",
						},
					},
					Config: &tpb.Config{},
				},
				{
					Name:   "r2",
					Vendor: tpb.Vendor(1002),
					Services: map[uint32]*tpb.Service{
						2000: {
							Name: "grpc",
						},
						3000: {
							Name: "gnmi",
						},
					},
					Config: &tpb.Config{},
				},
			},
			Links: []*tpb.Link{
				{
					ANode: "r1",
					AInt:  "eth1",
					ZNode: "r2",
					ZInt:  "eth1",
				},
			},
		},
	}, {
		desc: "failed node validation",
		topo: &tpb.Topology{
			Name: "test",
			Nodes: []*tpb.Node{{
				Name:   "r1",
				Vendor: tpb.Vendor(1002),
				Services: map[uint32]*tpb.Service{
					1000: {
						Name: "ssh",
					},
				},
				Config:          &tpb.Config{},
				HostConstraints: []*tpb.HostConstraint{{Constraint: &tpb.HostConstraint_KernelConstraint{}}},
			}},
		},
		wantErr: "failed to validate node",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			tf, err := tfake.NewSimpleClientset()
			if err != nil {
				t.Fatalf("cannot create fake topology clientset: %v", err)
			}
			kf := kfake.NewSimpleClientset()
			kf.PrependReactor("get", "pods", func(action ktest.Action) (bool, runtime.Object, error) {
				gAction, ok := action.(ktest.GetAction)
				if !ok {
					return false, nil, nil
				}
				p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: gAction.GetName()}}
				switch p.Name {
				default:
					p.Status.Phase = corev1.PodRunning
					p.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
				case "bad":
					p.Status.Phase = corev1.PodFailed
				case "hanging":
					p.Status.Phase = corev1.PodPending
				}
				return true, p, nil
			})

			opts := []Option{
				WithClusterConfig(&rest.Config{}),
				WithKubeClient(kf),
				WithTopoClient(tf),
			}
			opts = append(opts, tt.opts...)
			m, err := New(tt.topo, opts...)
			if err != nil {
				t.Fatalf("New() failed to create new topology manager: %v", err)
			}
			err = m.Create(ctx, tt.timeout)
			if s := errdiff.Check(err, tt.wantErr); s != "" {
				t.Errorf("Create() unexpected err: %s", s)
			}
		})
	}
}

type fakeWatch struct {
	ch   chan watch.Event
	done chan struct{}
}

func (f *fakeWatch) Stop() {
	close(f.done)
}

func (f *fakeWatch) ResultChan() <-chan watch.Event {
	return f.ch
}

func TestDelete(t *testing.T) {
	ctx := context.Background()
	node.Vendor(tpb.Vendor(1003), NewConfigurable)

	failWatchEvents := []watch.Event{
		{
			Type: watch.Deleted,
			Object: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dne",
				},
			},
		},
		{
			Type: watch.Added,
			Object: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
		},
	}
	passWatchEvents := append(failWatchEvents,
		watch.Event{
			Type: watch.Deleted,
			Object: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
		},
	)

	origDeleteWatchTimeout := deleteWatchTimeout
	defer func() {
		deleteWatchTimeout = origDeleteWatchTimeout
	}()
	deleteWatchTimeout = time.Millisecond

	tests := []struct {
		desc        string
		topo        *tpb.Topology
		k8sObjects  []runtime.Object
		skipWait    bool
		watchEvents []watch.Event
		wantErr     string
	}{{
		desc: "delete a non-existent topo",
		topo: &tpb.Topology{
			Name: "test",
			Nodes: []*tpb.Node{
				{
					Name:   "r1",
					Vendor: tpb.Vendor(1003),
					Services: map[uint32]*tpb.Service{
						1000: {
							Name: "ssh",
						},
					},
				},
				{
					Name:   "r2",
					Vendor: tpb.Vendor(1003),
					Services: map[uint32]*tpb.Service{
						2000: {
							Name: "grpc",
						},
						3000: {
							Name: "gnmi",
						},
					},
				},
			},
			Links: []*tpb.Link{
				{
					ANode: "r1",
					AInt:  "eth1",
					ZNode: "r2",
					ZInt:  "eth1",
				},
			},
		},
		skipWait: true,
		wantErr:  "does not exist in cluster",
	}, {
		desc: "delete an existing topo",
		topo: &tpb.Topology{
			Name: "test",
			Nodes: []*tpb.Node{
				{
					Name:   "r1",
					Vendor: tpb.Vendor(1003),
					Services: map[uint32]*tpb.Service{
						1000: {
							Name: "ssh",
						},
					},
				},
				{
					Name:   "r2",
					Vendor: tpb.Vendor(1003),
					Services: map[uint32]*tpb.Service{
						2000: {
							Name: "grpc",
						},
						3000: {
							Name: "gnmi",
						},
					},
				},
			},
			Links: []*tpb.Link{
				{
					ANode: "r1",
					AInt:  "eth1",
					ZNode: "r2",
					ZInt:  "eth1",
				},
			},
		},
		k8sObjects: []runtime.Object{
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r1",
					Namespace: "test",
				},
			},
		},
		skipWait: true,
	}, {
		desc: "delete with watch",
		topo: &tpb.Topology{
			Name: "test",
			Nodes: []*tpb.Node{
				{
					Name:   "r1",
					Vendor: tpb.Vendor(1003),
					Services: map[uint32]*tpb.Service{
						1000: {
							Name: "ssh",
						},
					},
				},
				{
					Name:   "r2",
					Vendor: tpb.Vendor(1003),
					Services: map[uint32]*tpb.Service{
						2000: {
							Name: "grpc",
						},
						3000: {
							Name: "gnmi",
						},
					},
				},
			},
			Links: []*tpb.Link{
				{
					ANode: "r1",
					AInt:  "eth1",
					ZNode: "r2",
					ZInt:  "eth1",
				},
			},
		},
		k8sObjects: []runtime.Object{
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r1",
					Namespace: "test",
				},
			},
		},
		watchEvents: passWatchEvents,
	}, {
		desc: "delete with watch - bad events",
		topo: &tpb.Topology{
			Name: "test",
			Nodes: []*tpb.Node{
				{
					Name:   "r1",
					Vendor: tpb.Vendor(1003),
					Services: map[uint32]*tpb.Service{
						1000: {
							Name: "ssh",
						},
					},
				},
				{
					Name:   "r2",
					Vendor: tpb.Vendor(1003),
					Services: map[uint32]*tpb.Service{
						2000: {
							Name: "grpc",
						},
						3000: {
							Name: "gnmi",
						},
					},
				},
			},
			Links: []*tpb.Link{
				{
					ANode: "r1",
					AInt:  "eth1",
					ZNode: "r2",
					ZInt:  "eth1",
				},
			},
		},
		k8sObjects: []runtime.Object{
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r1",
					Namespace: "test",
				},
			},
		},
		watchEvents: failWatchEvents,
		wantErr:     "context canceled before namespace deleted",
	}, {
		desc: "delete without watch - bad events",
		topo: &tpb.Topology{
			Name: "test",
			Nodes: []*tpb.Node{
				{
					Name:   "r1",
					Vendor: tpb.Vendor(1003),
					Services: map[uint32]*tpb.Service{
						1000: {
							Name: "ssh",
						},
					},
				},
				{
					Name:   "r2",
					Vendor: tpb.Vendor(1003),
					Services: map[uint32]*tpb.Service{
						2000: {
							Name: "grpc",
						},
						3000: {
							Name: "gnmi",
						},
					},
				},
			},
			Links: []*tpb.Link{
				{
					ANode: "r1",
					AInt:  "eth1",
					ZNode: "r2",
					ZInt:  "eth1",
				},
			},
		},
		k8sObjects: []runtime.Object{
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r1",
					Namespace: "test",
				},
			},
		},
		skipWait:    true,
		watchEvents: failWatchEvents,
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			tf, err := tfake.NewSimpleClientset()
			if err != nil {
				t.Fatalf("cannot create fake topology clientset: %v", err)
			}
			kf := kfake.NewSimpleClientset(tt.k8sObjects...)
			kf.PrependWatchReactor("*", func(action ktest.Action) (bool, watch.Interface, error) {
				f := &fakeWatch{
					ch:   make(chan watch.Event, 1),
					done: make(chan struct{}),
				}
				go func() {
					for _, e := range tt.watchEvents {
						select {
						case f.ch <- e:
						case <-f.done:
							return
						}
					}
				}()
				return true, f, nil
			})
			opts := []Option{
				WithClusterConfig(&rest.Config{}),
				WithKubeClient(kf),
				WithTopoClient(tf),
				WithSkipDeleteWait(tt.skipWait),
			}
			m, err := New(tt.topo, opts...)
			if err != nil {
				t.Fatalf("New() failed to create new topology manager: %v", err)
			}
			err = m.Delete(ctx)
			if s := errdiff.Check(err, tt.wantErr); s != "" {
				t.Errorf("Delete() unexpected err: %s", s)
			}
		})
	}
}

func TestShow(t *testing.T) {
	ctx := context.Background()
	node.Vendor(tpb.Vendor(1004), NewConfigurable)
	topo := &tpb.Topology{
		Name: "test",
		Nodes: []*tpb.Node{
			{
				Name:   "r1",
				Vendor: tpb.Vendor(1004),
				Services: map[uint32]*tpb.Service{
					22: {
						Name: "ssh",
					},
				},
			},
			{
				Name:   "r2",
				Vendor: tpb.Vendor(1004),
				Services: map[uint32]*tpb.Service{
					9337: {
						Name: "grpc",
					},
					9339: {
						Name: "gnmi",
					},
				},
			},
			{
				Name:   "r3",
				Vendor: tpb.Vendor(1004),
			},
		},
	}

	wantTopo := proto.Clone(topo).(*tpb.Topology)
	wantTopo.Nodes[0].Services[22].Inside = 22
	wantTopo.Nodes[0].Services[22].InsideIp = "10.1.1.1"
	wantTopo.Nodes[0].Services[22].Outside = 22
	wantTopo.Nodes[0].Services[22].OutsideIp = "192.168.16.50"
	wantTopo.Nodes[0].Services[22].NodePort = 20001
	wantTopo.Nodes[1].Services[9337].Inside = 9337
	wantTopo.Nodes[1].Services[9337].InsideIp = "10.1.1.2"
	wantTopo.Nodes[1].Services[9337].Outside = 9337
	wantTopo.Nodes[1].Services[9337].OutsideIp = "192.168.16.51"
	wantTopo.Nodes[1].Services[9337].NodePort = 20002
	wantTopo.Nodes[1].Services[9339].Inside = 9339
	wantTopo.Nodes[1].Services[9339].InsideIp = "10.1.1.2"
	wantTopo.Nodes[1].Services[9339].Outside = 9339
	wantTopo.Nodes[1].Services[9339].OutsideIp = "192.168.16.51"
	wantTopo.Nodes[1].Services[9339].NodePort = 20003

	topoRemapPorts := proto.Clone(wantTopo).(*tpb.Topology)
	topoRemapPorts.Nodes[1].Services[9337].Inside = 9339

	wantTopoRemapPorts := proto.Clone(topoRemapPorts).(*tpb.Topology)

	tests := []struct {
		desc       string
		k8sObjects []runtime.Object
		want       *cpb.ShowTopologyResponse
		wantErr    string
	}{{
		desc: "success",
		k8sObjects: []runtime.Object{
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r1",
					Namespace: "test",
				},
				Status: corev1.PodStatus{
					Phase:      corev1.PodRunning,
					Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r2",
					Namespace: "test",
				},
				Status: corev1.PodStatus{
					Phase:      corev1.PodRunning,
					Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r3",
					Namespace: "test",
				},
				Status: corev1.PodStatus{
					Phase:      corev1.PodRunning,
					Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
				},
			},
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-r1",
					Namespace: "test",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.1.1.1",
					Type:      "LoadBalancer",
					Ports: []corev1.ServicePort{{
						Name:       "ssh",
						Protocol:   "TCP",
						Port:       22,
						TargetPort: intstr.FromInt(22),
						NodePort:   20001,
					}},
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{{
							IP: "192.168.16.50",
						}},
					},
				},
			},
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-r2",
					Namespace: "test",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.1.1.2",
					Type:      "LoadBalancer",
					Ports: []corev1.ServicePort{{
						Name:       "grpc",
						Protocol:   "TCP",
						Port:       9337,
						TargetPort: intstr.FromInt(9337),
						NodePort:   20002,
					}, {
						Name:       "gnmi",
						Protocol:   "TCP",
						Port:       9339,
						TargetPort: intstr.FromInt(9339),
						NodePort:   20003,
					}},
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{{
							IP: "192.168.16.51",
						}},
					},
				},
			},
		},
		want: &cpb.ShowTopologyResponse{
			State:    cpb.TopologyState_TOPOLOGY_STATE_RUNNING,
			Topology: wantTopo,
		},
	}, {
		desc: "success with remapped ports",
		k8sObjects: []runtime.Object{
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r1",
					Namespace: "test",
				},
				Status: corev1.PodStatus{
					Phase:      corev1.PodRunning,
					Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r2",
					Namespace: "test",
				},
				Status: corev1.PodStatus{
					Phase:      corev1.PodRunning,
					Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r3",
					Namespace: "test",
				},
				Status: corev1.PodStatus{
					Phase:      corev1.PodRunning,
					Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
				},
			},
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-r1",
					Namespace: "test",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.1.1.1",
					Type:      "LoadBalancer",
					Ports: []corev1.ServicePort{{
						Name:       "ssh",
						Protocol:   "TCP",
						Port:       22,
						TargetPort: intstr.FromInt(22),
						NodePort:   20001,
					}},
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{{
							IP: "192.168.16.50",
						}},
					},
				},
			},
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-r2",
					Namespace: "test",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.1.1.2",
					Type:      "LoadBalancer",
					Ports: []corev1.ServicePort{{
						Name:       "grpc",
						Protocol:   "TCP",
						Port:       9337,
						TargetPort: intstr.FromInt(9339),
						NodePort:   20002,
					}, {
						Name:       "gnmi",
						Protocol:   "TCP",
						Port:       9339,
						TargetPort: intstr.FromInt(9339),
						NodePort:   20003,
					}},
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{{
							IP: "192.168.16.51",
						}},
					},
				},
			},
		},
		want: &cpb.ShowTopologyResponse{
			State:    cpb.TopologyState_TOPOLOGY_STATE_RUNNING,
			Topology: wantTopoRemapPorts,
		},
	}, {
		desc: "no pods",
		k8sObjects: []runtime.Object{
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-r1",
					Namespace: "test",
				},
			},
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-r2",
					Namespace: "test",
				},
			},
		},
		wantErr: "could not get pods",
	}, {
		desc: "no services",
		k8sObjects: []runtime.Object{
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r1",
					Namespace: "test",
				},
				Status: corev1.PodStatus{
					Phase:      corev1.PodRunning,
					Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r2",
					Namespace: "test",
				},
				Status: corev1.PodStatus{
					Phase:      corev1.PodRunning,
					Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r3",
					Namespace: "test",
				},
				Status: corev1.PodStatus{
					Phase:      corev1.PodRunning,
					Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
				},
			},
		},
		wantErr: "could not get services",
	}, {
		desc: "success - loading",
		k8sObjects: []runtime.Object{
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r1",
					Namespace: "test",
				},
				Status: corev1.PodStatus{Phase: corev1.PodPending},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r2",
					Namespace: "test",
				},
				Status: corev1.PodStatus{
					Phase:      corev1.PodRunning,
					Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r3",
					Namespace: "test",
				},
				Status: corev1.PodStatus{
					Phase:      corev1.PodRunning,
					Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
				},
			},
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-r1",
					Namespace: "test",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.1.1.1",
					Type:      "LoadBalancer",
					Ports: []corev1.ServicePort{{
						Name:       "ssh",
						Protocol:   "TCP",
						Port:       22,
						TargetPort: intstr.FromInt(22),
						NodePort:   20001,
					}},
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{{
							IP: "192.168.16.50",
						}},
					},
				},
			},
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-r2",
					Namespace: "test",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.1.1.2",
					Type:      "LoadBalancer",
					Ports: []corev1.ServicePort{{
						Name:       "grpc",
						Protocol:   "TCP",
						Port:       9337,
						TargetPort: intstr.FromInt(9337),
						NodePort:   20002,
					}, {
						Name:       "gnmi",
						Protocol:   "TCP",
						Port:       9339,
						TargetPort: intstr.FromInt(9339),
						NodePort:   20003,
					}},
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{{
							IP: "192.168.16.51",
						}},
					},
				},
			},
		},
		want: &cpb.ShowTopologyResponse{
			State:    cpb.TopologyState_TOPOLOGY_STATE_CREATING,
			Topology: wantTopo,
		},
	}, {
		desc: "success - unhealthy",
		k8sObjects: []runtime.Object{
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r1",
					Namespace: "test",
				},
				Status: corev1.PodStatus{Phase: corev1.PodFailed},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r2",
					Namespace: "test",
				},
				Status: corev1.PodStatus{
					Phase:      corev1.PodRunning,
					Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r3",
					Namespace: "test",
				},
				Status: corev1.PodStatus{
					Phase:      corev1.PodRunning,
					Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
				},
			},
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-r1",
					Namespace: "test",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.1.1.1",
					Type:      "LoadBalancer",
					Ports: []corev1.ServicePort{{
						Name:       "ssh",
						Protocol:   "TCP",
						Port:       22,
						TargetPort: intstr.FromInt(22),
						NodePort:   20001,
					}},
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{{
							IP: "192.168.16.50",
						}},
					},
				},
			},
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-r2",
					Namespace: "test",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.1.1.2",
					Type:      "LoadBalancer",
					Ports: []corev1.ServicePort{{
						Name:       "grpc",
						Protocol:   "TCP",
						Port:       9337,
						TargetPort: intstr.FromInt(9337),
						NodePort:   20002,
					}, {
						Name:       "gnmi",
						Protocol:   "TCP",
						Port:       9339,
						TargetPort: intstr.FromInt(9339),
						NodePort:   20003,
					}},
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{{
							IP: "192.168.16.51",
						}},
					},
				},
			},
		},
		want: &cpb.ShowTopologyResponse{
			State:    cpb.TopologyState_TOPOLOGY_STATE_ERROR,
			Topology: wantTopo,
		},
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			tf, err := tfake.NewSimpleClientset()
			if err != nil {
				t.Fatalf("cannot create fake topology clientset: %v", err)
			}
			opts := []Option{
				WithClusterConfig(&rest.Config{}),
				WithKubeClient(kfake.NewSimpleClientset(tt.k8sObjects...)),
				WithTopoClient(tf),
			}
			tTopo := proto.Clone(topo).(*tpb.Topology)
			m, err := New(tTopo, opts...)
			if err != nil {
				t.Fatalf("New() failed to create new topology manager: %v", err)
			}
			got, err := m.Show(ctx)
			if s := errdiff.Check(err, tt.wantErr); s != "" {
				t.Fatalf("Show() unexpected err: %s", s)
			}
			if tt.wantErr != "" {
				return
			}
			if s := cmp.Diff(tt.want, got, protocmp.Transform()); s != "" {
				t.Fatalf("Show() unexpected diff (-want +got):\n%s", s)
			}
		})
	}
}

func TestResources(t *testing.T) {
	ctx := context.Background()
	node.Vendor(tpb.Vendor(1005), NewConfigurable)
	topo := &tpb.Topology{
		Name: "test",
		Nodes: []*tpb.Node{
			{
				Name:   "r1",
				Vendor: tpb.Vendor(1005),
				Services: map[uint32]*tpb.Service{
					1000: {
						Name: "ssh",
					},
				},
			},
			{
				Name:   "r2",
				Vendor: tpb.Vendor(1005),
				Services: map[uint32]*tpb.Service{
					2000: {
						Name: "grpc",
					},
					3000: {
						Name: "gnmi",
					},
				},
			},
		},
	}
	tests := []struct {
		desc        string
		k8sObjects  []runtime.Object
		topoObjects []runtime.Object
		want        *Resources
		wantErr     string
	}{{
		desc: "success",
		k8sObjects: []runtime.Object{
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r1",
					Namespace: "test",
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r2",
					Namespace: "test",
				},
			},
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-r1",
					Namespace: "test",
				},
			},
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-r2",
					Namespace: "test",
				},
			},
		},
		topoObjects: []runtime.Object{
			&topologyv1.Topology{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "t1",
					Namespace: "test",
				},
			},
		},
		want: &Resources{
			Pods: map[string][]*corev1.Pod{
				"r1": {{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "r1",
						Namespace: "test",
					},
				}},
				"r2": {{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "r2",
						Namespace: "test",
					},
				}},
			},
			Services: map[string][]*corev1.Service{
				"r1": {{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-r1",
						Namespace: "test",
					},
				}},
				"r2": {{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-r2",
						Namespace: "test",
					},
				}},
			},
			ConfigMaps: map[string]*corev1.ConfigMap{},
			Topologies: map[string]*topologyv1.Topology{
				"t1": {
					TypeMeta: metav1.TypeMeta{
						Kind:       "Topology",
						APIVersion: "networkop.co.uk/v1beta1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "t1",
						Namespace: "test",
					},
				},
			},
		},
	}, {
		desc: "no pods",
		k8sObjects: []runtime.Object{
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-r1",
					Namespace: "test",
				},
			},
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-r2",
					Namespace: "test",
				},
			},
		},
		wantErr: "could not get pods",
	}, {
		desc: "no services",
		k8sObjects: []runtime.Object{
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r1",
					Namespace: "test",
				},
				Status: corev1.PodStatus{Phase: corev1.PodRunning},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "r2",
					Namespace: "test",
				},
				Status: corev1.PodStatus{Phase: corev1.PodRunning},
			},
		},
		wantErr: "could not get services",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			tf, err := tfake.NewSimpleClientset(tt.topoObjects...)
			if err != nil {
				t.Fatalf("cannot create fake topology clientset: %v", err)
			}
			opts := []Option{
				WithClusterConfig(&rest.Config{}),
				WithKubeClient(kfake.NewSimpleClientset(tt.k8sObjects...)),
				WithTopoClient(tf),
			}
			tTopo := proto.Clone(topo).(*tpb.Topology)
			m, err := New(tTopo, opts...)
			if err != nil {
				t.Fatalf("New() failed to create new topology manager: %v", err)
			}
			got, err := m.Resources(ctx)
			if s := errdiff.Check(err, tt.wantErr); s != "" {
				t.Errorf("Resources() unexpected err: %s", s)
			}
			if s := cmp.Diff(tt.want, got); s != "" {
				t.Errorf("Resources() unexpected diff (-want +got):\n%s", s)
			}
		})
	}
}

func TestNodes(t *testing.T) {
	aNode := &configurable{}
	bNode := &configurable{}
	cNode := &configurable{}
	tests := []struct {
		desc string
		want map[string]node.Node
	}{{
		desc: "non-zero nodes",
		want: map[string]node.Node{
			"a": aNode,
			"b": bNode,
			"c": cNode,
		},
	}, {
		desc: "zero nodes",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			m := &Manager{nodes: tt.want}
			got := m.Nodes()
			if s := cmp.Diff(tt.want, got); s != "" {
				t.Errorf("Nodes() unexpected diff: %s", s)
			}
		})
	}
}

func TestConfigPush(t *testing.T) {
	m := &Manager{
		nodes: map[string]node.Node{
			"configurable":     &configurable{},
			"not_configurable": &notConfigurable{},
		},
	}
	tests := []struct {
		desc    string
		name    string
		cfg     io.Reader
		wantErr string
	}{{
		desc: "configurable good config",
		name: "configurable",
		cfg:  bytes.NewReader([]byte("good config")),
	}, {
		desc:    "configurable bad config",
		name:    "configurable",
		cfg:     bytes.NewReader([]byte("error")),
		wantErr: "error",
	}, {
		desc:    "not configurable",
		name:    "not_configurable",
		wantErr: "does not implement ConfigPusher interface",
	}, {
		desc:    "node not found",
		name:    "dne",
		wantErr: "not found",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			err := m.ConfigPush(context.Background(), tt.name, tt.cfg)
			if s := errdiff.Check(err, tt.wantErr); s != "" {
				t.Errorf("ConfigPush() unexpected error: %s", s)
			}
		})
	}
}

func TestResetCfg(t *testing.T) {
	m := &Manager{
		nodes: map[string]node.Node{
			"resettable":     &resettable{},
			"resettable_err": &resettable{rErr: "failed to reset"},
			"not_resettable": &notResettable{},
		},
	}
	tests := []struct {
		desc    string
		name    string
		wantErr string
	}{{
		desc: "resettable",
		name: "resettable",
	}, {
		desc:    "resettable failure",
		name:    "resettable_err",
		wantErr: "failed to reset",
	}, {
		desc:    "not resettable",
		name:    "not_resettable",
		wantErr: "does not implement Resetter interface",
	}, {
		desc:    "node not found",
		name:    "dne",
		wantErr: "not found",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			err := m.ResetCfg(context.Background(), tt.name)
			if s := errdiff.Check(err, tt.wantErr); s != "" {
				t.Errorf("ResetCfg() unexpected error: %s", s)
			}
		})
	}
}

func TestGenerateSelfSigned(t *testing.T) {
	m := &Manager{
		nodes: map[string]node.Node{
			"certable": &certable{
				proto: &tpb.Node{
					Config: &tpb.Config{
						Cert: &tpb.CertificateCfg{
							Config: &tpb.CertificateCfg_SelfSigned{},
						},
					},
				},
			},
			"certable_err": &certable{
				gErr: "failed to generate certs",
				proto: &tpb.Node{
					Config: &tpb.Config{
						Cert: &tpb.CertificateCfg{
							Config: &tpb.CertificateCfg_SelfSigned{},
						},
					},
				},
			},
			"not_certable": &notCertable{
				proto: &tpb.Node{
					Config: &tpb.Config{
						Cert: &tpb.CertificateCfg{
							Config: &tpb.CertificateCfg_SelfSigned{},
						},
					},
				},
			},
			"no_info": &certable{},
		},
	}
	tests := []struct {
		desc    string
		name    string
		wantErr string
	}{{
		desc: "certable",
		name: "certable",
	}, {
		desc:    "certable failure",
		name:    "certable_err",
		wantErr: "failed to generate certs",
	}, {
		desc:    "not certable",
		name:    "not_certable",
		wantErr: "does not implement Certer interface",
	}, {
		desc: "no cert info",
		name: "no_info",
	}, {
		desc:    "node not found",
		name:    "dne",
		wantErr: "not found",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			err := m.GenerateSelfSigned(context.Background(), tt.name)
			if s := errdiff.Check(err, tt.wantErr); s != "" {
				t.Errorf("GenerateSelfSigned() unexpected error: %s", s)
			}
		})
	}
}

func TestStateMap(t *testing.T) {
	type nodeInfo struct {
		name  string
		phase node.Status
	}

	tests := []struct {
		desc  string
		nodes []*nodeInfo
		want  cpb.TopologyState
	}{{
		desc: "no nodes",
		want: cpb.TopologyState_TOPOLOGY_STATE_UNSPECIFIED,
	}, {
		desc: "one node failed",
		nodes: []*nodeInfo{
			{"n1", node.StatusFailed},
			{"n2", node.StatusRunning},
			{"n3", node.StatusRunning},
		},
		want: cpb.TopologyState_TOPOLOGY_STATE_ERROR,
	}, {
		desc: "one node failed with one node pending",
		nodes: []*nodeInfo{
			{"n1", node.StatusFailed},
			{"n2", node.StatusRunning},
			{"n3", node.StatusRunning},
		},
		want: cpb.TopologyState_TOPOLOGY_STATE_ERROR,
	}, {
		desc: "one node failed, one node pending, one node unknown",
		nodes: []*nodeInfo{
			{"n1", node.StatusFailed},
			{"n2", node.StatusPending},
			{"n3", node.StatusUnknown},
		},
		want: cpb.TopologyState_TOPOLOGY_STATE_ERROR,
	}, {
		desc: "all nodes failed",
		nodes: []*nodeInfo{
			{"n1", node.StatusFailed},
			{"n2", node.StatusFailed},
			{"n3", node.StatusFailed},
		},
		want: cpb.TopologyState_TOPOLOGY_STATE_ERROR,
	}, {
		desc: "one node pending",
		nodes: []*nodeInfo{
			{"n1", node.StatusPending},
			{"n2", node.StatusRunning},
			{"n3", node.StatusRunning},
		},
		want: cpb.TopologyState_TOPOLOGY_STATE_CREATING,
	},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			sm := &stateMap{}
			for _, n := range tt.nodes {
				sm.setNodeState(n.name, n.phase)
			}
			got := sm.topologyState()
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestUpdateServicePortName(t *testing.T) {
	tests := []struct {
		desc string
		key  uint32
		svc  *tpb.Service
		want *tpb.Service
	}{
		{
			desc: "services valid names with duplicates",
			svc: &tpb.Service{
				Name:   "gnmi",
				Names:  []string{"gnmi", "gribi", "gribi"},
				Inside: 9339,
			},
			want: &tpb.Service{
				Name:   "gnmi",
				Names:  []string{"gnmi", "gribi", "gribi"},
				Inside: 9339,
			},
		},
		{
			desc: "services valid names with empty name",
			svc: &tpb.Service{
				Names:  []string{"gnmi", "gribi"},
				Inside: 9339,
			},
			want: &tpb.Service{
				Name:   "gnmi",
				Names:  []string{"gnmi", "gribi"},
				Inside: 9339,
			},
		},
		{
			desc: "services valid names with empty string",
			svc: &tpb.Service{
				Name:   "gnmi",
				Names:  []string{"", "gribi"},
				Inside: 9339,
			},
			want: &tpb.Service{
				Name:   "gnmi",
				Names:  []string{"gribi"},
				Inside: 9339,
			},
		},
		{
			desc: "services empty name and names",
			svc: &tpb.Service{
				Inside: 9339,
			},
			key: uint32(1000),
			want: &tpb.Service{
				Name:   "port-1000",
				Inside: 9339,
			},
		},
		{
			desc: "services valid name with empty names",
			svc: &tpb.Service{
				Name:   "gnmi",
				Inside: 9339,
			},
			key: uint32(1000),
			want: &tpb.Service{
				Name:   "gnmi",
				Inside: 9339,
			},
		},
	}

	for _, tt := range tests {
		updateServicePortName(tt.svc, tt.key)
		if s := cmp.Diff(tt.svc, tt.want, protocmp.Transform()); s != "" {
			t.Fatalf("updateServicePortName() failed: %s \n\n got: %s", s, tt.svc)
		}
	}
}
