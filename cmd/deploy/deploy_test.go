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
package deploy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	dtypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/network"
	"github.com/golang/mock/gomock"
	"github.com/google/kne/cmd/deploy/mocks"
	"github.com/h-fam/errdiff"
	"github.com/kylelemons/godebug/diff"
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	fakecorev1 "k8s.io/client-go/kubernetes/typed/core/v1/fake"
	ktest "k8s.io/client-go/testing"
	"github.com/google/kne/cmd/deploy/exec"
)

var (
	invalidCluster = `
cluster:
  kind: InvalidCluster
  spec:
    name: kne
    recycle: True
    version: 0.11.1
    image: kindest/node:v1.22.0
ingress:
  kind: MetalLB
  spec:
    manifests: ../../manifests/metallb
    ip_count: 100
cni:
  kind: Meshnet
  spec:
    manifests: ../../manifests/meshnet/base
`
	invalidIngress = `
cluster:
  kind: Kind
  spec:
    name: kne
    recycle: True
    version: 0.11.1
    image: kindest/node:v1.22.0
ingress:
  kind: InvalidIngress
  spec:
    manifests: ../../manifests/metallb
    ip_count: 100
cni:
  kind: Meshnet
  spec:
    manifests: ../../manifests/meshnet/base
`
	invalidCNI = `
cluster:
  kind: Kind
  spec:
    name: kne
    recycle: True
    version: 0.11.1
    image: kindest/node:v1.22.0
ingress:
  kind: MetalLB
  spec:
    manifests: ../../manifests/metallb
    ip_count: 100
cni:
  kind: InvalidCNI
  spec:
    manifests: ../../manifests/meshnet/base`
)

func TestNew(t *testing.T) {
	c := New()
	if !strings.HasPrefix(c.Use, "deploy") {
		t.Fatalf("unexpected command object: got %q, want \"deploy\"", c.Use)
	}
}

func TestNewDeployment(t *testing.T) {
	tests := []struct {
		desc    string
		cfg     string
		path    string
		wantErr string
	}{{
		desc:    "invalid cluster",
		cfg:     invalidCluster,
		wantErr: "cluster type not supported",
	}, {
		desc:    "invalid ingress",
		cfg:     invalidIngress,
		wantErr: "ingress type not supported",
	}, {
		desc:    "invalid cni",
		cfg:     invalidCNI,
		wantErr: "CNI type not supported",
	}, {
		desc: "kind example",
		cfg:  "",
		path: "../../deploy/kne/kind.yaml",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if tt.path == "" {
				f, err := ioutil.TempFile("", "dtest")
				if err != nil {
					t.Fatalf("failed to create tempfile: %v", err)
				}
				if _, err := f.Write([]byte(tt.cfg)); err != nil {
					t.Fatalf("failed to create tempfile: %v", err)
				}
				if err := f.Close(); err != nil {
					t.Fatalf("failed to create tempfile: %v", err)
				}
				tt.path = f.Name()
				defer os.Remove(f.Name())
			}
			cfg, _, err := deploymentFromArg(tt.path)
			if err != nil {
				t.Fatalf("failed to unmarshal config: %v", err)
			}
			d, err := NewDeployment(cfg)
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if err != nil {
				return
			}
			t.Log(d)
		})
	}
}

func TestKindSpec(t *testing.T) {
	ctx := context.Background()
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	tests := []struct {
		desc        string
		k           *KindSpec
		execPathErr bool
		wantErr     string
		mockExpects func(m *mocks.Mockprovider)
	}{{
		desc: "create cluster with cli",
		k: &KindSpec{
			Name:   "test",
			execer: exec.NewFakeExecer(nil),
		},
	}, {
		desc: "create cluster with client",
		k: &KindSpec{
			Name:             "test",
			DeployWithClient: true,
		},
		mockExpects: func(m *mocks.Mockprovider) {
			m.EXPECT().Create("test", gomock.Any()).Return(nil)
		},
	}, {
		desc: "create cluster with recycle",
		k: &KindSpec{
			Name:    "test",
			Recycle: true,
			execer: exec.NewFakeExecer(nil),
		},
		mockExpects: func(m *mocks.Mockprovider) {
			m.EXPECT().List().Return([]string{"test1"}, nil)
		},
	}, {
		desc: "create cluster with recycle with client",
		k: &KindSpec{
			Name:             "test",
			Recycle:          true,
			DeployWithClient: true,
		},
		mockExpects: func(m *mocks.Mockprovider) {
			m.EXPECT().Create("test", gomock.Any()).Return(nil)
			m.EXPECT().List().Return([]string{"test1"}, nil)
		},
	}, {
		desc: "exists cluster with recycle",
		k: &KindSpec{
			Name:    "test",
			Recycle: true,
		},
		mockExpects: func(m *mocks.Mockprovider) {
			m.EXPECT().List().Return([]string{"test"}, nil)
		},
	}, {
		desc: "unable to find kind cli",
		k: &KindSpec{
			Name:   "test",
			execer: exec.NewFakeExecer(nil),
		},
		execPathErr: true,
		wantErr:     "install kind cli to deploy",
	}, {
		desc: "create cluster with cli fail",
		k: &KindSpec{
			Name:   "test",
			execer: exec.NewFakeExecer(errors.New("cmd failed")),
		},
		wantErr: "failed to create cluster using cli",
	}, {
		desc: "create cluster with client fail",
		k: &KindSpec{
			Name:             "test",
			DeployWithClient: true,
		},
		mockExpects: func(m *mocks.Mockprovider) {
			m.EXPECT().Create("test", gomock.Any()).Return(fmt.Errorf("create failed"))
		},
		wantErr: "failed to create cluster using kind client",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			m := mocks.NewMockprovider(mockCtrl)
			if tt.mockExpects != nil {
				tt.mockExpects(m)
			}
			newProvider = func() provider {
				return m
			}
			execLookPath = func(_ string) (string, error) {
				if tt.execPathErr {
					return "", errors.New("unable to find on path")
				}
				return "fakePath", nil
			}
			err := tt.k.Deploy(ctx)
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
		})
	}
}

type fakeWatch struct {
	e    []watch.Event
	ch   chan watch.Event
	done chan struct{}
}

func newFakeWatch(e []watch.Event) *fakeWatch {
	f := &fakeWatch{
		e:    e,
		ch:   make(chan watch.Event, 1),
		done: make(chan struct{}),
	}
	go func() {
		for len(f.e) != 0 {
			e := f.e[0]
			f.e = f.e[1:]
			select {
			case f.ch <- e:
			case <-f.done:
				return
			}
		}
	}()
	return f
}
func (f *fakeWatch) Stop() {
	close(f.done)
}

func (f *fakeWatch) ResultChan() <-chan watch.Event {
	return f.ch
}

//go:generate mockgen -destination=mocks/mock_dnetwork.go -package=mocks github.com/docker/docker/client  NetworkAPIClient

func TestMetalbSpec(t *testing.T) {
	nl := []dtypes.NetworkResource{{
		Name: "kind",
		IPAM: network.IPAM{
			Config: []network.IPAMConfig{{
				Subnet: "172.18.0.0/16",
			}, {
				Subnet: "127::0/64",
			}},
		},
	}, {
		Name: "docker",
		IPAM: network.IPAM{
			Config: []network.IPAMConfig{{
				Subnet: "1.1.1.1/16",
			}}},
	}}
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "metallb-system",
		},
	}
	tests := []struct {
		desc        string
		m           *MetalLBSpec
		wantCM      string
		dErr        string
		hErr        string
		ctx         context.Context
		mockExpects func(*mocks.MockNetworkAPIClient)
		mockKClient func(*fake.Clientset)
		k8sObjects  []runtime.Object
	}{{
		desc: "namespace error",
		m: &MetalLBSpec{
			IPCount: 20,
			execer:  exec.NewFakeExecer(errors.New("namespace error")),
		},
		dErr: "namespace error",
	}, {
		desc: "secret create",
		m: &MetalLBSpec{
			IPCount: 20,
			execer:  exec.NewFakeExecer(nil, nil),
		},
		mockKClient: func(k *fake.Clientset) {
			k.CoreV1().(*fakecorev1.FakeCoreV1).PrependReactor("get", "secrets", func(action ktest.Action) (handled bool, ret runtime.Object, err error) {
				return true, nil, fmt.Errorf("get secret error")
			})
			k.CoreV1().(*fakecorev1.FakeCoreV1).PrependReactor("create", "secrets", func(action ktest.Action) (handled bool, ret runtime.Object, err error) {
				return true, nil, fmt.Errorf("create secret error")
			})
		},
		dErr: "secret error",
	}, {
		desc: "metallb error",
		m: &MetalLBSpec{
			IPCount: 20,
			execer:  exec.NewFakeExecer(nil, errors.New("metallb error")),
		},
		k8sObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "metallb-system",
					Name:      "memberlist",
				},
			},
		},
		dErr: "metallb error",
	}, {
		desc: "canceled ctx",
		m: &MetalLBSpec{
			IPCount: 20,
			execer:  exec.NewFakeExecer(nil, nil),
		},
		k8sObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "metallb-system",
					Name:      "memberlist",
				},
			},
		},
		mockExpects: func(m *mocks.MockNetworkAPIClient) {
			m.EXPECT().NetworkList(gomock.Any(), gomock.Any()).Return(nl, nil)
		},
		ctx:  canceledCtx,
		hErr: "context canceled",
	}, {
		desc: "dclient error",
		m: &MetalLBSpec{
			IPCount: 20,
			execer:  exec.NewFakeExecer(nil, nil, nil),
		},
		mockExpects: func(m *mocks.MockNetworkAPIClient) {
			m.EXPECT().NetworkList(gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("dclient error"))
		},
		dErr: "dclient error",
	}, {
		desc: "valid deployment",
		m: &MetalLBSpec{
			IPCount: 20,
			execer:  exec.NewFakeExecer(nil, nil, nil),
		},
		wantCM: `address-pools:
    - name: default
      protocol: layer2
      addresses:
        - 172.18.0.50 - 172.18.0.70
`,
		mockExpects: func(m *mocks.MockNetworkAPIClient) {
			m.EXPECT().NetworkList(gomock.Any(), gomock.Any()).Return(nl, nil)
		},
		mockKClient: func(k *fake.Clientset) {
			reaction := func(action ktest.Action) (handled bool, ret watch.Interface, err error) {
				f := newFakeWatch([]watch.Event{{
					Type: watch.Added,
					Object: &appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "metallb-system",
						},
						Status: appsv1.DeploymentStatus{
							AvailableReplicas:   0,
							ReadyReplicas:       0,
							Replicas:            0,
							UnavailableReplicas: 1,
							UpdatedReplicas:     0,
						},
					},
				}, {
					Type: watch.Modified,
					Object: &appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "metallb-system",
						},
						Status: appsv1.DeploymentStatus{
							AvailableReplicas:   1,
							ReadyReplicas:       1,
							Replicas:            1,
							UnavailableReplicas: 0,
							UpdatedReplicas:     1,
						},
					},
				}})
				return true, f, nil
			}
			k.PrependWatchReactor("deployments", reaction)
		},
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			tt.k8sObjects = append(tt.k8sObjects, d)
			ki := fake.NewSimpleClientset(tt.k8sObjects...)
			if tt.mockKClient != nil {
				tt.mockKClient(ki)
			}
			tt.m.SetKClient(ki)
			if tt.mockExpects != nil {
				m := mocks.NewMockNetworkAPIClient(mockCtrl)
				tt.mockExpects(m)
				tt.m.dClient = m
			}
			err := tt.m.Deploy(context.Background())
			if s := errdiff.Substring(err, tt.dErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if err != nil {
				return
			}
			if tt.ctx == nil {
				tt.ctx = context.Background()
			}
			err = tt.m.Healthy(tt.ctx)
			if s := errdiff.Substring(err, tt.hErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if err != nil {
				return
			}
			cm, err := tt.m.kClient.CoreV1().ConfigMaps("metallb-system").Get(context.Background(), "config", metav1.GetOptions{})
			if err != nil {
				t.Fatalf("failed to get config-map: %v", err)
			}
			cText := cm.Data["config"]
			diff := diff.Diff(cText, tt.wantCM)
			if diff != "" {
				t.Fatalf("invalid configmap data: \n%s", diff)
			}
		})
	}
}

func TestMeshnet(t *testing.T) {
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	d := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "meshnet",
			Namespace: "meshnet",
		},
	}
	tests := []struct {
		desc string
		m    *MeshnetSpec
		dErr string
		hErr string
		ctx  context.Context
	}{{
		desc: "apply error cluster",
		m: &MeshnetSpec{
			execer: exec.NewFakeExecer(errors.New("apply error")),
		},
		dErr: "apply error",
	}, {
		desc: "canceled ctx",
		m: &MeshnetSpec{
			execer: exec.NewFakeExecer(nil),
		},
		ctx:  canceledCtx,
		hErr: "context canceled",
	}, {
		desc: "valid deployment",
		m: &MeshnetSpec{
			execer: exec.NewFakeExecer(nil),
		},
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			ki := fake.NewSimpleClientset(d)
			reaction := func(action ktest.Action) (handled bool, ret watch.Interface, err error) {
				f := newFakeWatch([]watch.Event{{
					Type: watch.Added,
					Object: &appsv1.DaemonSet{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "meshnet",
							Namespace: "meshnet",
						},
						Status: appsv1.DaemonSetStatus{
							NumberReady:            0,
							DesiredNumberScheduled: 1,
							NumberUnavailable:      1,
						},
					},
				}, {
					Type: watch.Modified,
					Object: &appsv1.DaemonSet{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "meshnet",
							Namespace: "meshnet",
						},
						Status: appsv1.DaemonSetStatus{
							NumberReady:            1,
							DesiredNumberScheduled: 1,
							NumberUnavailable:      0,
						},
					},
				}})
				return true, f, nil
			}
			ki.PrependWatchReactor("daemonsets", reaction)
			tt.m.SetKClient(ki)
			err := tt.m.Deploy(context.Background())
			if s := errdiff.Substring(err, tt.dErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if err != nil {
				return
			}
			if tt.ctx == nil {
				tt.ctx = context.Background()
			}
			err = tt.m.Healthy(tt.ctx)
			if s := errdiff.Substring(err, tt.hErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
		})
	}
}

func TestLogAdaptor(t *testing.T) {
	logger := log.New()
	b := bytes.NewBuffer([]byte{})
	logger.SetOutput(b)
	l := logAdapter{logger}
	msg := "message write"

	tests := []struct {
		desc string
		want string
		f    func()
	}{{
		desc: "no debug V(10).Info",
		want: "",
		f: func() {
			l.V(10).Info(msg)
		},
	}, {
		desc: "no debug V(10).Infof",
		want: "",
		f: func() {
			l.V(10).Infof("%s", msg)
		},
	}, {
		desc: "no debug V(0).Info",
		want: msg,
		f: func() {
			l.V(0).Info(msg)
		},
	}, {
		desc: "no debug V(0).Infof",
		want: msg,
		f: func() {
			l.V(0).Infof("%s", msg)
		},
	}, {
		desc: "debug V(10).Info",
		want: msg,
		f: func() {
			l.SetLevel(log.DebugLevel)
			l.V(10).Infof("%s", msg)
			l.SetLevel(log.InfoLevel)
		},
	}, {
		desc: "debug V(10).Infof",
		want: msg,
		f: func() {
			l.SetLevel(log.DebugLevel)
			l.V(10).Infof("%s", msg)
			l.SetLevel(log.InfoLevel)
		},
	}, {
		desc: "write error ",
		want: msg,
		f: func() {
			l.Error(msg)
		},
	}, {
		desc: "write errorf",
		want: msg,
		f: func() {
			l.Errorf("%s", msg)
		},
	}, {
		desc: "write warn",
		want: msg,
		f: func() {
			l.Warn(msg)
		},
	}, {
		desc: "write warnf",
		want: msg,
		f: func() {
			l.Warnf(msg)
		},
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			b.Reset()
			tt.f()
			if !strings.Contains(b.String(), tt.want) {
				t.Fatalf("V write failed: got %q, want %q", b.String(), tt.want)
			}
		})
	}
}
