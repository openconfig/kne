package deploy

import (
	"context"
	"fmt"
	"os"
	"testing"

	dtypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/network"
	"github.com/golang/mock/gomock"
	"github.com/google/go-cmp/cmp"
	"github.com/h-fam/errdiff"
	mfake "github.com/openconfig/kne/api/metallb/clientset/v1beta1/fake"
	"github.com/openconfig/kne/deploy/mocks"
	"github.com/openconfig/kne/os/exec"
	"github.com/pkg/errors"
	metallbv1 "go.universe.tf/metallb/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	fakecorev1 "k8s.io/client-go/kubernetes/typed/core/v1/fake"

	ktest "k8s.io/client-go/testing"
)

func TestKindSpec(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		desc        string
		k           *KindSpec
		execer      execerInterface
		vExecer     execerInterface
		execPathErr bool
		wantErr     string
	}{{
		desc: "create cluster with cli",
		k: &KindSpec{
			Name: "test",
		},
		execer: exec.NewFakeExecer(nil),
	}, {
		desc: "create cluster with recycle",
		k: &KindSpec{
			Name:    "test",
			Recycle: true,
		},
		execer: exec.NewFakeExecer(nil, nil),
	}, {
		desc: "exists cluster with recycle",
		k: &KindSpec{
			Name:    "test",
			Recycle: true,
		},
		execer: exec.NewFakeExecer(nil),
	}, {
		desc: "unable to find kind cli",
		k: &KindSpec{
			Name: "test",
		},
		execPathErr: true,
		wantErr:     `install dependency "kind" to deploy`,
	}, {
		desc: "create cluster fail",
		k: &KindSpec{
			Name: "test",
		},
		execer:  exec.NewFakeExecer(errors.New("cmd failed")),
		wantErr: "failed to create cluster",
	}, {
		desc: "create cluster with GAR - 1 reg",
		k: &KindSpec{
			Name:                     "test",
			GoogleArtifactRegistries: []string{"us-west1-docker.pkg.dev"},
		},
		execer: exec.NewFakeExecer(nil, nil, nil, nil, nil, nil),
	}, {
		desc: "create cluster with GAR - 2 regs",
		k: &KindSpec{
			Name:                     "test",
			GoogleArtifactRegistries: []string{"us-west1-docker.pkg.dev", "us-central1-docker.pkg.dev"},
		},
		execer: exec.NewFakeExecer(nil, nil, nil, nil, nil, nil, nil),
	}, {
		desc: "create cluster with GAR - failed to get access token",
		k: &KindSpec{
			Name:                     "test",
			GoogleArtifactRegistries: []string{"us-west1-docker.pkg.dev"},
		},
		execer:  exec.NewFakeExecer(nil, errors.New("failed to get access token")),
		wantErr: "failed to get access token",
	}, {
		desc: "create cluster with GAR - failed docker login",
		k: &KindSpec{
			Name:                     "test",
			GoogleArtifactRegistries: []string{"us-west1-docker.pkg.dev"},
		},
		execer:  exec.NewFakeExecer(nil, nil, errors.New("failed to login to docker")),
		wantErr: "failed to login to docker",
	}, {
		desc: "create cluster with GAR - failed to get nodes",
		k: &KindSpec{
			Name:                     "test",
			GoogleArtifactRegistries: []string{"us-west1-docker.pkg.dev"},
		},
		execer:  exec.NewFakeExecer(nil, nil, nil, errors.New("failed to get nodes")),
		wantErr: "failed to get nodes",
	}, {
		desc: "create cluster with GAR - failed to cp config to node",
		k: &KindSpec{
			Name:                     "test",
			GoogleArtifactRegistries: []string{"us-west1-docker.pkg.dev"},
		},
		execer:  exec.NewFakeExecer(nil, nil, nil, nil, errors.New("failed to cp config to node")),
		wantErr: "failed to cp config to node",
	}, {
		desc: "create cluster with GAR - failed to restart kubelet",
		k: &KindSpec{
			Name:                     "test",
			GoogleArtifactRegistries: []string{"us-west1-docker.pkg.dev"},
		},
		execer:  exec.NewFakeExecer(nil, nil, nil, nil, nil, errors.New("failed to restart kubelet")),
		wantErr: "failed to restart kubelet",
	}, {
		desc: "create cluster load containers",
		k: &KindSpec{
			Name: "test",
			ContainerImages: map[string]string{
				"docker": "local",
				"gar":    "docker",
			},
			Image:          "foo:latest",
			Retain:         true,
			Wait:           180,
			Kubecfg:        "kubecfg.yaml",
			KindConfigFile: "test.yaml",
		},
		execer: exec.NewFakeExecer(nil, nil, nil, nil, nil, nil, nil),
	}, {
		desc: "create cluster load containers additional manifests",
		k: &KindSpec{
			Name: "test",
			ContainerImages: map[string]string{
				"docker": "local",
				"gar":    "docker",
			},
			Image:          "foo:latest",
			Retain:         true,
			Wait:           180,
			Kubecfg:        "kubecfg.yaml",
			KindConfigFile: "test.yaml",
			AdditionalManifests: []string{
				"bar:latest",
				"baz:latest",
			},
		},
		execer: exec.NewFakeExecer(nil, nil, nil, nil, nil, nil, nil, nil, nil),
	}, {
		desc: "failed create cluster load containers additional manifests",
		k: &KindSpec{
			Name: "test",
			ContainerImages: map[string]string{
				"docker": "local",
				"gar":    "docker",
			},
			Image:          "foo:latest",
			Retain:         true,
			Wait:           180,
			Kubecfg:        "kubecfg.yaml",
			KindConfigFile: "test.yaml",
			AdditionalManifests: []string{
				"bar:latest",
				"baz:latest",
			},
		},
		execer:  exec.NewFakeExecer(nil, nil, fmt.Errorf("manifest error")),
		wantErr: "failed to deploy manifest",
	}, {
		desc: "create cluster load containers - failed pull",
		k: &KindSpec{
			Name: "test",
			ContainerImages: map[string]string{
				"docker": "local",
			},
		},
		execer:  exec.NewFakeExecer(nil, errors.New("unable to pull")),
		wantErr: "failed to pull",
	}, {
		desc: "create cluster load containers - failed tag",
		k: &KindSpec{
			Name: "test",
			ContainerImages: map[string]string{
				"docker": "local",
			},
		},
		execer:  exec.NewFakeExecer(nil, nil, errors.New("unable to tag")),
		wantErr: "failed to tag",
	}, {
		desc: "create cluster load containers - failed load",
		k: &KindSpec{
			Name: "test",
			ContainerImages: map[string]string{
				"docker": "local",
			},
		},
		execer:  exec.NewFakeExecer(nil, nil, nil, errors.New("unable to load")),
		wantErr: "failed to load",
	}, {
		desc: "create cluster load containers - failed empty key",
		k: &KindSpec{
			Name: "test",
			ContainerImages: map[string]string{
				"": "local",
			},
		},
		execer:  exec.NewFakeExecer(nil),
		wantErr: "source container must not be empty",
	}, {
		desc: "create cluster load containers - success empty value",
		k: &KindSpec{
			Name: "test",
			ContainerImages: map[string]string{
				"docker": "",
			},
		},
		execer: exec.NewFakeExecer(nil, nil, nil, nil),
	}, {
		desc: "failed kind version - no prefix",
		k: &KindSpec{
			Name:    "test",
			Version: "0.1.15",
		},
		wantErr: "missing prefix on major version",
	}, {
		desc: "failed kind version - invalid format",
		k: &KindSpec{
			Name:    "test",
			Version: "versionfoo",
		},
		wantErr: "failed to get versions from",
	}, {
		desc: "failed kind version - invalid major",
		k: &KindSpec{
			Name:    "test",
			Version: "vr.1.1",
		},
		wantErr: "failed to convert major version",
	}, {
		desc: "failed kind version - invalid minor",
		k: &KindSpec{
			Name:    "test",
			Version: "v0.foo.15",
		},
		wantErr: "failed to convert minor version",
	}, {
		desc: "failed kind version - invalid patch",
		k: &KindSpec{
			Name:    "test",
			Version: "v0.1.foo",
		},
		wantErr: "failed to convert patch version",
	}, {
		desc: "failed kind version less check",
		k: &KindSpec{
			Name:    "test",
			Version: "v0.15.0",
		},
		vExecer: exec.NewFakeExecerWithIO(vOut, vOut, exec.Response{Stdout: "kind v0.14.0 go1.18.2 linux/amd64"}),
		wantErr: "kind version check failed: got v0.14.0, want v0.15.0",
	}, {
		desc: "failed kind version exec fail",
		k: &KindSpec{
			Name:    "test",
			Version: "v0.15.0",
		},
		vExecer: exec.NewFakeExecerWithIO(vOut, vOut, exec.Response{Err: fmt.Errorf("exec error")}),
		wantErr: "failed to get kind version: exec error",
	}, {
		desc: "failed kind version parse",
		k: &KindSpec{
			Name:    "test",
			Version: "v0.15.0",
		},
		vExecer: exec.NewFakeExecerWithIO(vOut, vOut, exec.Response{Stdout: "kind "}),
		wantErr: "failed to parse kind version from",
	}, {
		desc: "failed kind version got fail parse",
		k: &KindSpec{
			Name:    "test",
			Version: "v0.15.0",
		},
		vExecer: exec.NewFakeExecerWithIO(vOut, vOut, exec.Response{Stdout: "kind 0.14.0 go1.18.2 linux/amd64"}),
		wantErr: "kind version check failed: missing prefix on major version",
	}, {
		desc: "kind version pass",
		k: &KindSpec{
			Name:    "test",
			Version: "v0.15.0",
		},
		execer:  exec.NewFakeExecer(nil, nil, nil, nil, nil, nil, nil),
		vExecer: exec.NewFakeExecerWithIO(vOut, vOut, exec.Response{Stdout: "kind v0.15.0 go1.18.2 linux/amd64"}),
	}, {
		desc: "kind version pass - major",
		k: &KindSpec{
			Name:    "test",
			Version: "v0.15.0",
		},
		execer:  exec.NewFakeExecer(nil, nil, nil, nil, nil, nil, nil),
		vExecer: exec.NewFakeExecerWithIO(vOut, vOut, exec.Response{Stdout: "kind v1.15.0 go1.18.2 linux/amd64"}),
	}, {
		desc: "kind version pass - patch",
		k: &KindSpec{
			Name:    "test",
			Version: "v0.15.0",
		},
		execer:  exec.NewFakeExecer(nil, nil, nil, nil, nil, nil, nil),
		vExecer: exec.NewFakeExecerWithIO(vOut, vOut, exec.Response{Stdout: "kind v0.15.1 go1.18.2 linux/amd64"}),
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if tt.execer != nil {
				execer = tt.execer
			}
			if tt.vExecer != nil {
				vExec = tt.vExecer
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

func TestMetalLBSpec(t *testing.T) {
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
		execer      execerInterface
		wantConfig  *metallbv1.IPAddressPool
		dErr        string
		hErr        string
		ctx         context.Context
		mockExpects func(*mocks.MockNetworkAPIClient)
		mockKClient func(*fake.Clientset)
		k8sObjects  []runtime.Object
		mObjects    []runtime.Object
	}{{
		desc: "namespace error",
		m: &MetalLBSpec{
			IPCount: 20,
		},
		execer: exec.NewFakeExecer(errors.New("namespace error")),
		dErr:   "namespace error",
	}, {
		desc: "secret create",
		m: &MetalLBSpec{
			IPCount: 20,
		},
		execer: exec.NewFakeExecer(nil, nil),
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
		},
		execer: exec.NewFakeExecer(errors.New("metallb error")),
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
		},
		execer: exec.NewFakeExecer(nil, nil),
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
		ctx:  canceledCtx,
		hErr: "context canceled",
	}, {
		desc: "dclient error",
		m: &MetalLBSpec{
			IPCount: 20,
		},
		execer: exec.NewFakeExecer(nil, nil, nil),
		mockExpects: func(m *mocks.MockNetworkAPIClient) {
			m.EXPECT().NetworkList(gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("dclient error"))
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
		dErr: "dclient error",
	}, {
		desc: "valid deployment",
		m: &MetalLBSpec{
			IPCount: 20,
		},
		execer: exec.NewFakeExecer(nil, nil, nil),
		wantConfig: &metallbv1.IPAddressPool{
			TypeMeta: metav1.TypeMeta{
				Kind:       "IPAddressPool",
				APIVersion: "metallb.io/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kne-service-pool",
				Namespace: "metallb-system",
			},
			Spec: metallbv1.IPAddressPoolSpec{
				Addresses: []string{"172.18.0.50 - 172.18.0.70"},
			},
		},
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
			mi, err := mfake.NewSimpleClientset(tt.mObjects...)
			if err != nil {
				t.Fatalf("faild to create fake client: %v", err)
			}

			tt.m.SetKClient(ki)
			tt.m.mClient = mi
			if tt.mockExpects != nil {
				m := mocks.NewMockNetworkAPIClient(mockCtrl)
				tt.mockExpects(m)
				tt.m.dClient = m
			}
			if tt.execer != nil {
				execer = tt.execer
			}
			err = tt.m.Deploy(context.Background())
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
			config, err := tt.m.mClient.IPAddressPool("metallb-system").Get(context.Background(), "kne-service-pool", metav1.GetOptions{})
			if err != nil {
				t.Fatalf("failed to get config: %v", err)
			}
			if s := cmp.Diff(tt.wantConfig, config); s != "" {
				t.Fatalf("invalid config data: \n%s", s)
			}
			l2Advert, err := tt.m.mClient.L2Advertisement("metallb-system").Get(context.Background(), "kne-l2-service-pool", metav1.GetOptions{})
			if err != nil {
				t.Fatalf("failed to get config: %v", err)
			}
			if s := cmp.Diff(&metallbv1.L2Advertisement{
				TypeMeta: metav1.TypeMeta{
					Kind:       "L2Advertisement",
					APIVersion: "metallb.io/v1beta1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kne-l2-service-pool",
					Namespace: "metallb-system",
				},
				Spec: metallbv1.L2AdvertisementSpec{
					IPAddressPools: []string{"kne-service-pool"},
				},
			}, l2Advert); s != "" {
				t.Fatalf("invalid config data: \n%s", s)
			}
		})
	}
}

func TestMeshnetSpec(t *testing.T) {
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	d := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "meshnet",
			Namespace: "meshnet",
		},
	}
	tests := []struct {
		desc   string
		m      *MeshnetSpec
		execer execerInterface
		dErr   string
		hErr   string
		ctx    context.Context
	}{{
		desc:   "apply error cluster",
		m:      &MeshnetSpec{},
		execer: exec.NewFakeExecer(errors.New("apply error")),
		dErr:   "apply error",
	}, {
		desc:   "canceled ctx",
		m:      &MeshnetSpec{},
		execer: exec.NewFakeExecer(nil),
		ctx:    canceledCtx,
		hErr:   "context canceled",
	}, {
		desc:   "valid deployment",
		m:      &MeshnetSpec{},
		execer: exec.NewFakeExecer(nil),
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
			if tt.execer != nil {
				execer = tt.execer
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
		})
	}
}

func TestIxiaTGSpec(t *testing.T) {
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	deploymentName := "foo"
	deploymentNS := "ixiatg-op-system"
	var replicas int32 = 2
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: deploymentNS,
		},
	}
	tests := []struct {
		desc        string
		i           *IxiaTGSpec
		execer      execerInterface
		dErr        string
		hErr        string
		cmNotFound  bool
		ctx         context.Context
		mockKClient func(*fake.Clientset)
	}{{
		desc:   "configmap file found - 2 replicas",
		i:      &IxiaTGSpec{},
		execer: exec.NewFakeExecer(nil, nil),
		mockKClient: func(k *fake.Clientset) {
			reaction := func(action ktest.Action) (handled bool, ret watch.Interface, err error) {
				f := newFakeWatch([]watch.Event{{
					Type: watch.Added,
					Object: &appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      deploymentName,
							Namespace: deploymentNS,
						},
						Spec: appsv1.DeploymentSpec{
							Replicas: &replicas,
						},
						Status: appsv1.DeploymentStatus{
							AvailableReplicas:   0,
							ReadyReplicas:       0,
							Replicas:            0,
							UnavailableReplicas: replicas,
							UpdatedReplicas:     0,
						},
					},
				}, {
					Type: watch.Modified,
					Object: &appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      deploymentName,
							Namespace: deploymentNS,
						},
						Spec: appsv1.DeploymentSpec{
							Replicas: &replicas,
						},
						Status: appsv1.DeploymentStatus{
							AvailableReplicas:   replicas,
							ReadyReplicas:       replicas,
							Replicas:            replicas,
							UnavailableReplicas: 0,
							UpdatedReplicas:     replicas,
						},
					},
				}})
				return true, f, nil
			}
			k.PrependWatchReactor("deployments", reaction)
		},
	}, {
		desc: "configmap specified - 1 replica",
		i: &IxiaTGSpec{
			ConfigMap: &IxiaTGConfigMap{
				Release: "from-arg",
				Images: []*IxiaTGImage{{
					Name: "controller",
					Path: "some/path",
					Tag:  "latest",
				}},
			},
		},
		execer: exec.NewFakeExecer(nil, nil),
		mockKClient: func(k *fake.Clientset) {
			reaction := func(action ktest.Action) (handled bool, ret watch.Interface, err error) {
				f := newFakeWatch([]watch.Event{{
					Type: watch.Added,
					Object: &appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      deploymentName,
							Namespace: deploymentNS,
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
							Name:      deploymentName,
							Namespace: deploymentNS,
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
	}, {
		desc:       "no configmap",
		i:          &IxiaTGSpec{},
		execer:     exec.NewFakeExecer(nil),
		cmNotFound: true,
		dErr:       "ixia configmap not found",
	}, {
		desc:   "operator deploy error",
		i:      &IxiaTGSpec{},
		execer: exec.NewFakeExecer(errors.New("failed to apply operator")),
		dErr:   "failed to apply operator",
	}, {
		desc: "configmap deploy error",
		i: &IxiaTGSpec{
			ConfigMap: &IxiaTGConfigMap{
				Release: "from-arg",
				Images: []*IxiaTGImage{{
					Name: "controller",
					Path: "some/path",
					Tag:  "latest",
				}},
			},
		},
		execer: exec.NewFakeExecer(nil, errors.New("failed to apply configmap")),
		dErr:   "failed to apply configmap",
	}, {
		desc:   "context canceled",
		i:      &IxiaTGSpec{},
		execer: exec.NewFakeExecer(nil, nil),
		ctx:    canceledCtx,
		hErr:   "context canceled",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			ki := fake.NewSimpleClientset(d)
			if tt.mockKClient != nil {
				tt.mockKClient(ki)
			}
			tt.i.SetKClient(ki)
			if tt.execer != nil {
				execer = tt.execer
			}
			var cmNotFoundErr error
			if tt.cmNotFound {
				cmNotFoundErr = errors.New("file not found")
			}
			osStat = func(_ string) (os.FileInfo, error) {
				return nil, cmNotFoundErr
			}
			err := tt.i.Deploy(context.Background())
			if s := errdiff.Substring(err, tt.dErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if err != nil {
				return
			}
			if tt.ctx == nil {
				tt.ctx = context.Background()
			}
			err = tt.i.Healthy(tt.ctx)
			if s := errdiff.Substring(err, tt.hErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
		})
	}
}

func TestSRLinuxSpec(t *testing.T) {
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	deploymentName := "foo"
	deploymentNS := "srlinux-controller"
	var replicas int32 = 2
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: deploymentNS,
		},
	}
	tests := []struct {
		desc        string
		srl         *SRLinuxSpec
		execer      execerInterface
		dErr        string
		hErr        string
		ctx         context.Context
		mockKClient func(*fake.Clientset)
	}{{
		desc:   "1 replica",
		srl:    &SRLinuxSpec{},
		execer: exec.NewFakeExecer(nil),
		mockKClient: func(k *fake.Clientset) {
			reaction := func(action ktest.Action) (handled bool, ret watch.Interface, err error) {
				f := newFakeWatch([]watch.Event{{
					Type: watch.Added,
					Object: &appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      deploymentName,
							Namespace: deploymentNS,
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
							Name:      deploymentName,
							Namespace: deploymentNS,
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
	}, {
		desc:   "2 replicas",
		srl:    &SRLinuxSpec{},
		execer: exec.NewFakeExecer(nil),
		mockKClient: func(k *fake.Clientset) {
			reaction := func(action ktest.Action) (handled bool, ret watch.Interface, err error) {
				f := newFakeWatch([]watch.Event{{
					Type: watch.Added,
					Object: &appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      deploymentName,
							Namespace: deploymentNS,
						},
						Spec: appsv1.DeploymentSpec{
							Replicas: &replicas,
						},
						Status: appsv1.DeploymentStatus{
							AvailableReplicas:   0,
							ReadyReplicas:       0,
							Replicas:            0,
							UnavailableReplicas: replicas,
							UpdatedReplicas:     0,
						},
					},
				}, {
					Type: watch.Modified,
					Object: &appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      deploymentName,
							Namespace: deploymentNS,
						},
						Spec: appsv1.DeploymentSpec{
							Replicas: &replicas,
						},
						Status: appsv1.DeploymentStatus{
							AvailableReplicas:   replicas,
							ReadyReplicas:       replicas,
							Replicas:            replicas,
							UnavailableReplicas: 0,
							UpdatedReplicas:     replicas,
						},
					},
				}})
				return true, f, nil
			}
			k.PrependWatchReactor("deployments", reaction)
		},
	}, {
		desc:   "operator deploy error",
		srl:    &SRLinuxSpec{},
		execer: exec.NewFakeExecer(errors.New("failed to apply operator")),
		dErr:   "failed to apply operator",
	}, {
		desc:   "context canceled",
		srl:    &SRLinuxSpec{},
		execer: exec.NewFakeExecer(nil),
		ctx:    canceledCtx,
		hErr:   "context canceled",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			ki := fake.NewSimpleClientset(d)
			if tt.mockKClient != nil {
				tt.mockKClient(ki)
			}
			tt.srl.SetKClient(ki)
			if tt.execer != nil {
				execer = tt.execer
			}
			err := tt.srl.Deploy(context.Background())
			if s := errdiff.Substring(err, tt.dErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if err != nil {
				return
			}
			if tt.ctx == nil {
				tt.ctx = context.Background()
			}
			err = tt.srl.Healthy(tt.ctx)
			if s := errdiff.Substring(err, tt.hErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
		})
	}
}

func TestCEOSLabSpec(t *testing.T) {
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	deploymentName := "foo"
	deploymentNS := "arista-ceoslab-operator-system"
	var replicas int32 = 2
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: deploymentNS,
		},
	}
	tests := []struct {
		desc        string
		ceos        *CEOSLabSpec
		execer      execerInterface
		dErr        string
		hErr        string
		ctx         context.Context
		mockKClient func(*fake.Clientset)
	}{{
		desc:   "1 replica",
		ceos:   &CEOSLabSpec{},
		execer: exec.NewFakeExecer(nil),
		mockKClient: func(k *fake.Clientset) {
			reaction := func(action ktest.Action) (handled bool, ret watch.Interface, err error) {
				f := newFakeWatch([]watch.Event{{
					Type: watch.Added,
					Object: &appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      deploymentName,
							Namespace: deploymentNS,
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
							Name:      deploymentName,
							Namespace: deploymentNS,
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
	}, {
		desc:   "2 replicas",
		ceos:   &CEOSLabSpec{},
		execer: exec.NewFakeExecer(nil),
		mockKClient: func(k *fake.Clientset) {
			reaction := func(action ktest.Action) (handled bool, ret watch.Interface, err error) {
				f := newFakeWatch([]watch.Event{{
					Type: watch.Added,
					Object: &appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      deploymentName,
							Namespace: deploymentNS,
						},
						Spec: appsv1.DeploymentSpec{
							Replicas: &replicas,
						},
						Status: appsv1.DeploymentStatus{
							AvailableReplicas:   0,
							ReadyReplicas:       0,
							Replicas:            0,
							UnavailableReplicas: replicas,
							UpdatedReplicas:     0,
						},
					},
				}, {
					Type: watch.Modified,
					Object: &appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      deploymentName,
							Namespace: deploymentNS,
						},
						Spec: appsv1.DeploymentSpec{
							Replicas: &replicas,
						},
						Status: appsv1.DeploymentStatus{
							AvailableReplicas:   replicas,
							ReadyReplicas:       replicas,
							Replicas:            replicas,
							UnavailableReplicas: 0,
							UpdatedReplicas:     replicas,
						},
					},
				}})
				return true, f, nil
			}
			k.PrependWatchReactor("deployments", reaction)
		},
	}, {
		desc:   "operator deploy error",
		ceos:   &CEOSLabSpec{},
		execer: exec.NewFakeExecer(errors.New("failed to apply operator")),
		dErr:   "failed to apply operator",
	}, {
		desc:   "context canceled",
		ceos:   &CEOSLabSpec{},
		execer: exec.NewFakeExecer(nil),
		ctx:    canceledCtx,
		hErr:   "context canceled",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			ki := fake.NewSimpleClientset(d)
			if tt.mockKClient != nil {
				tt.mockKClient(ki)
			}
			tt.ceos.SetKClient(ki)
			if tt.execer != nil {
				execer = tt.execer
			}
			err := tt.ceos.Deploy(context.Background())
			if s := errdiff.Substring(err, tt.dErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if err != nil {
				return
			}
			if tt.ctx == nil {
				tt.ctx = context.Background()
			}
			err = tt.ceos.Healthy(tt.ctx)
			if s := errdiff.Substring(err, tt.hErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
		})
	}
}
