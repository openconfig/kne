package deploy

import (
	"context"
	"fmt"
	"testing"
	"time"

	dtypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/network"
	"github.com/golang/mock/gomock"
	"github.com/google/go-cmp/cmp"
	"github.com/openconfig/gnmi/errdiff"
	mfake "github.com/openconfig/kne/api/metallb/clientset/v1beta1/fake"
	"github.com/openconfig/kne/deploy/mocks"
	kexec "github.com/openconfig/kne/exec"
	fexec "github.com/openconfig/kne/exec/fake"
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
	"k8s.io/klog/v2"
)

const (
	verbose = true
)

func init() {
	klog.LogToStderr(false)
}

func TestKubeadmSpec(t *testing.T) {
	ctx := context.Background()

	origHomeDir := homeDir
	defer func() {
		homeDir = origHomeDir
	}()
	homeDir = func() string { return t.TempDir() }

	origOSGetuid := osGetuid
	defer func() {
		osGetuid = origOSGetuid
	}()
	osGetuid = func() int { return 1111 }

	origOSGetgid := osGetgid
	defer func() {
		osGetgid = origOSGetgid
	}()
	osGetgid = func() int { return 9999 }

	tests := []struct {
		desc        string
		k           *KubeadmSpec
		resp        []fexec.Response
		execPathErr bool
		wantErr     string
	}{{
		desc:        "kubeadm not found",
		k:           &KubeadmSpec{},
		execPathErr: true,
		wantErr:     `install dependency "kubeadm" to deploy`,
	}, {
		desc: "create cluster",
		k:    &KubeadmSpec{},
		resp: []fexec.Response{
			{Cmd: "sudo", Args: []string{"kubeadm", "init"}},
			{Cmd: "sudo", Args: []string{"cp", "/etc/kubernetes/admin.conf", ".*/.kube/config"}},
			{Cmd: "sudo", Args: []string{"chown", "1111:9999", ".*/.kube/config"}},
			{Cmd: "docker", Args: []string{"network", "create", "kne-kubeadm-.*"}},
		},
	}, {
		desc: "allow control plane scheduling",
		k: &KubeadmSpec{
			AllowControlPlaneScheduling: true,
		},
		resp: []fexec.Response{
			{Cmd: "sudo", Args: []string{"kubeadm", "init"}},
			{Cmd: "sudo", Args: []string{"cp", "/etc/kubernetes/admin.conf", ".*/.kube/config"}},
			{Cmd: "sudo", Args: []string{"chown", "1111:9999", ".*/.kube/config"}},
			{Cmd: "kubectl", Args: []string{"taint", "nodes", "--all", "node-role.kubernetes.io/control-plane:NoSchedule-"}},
			{Cmd: "docker", Args: []string{"network", "create", "kne-kubeadm-.*"}},
		},
	}, {
		desc: "pod manifest add on data",
		k: &KubeadmSpec{
			PodNetworkAddOnManifestData: []byte("manifest yaml"),
		},
		resp: []fexec.Response{
			{Cmd: "sudo", Args: []string{"kubeadm", "init"}},
			{Cmd: "sudo", Args: []string{"cp", "/etc/kubernetes/admin.conf", ".*/.kube/config"}},
			{Cmd: "sudo", Args: []string{"chown", "1111:9999", ".*/.kube/config"}},
			{Cmd: "kubectl", Args: []string{"apply", "-f", "-"}},
			{Cmd: "docker", Args: []string{"network", "create", "kne-kubeadm-.*"}},
		},
	}, {
		desc: "provided network",
		k: &KubeadmSpec{
			Network: "my-network",
		},
		resp: []fexec.Response{
			{Cmd: "sudo", Args: []string{"kubeadm", "init"}},
			{Cmd: "sudo", Args: []string{"cp", "/etc/kubernetes/admin.conf", ".*/.kube/config"}},
			{Cmd: "sudo", Args: []string{"chown", "1111:9999", ".*/.kube/config"}},
		},
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if verbose {
				fexec.LogCommand = func(s string) {
					t.Logf("%s: %s", tt.desc, s)
				}
			}
			cmds := fexec.Commands(tt.resp)
			kexec.Command = cmds.Command
			defer checkCmds(t, cmds)

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

func TestKubectlApply(t *testing.T) {
	tests := []struct {
		desc    string
		cfg     string
		resp    []fexec.Response
		wantErr string
	}{{
		desc: "success",
		cfg:  "valid kubeyaml",
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", "-"}},
		},
	}, {
		desc: "failed to apply",
		cfg:  "invalid",
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", "-"}, Err: "invalid yaml"},
		},
		wantErr: "invalid yaml",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if verbose {
				fexec.LogCommand = func(s string) {
					t.Logf("%s: %s", tt.desc, s)
				}
			}
			cmds := fexec.Commands(tt.resp)
			kexec.Command = cmds.Command
			defer checkCmds(t, cmds)

			err := kubectlApply([]byte(tt.cfg))
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
		})
	}
}

func TestKindSpec(t *testing.T) {
	ctx := context.Background()

	origSetPIDMaxScript := setPIDMaxScript
	defer func() {
		setPIDMaxScript = origSetPIDMaxScript
	}()
	setPIDMaxScript = "echo Executing fake set_pid_max script..."

	origPullRetryDelay := pullRetryDelay
	defer func() {
		pullRetryDelay = origPullRetryDelay
	}()
	pullRetryDelay = time.Millisecond

	tests := []struct {
		desc        string
		k           *KindSpec
		resp        []fexec.Response
		execPathErr bool
		setupGARErr bool
		wantErr     string
	}{{
		desc: "create cluster with cli",
		k: &KindSpec{
			Name: "test",
		},
		resp: []fexec.Response{
			{Cmd: "kind", Args: []string{"create", "cluster", "--name", "test"}},
		},
	}, {
		desc: "create cluster with recycle",
		k: &KindSpec{
			Name:    "test",
			Recycle: true,
		},
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"cluster-info", "--context", "kind-test"}, Err: "already exists"},
			{Cmd: "kind", Args: []string{"create", "cluster", "--name", "test"}},
		},
	}, {
		desc: "exists cluster with recycle",
		k: &KindSpec{
			Name:    "test",
			Recycle: true,
		},
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"cluster-info", "--context", "kind-test"}},
		},
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
		resp: []fexec.Response{
			{Cmd: "kind", Args: []string{"create", "cluster", "--name", "test"}, Err: "failed to create cluster"},
		},
		wantErr: "failed to create cluster",
	}, {
		desc: "create cluster with GAR",
		k: &KindSpec{
			Name:                     "test",
			GoogleArtifactRegistries: []string{"us-west1-docker.pkg.dev", "us-central1-docker.pkg.dev"},
		},
		resp: []fexec.Response{
			{Cmd: "kind", Args: []string{"create", "cluster", "--name", "test"}},
		},
	}, {
		desc: "create cluster with GAR - failure",
		k: &KindSpec{
			Name:                     "test",
			GoogleArtifactRegistries: []string{"us-west1-docker.pkg.dev", "us-central1-docker.pkg.dev"},
		},
		resp: []fexec.Response{
			{Cmd: "kind", Args: []string{"create", "cluster", "--name", "test"}},
		},
		setupGARErr: true,
		wantErr:     "failed to setup GAR access",
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
		resp: []fexec.Response{
			{Cmd: "kind", Args: []string{"create", "cluster", "--name", "test", "--image", "foo:latest", "--retain", "--wait", "180ns", "--kubeconfig", "kubecfg.yaml", "--config", "test.yaml"}},

			{Cmd: "docker", Args: []string{"pull", "docker"}, OutOfOrder: true},
			{Cmd: "docker", Args: []string{"tag", "docker", "local"}, OutOfOrder: true},
			{Cmd: "kind", Args: []string{"load", "docker-image", "local", "--name", "test"}, OutOfOrder: true},

			{Cmd: "docker", Args: []string{"pull", "gar"}, OutOfOrder: true},
			{Cmd: "docker", Args: []string{"tag", "gar", "docker"}, OutOfOrder: true},
			{Cmd: "kind", Args: []string{"load", "docker-image", "docker", "--name", "test"}, OutOfOrder: true},
		},
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
		resp: []fexec.Response{
			{Cmd: "kind", Args: []string{"create", "cluster", "--name", "test", "--image", "foo:latest", "--retain", "--wait", "180ns", "--kubeconfig", "kubecfg.yaml", "--config", "test.yaml"}},
			{Cmd: "kubectl", Args: []string{"apply", "-f", "bar:latest"}},
			{Cmd: "kubectl", Args: []string{"apply", "-f", "baz:latest"}},
			{Cmd: "kind", Args: []string{"load", "docker-image", "local", "--name", "test"}, OutOfOrder: true},
			{Cmd: "kind", Args: []string{"load", "docker-image", "docker", "--name", "test"}, OutOfOrder: true},
			{Cmd: "docker", Args: []string{"pull", "docker"}, OutOfOrder: true},
			{Cmd: "docker", Args: []string{"tag", "docker", "local"}, OutOfOrder: true},
			{Cmd: "docker", Args: []string{"pull", "gar"}, OutOfOrder: true},
			{Cmd: "docker", Args: []string{"tag", "gar", "docker"}, OutOfOrder: true},
		},
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
		resp: []fexec.Response{
			{Cmd: "kind", Args: []string{"create", "cluster", "--name", "test", "--image", "foo:latest", "--retain", "--wait", "180ns", "--kubeconfig", "kubecfg.yaml", "--config", "test.yaml"}},
			{Cmd: "kubectl", Args: []string{"apply", "-f", "bar:latest"}},
			{Cmd: "kubectl", Args: []string{"apply", "-f", "baz:latest"}, Err: "failed to deploy manifest"},
		},
		wantErr: "failed to deploy manifest",
	}, {
		desc: "create cluster load containers - failed all pull attempts",
		k: &KindSpec{
			Name: "test",
			ContainerImages: map[string]string{
				"docker": "local",
			},
		},
		resp: []fexec.Response{
			{Cmd: "kind", Args: []string{"create", "cluster", "--name", "test"}},
			{Cmd: "docker", Args: []string{"pull", "docker"}, Err: "failed to pull"},
			{Cmd: "docker", Args: []string{"pull", "docker"}, Err: "failed to pull"},
			{Cmd: "docker", Args: []string{"pull", "docker"}, Err: "failed to pull"},
			{Cmd: "docker", Args: []string{"pull", "docker"}, Err: "failed to pull"},
		},
		wantErr: "failed to pull",
	}, {
		desc: "create cluster load containers - failed initial pull",
		k: &KindSpec{
			Name: "test",
			ContainerImages: map[string]string{
				"docker": "local",
			},
		},
		resp: []fexec.Response{
			{Cmd: "kind", Args: []string{"create", "cluster", "--name", "test"}},
			{Cmd: "docker", Args: []string{"pull", "docker"}, Err: "failed to pull"},
			{Cmd: "docker", Args: []string{"pull", "docker"}},
			{Cmd: "docker", Args: []string{"tag", "docker", "local"}},
			{Cmd: "kind", Args: []string{"load", "docker-image", "local", "--name", "test"}},
		},
	}, {
		desc: "create cluster load containers - failed pull container not found",
		k: &KindSpec{
			Name: "test",
			ContainerImages: map[string]string{
				"docker": "local",
			},
		},
		resp: []fexec.Response{
			{Cmd: "kind", Args: []string{"create", "cluster", "--name", "test"}},
			{Cmd: "docker", Args: []string{"pull", "docker"}, Err: "failed to pull", Stdout: "not found"},
		},
		wantErr: "not found",
	}, {
		desc: "create cluster load containers - failed tag",
		k: &KindSpec{
			Name: "test",
			ContainerImages: map[string]string{
				"docker": "local",
			},
		},
		resp: []fexec.Response{
			{Cmd: "kind", Args: []string{"create", "cluster", "--name", "test"}},
			{Cmd: "docker", Args: []string{"pull", "docker"}},
			{Cmd: "docker", Args: []string{"tag", "docker", "local"}, Err: "failed to tag"},
		},
		wantErr: "failed to tag",
	}, {
		desc: "create cluster load containers - failed load",
		k: &KindSpec{
			Name: "test",
			ContainerImages: map[string]string{
				"docker": "local",
			},
		},
		resp: []fexec.Response{
			{Cmd: "kind", Args: []string{"create", "cluster", "--name", "test"}},
			{Cmd: "docker", Args: []string{"pull", "docker"}},
			{Cmd: "docker", Args: []string{"tag", "docker", "local"}},
			{Cmd: "kind", Args: []string{"load", "docker-image", "local", "--name", "test"}, Err: "failed to load"},
		},
		wantErr: "failed to load",
	}, {
		desc: "create cluster load containers - failed empty key",
		k: &KindSpec{
			Name: "test",
			ContainerImages: map[string]string{
				"": "local",
			},
		},
		resp: []fexec.Response{
			{Cmd: "kind", Args: []string{"create", "cluster", "--name", "test"}},
		},
		wantErr: "source container must not be empty",
	}, {
		desc: "create cluster load containers - success empty value",
		k: &KindSpec{
			Name: "test",
			ContainerImages: map[string]string{
				"docker": "",
			},
		},
		resp: []fexec.Response{
			{Cmd: "kind", Args: []string{"create", "cluster", "--name", "test"}},
			{Cmd: "docker", Args: []string{"pull", "docker"}},
			{Cmd: "kind", Args: []string{"load", "docker-image", "docker", "--name", "test"}},
		},
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
		wantErr: "No Major.Minor.Patch elements found",
	}, {
		desc: "failed kind version - invalid major",
		k: &KindSpec{
			Name:    "test",
			Version: "vr.1.1",
		},
		wantErr: `Invalid character(s) found in major number "r"`,
	}, {
		desc: "failed kind version - invalid minor",
		k: &KindSpec{
			Name:    "test",
			Version: "v0.foo.15",
		},
		wantErr: `Invalid character(s) found in minor number "foo"`,
	}, {
		desc: "failed kind version - invalid patch",
		k: &KindSpec{
			Name:    "test",
			Version: "v0.1.foo",
		},
		wantErr: `Invalid character(s) found in patch number "foo"`,
	}, {
		desc: "failed kind version less check",
		k: &KindSpec{
			Name:    "test",
			Version: "v0.15.0",
		},
		resp: []fexec.Response{
			{Cmd: "kind", Args: []string{"version"}, Stdout: "kind v0.14.0 go1.18.2 linux/amd64", Err: "kind version check failed: got v0.14.0, want v0.15.0"},
		},
		wantErr: "kind version check failed: got v0.14.0, want v0.15.0",
	}, {
		desc: "failed kind version exec fail",
		k: &KindSpec{
			Name:    "test",
			Version: "v0.15.0",
		},
		resp: []fexec.Response{
			{Cmd: "kind", Args: []string{"version"}, Err: "failed to get kind version: exec error"},
		},
		wantErr: "failed to get kind version: exec error",
	}, {
		desc: "failed kind version parse",
		k: &KindSpec{
			Name:    "test",
			Version: "v0.15.0",
		},
		resp: []fexec.Response{
			{Cmd: "kind", Args: []string{"version"}, Stdout: "kind "},
		},
		wantErr: "failed to parse kind version from",
	}, {
		desc: "failed kind version got fail parse",
		k: &KindSpec{
			Name:    "test",
			Version: "v0.15.0",
		},
		resp: []fexec.Response{
			{Cmd: "kind", Args: []string{"version"}, Stdout: "kind 0.14.0 go1.18.2 linux/amd64"},
		},
		wantErr: "kind version check failed: missing prefix on major version",
	}, {
		desc: "kind version pass",
		k: &KindSpec{
			Name:    "test",
			Version: "v0.15.0",
		},
		resp: []fexec.Response{
			{Cmd: "kind", Args: []string{"version"}, Stdout: "kind v0.15.0 go1.18.2 linux/amd64"},
			{Cmd: "kind", Args: []string{"create", "cluster", "--name", "test"}},
		},
	}, {
		desc: "kind prerelease version pass",
		k: &KindSpec{
			Name:    "test",
			Version: "v0.15.0",
		},
		resp: []fexec.Response{
			{Cmd: "kind", Args: []string{"version"}, Stdout: "kind v0.15.0-prelease go1.18.2 linux/amd64"},
			{Cmd: "kind", Args: []string{"create", "cluster", "--name", "test"}},
		},
	}, {
		desc: "kind version pass - major",
		k: &KindSpec{
			Name:    "test",
			Version: "v0.15.0",
		},
		resp: []fexec.Response{
			{Cmd: "kind", Args: []string{"version"}, Stdout: "kind v1.15.0 go1.18.2 linux/amd64"},
			{Cmd: "kind", Args: []string{"create", "cluster", "--name", "test"}},
		},
	}, {
		desc: "kind version pass - patch",
		k: &KindSpec{
			Name:    "test",
			Version: "v0.15.0",
		},
		resp: []fexec.Response{
			{Cmd: "kind", Args: []string{"version"}, Stdout: "kind v0.15.1 go1.18.2 linux/amd64"},
			{Cmd: "kind", Args: []string{"create", "cluster", "--name", "test"}},
		},
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if verbose {
				fexec.LogCommand = func(s string) {
					t.Logf("%s: %s", tt.desc, s)
				}
			}
			cmds := fexec.Commands(tt.resp)
			kexec.Command = cmds.Command
			defer checkCmds(t, cmds)

			execLookPath = func(_ string) (string, error) {
				if tt.execPathErr {
					return "", errors.New("unable to find on path")
				}
				return "fakePath", nil
			}
			kindSetupGARAccess = func(_ context.Context, _ []string) error {
				if tt.setupGARErr {
					return errors.New("unable to setup GAR access")
				}
				return nil
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
	nl := []dtypes.NetworkResource{
		{
			Name: "kind",
			IPAM: network.IPAM{
				Config: []network.IPAMConfig{
					{
						Subnet: "172.18.0.0/16",
					},
					{
						Subnet: "127::0/64",
					},
				},
			},
		},
		{
			Name: "bridge",
			IPAM: network.IPAM{
				Config: []network.IPAMConfig{
					{
						Subnet: "192.18.0.0/16",
					},
					{
						Subnet: "129::0/64",
					},
				},
			},
		},
		{
			Name: "docker",
			IPAM: network.IPAM{
				Config: []network.IPAMConfig{
					{
						Subnet: "1.1.1.1/16",
					},
				},
			},
		},
	}
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

	origPoolRetryDelay := poolRetryDelay
	defer func() {
		poolRetryDelay = origPoolRetryDelay
	}()
	poolRetryDelay = time.Millisecond

	tests := []struct {
		desc        string
		m           *MetalLBSpec
		resp        []fexec.Response
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
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ""}, Err: "namespace error"},
		},
		dErr: "namespace error",
	}, {
		desc: "secret create",
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ""}},
		},
		m: &MetalLBSpec{
			IPCount: 20,
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
		},
		k8sObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "metallb-system",
					Name:      "memberlist",
				},
			},
		},
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ""}, Err: "metallb error"},
		},
		dErr: "metallb error",
	}, {
		desc: "canceled ctx",
		m: &MetalLBSpec{
			IPCount: 20,
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
		ctx: canceledCtx,
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ""}},
		},
		hErr: "context canceled",
	}, {
		desc: "dclient error",
		m: &MetalLBSpec{
			IPCount: 20,
		},
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
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ""}},
		},
		dErr: "dclient error",
	}, {
		desc: "valid deployment",
		m: &MetalLBSpec{
			IPCount: 20,
		},
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ""}},
		},
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
				Addresses: []string{"192.18.0.50 - 192.18.0.70"},
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
	}, {
		desc: "valid deployment - manifest data instead of file",
		m: &MetalLBSpec{
			ManifestData: []byte("a fake manifest"),
			IPCount:      20,
		},
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
				Addresses: []string{"192.18.0.50 - 192.18.0.70"},
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
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ".*.yaml"}},
		},
	}, {
		desc: "valid deployment - kind",
		m: &MetalLBSpec{
			IPCount:                   20,
			dockerNetworkResourceName: "kind",
		},
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ""}},
		},
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
			if verbose {
				fexec.LogCommand = func(s string) {
					t.Logf("%s: %s", tt.desc, s)
				}
			}
			cmds := fexec.Commands(tt.resp)
			kexec.Command = cmds.Command
			defer checkCmds(t, cmds)

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
		desc string
		m    *MeshnetSpec
		resp []fexec.Response
		dErr string
		hErr string
		ctx  context.Context
	}{{
		desc: "apply error cluster",
		m:    &MeshnetSpec{},
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ""}, Err: "apply error"},
		},
		dErr: "apply error",
	}, {
		desc: "canceled ctx",
		m:    &MeshnetSpec{},
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ""}},
		},
		ctx:  canceledCtx,
		hErr: "context canceled",
	}, {
		desc: "valid deployment",
		m:    &MeshnetSpec{},
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ""}},
		},
	}, {
		desc: "valid deployment - manifest data over file",
		m: &MeshnetSpec{
			ManifestData: []byte("fake manifest data"),
		},
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ".*.yaml"}},
		},
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if verbose {
				fexec.LogCommand = func(s string) {
					t.Logf("%s: %s", tt.desc, s)
				}
			}

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
			cmds := fexec.Commands(tt.resp)
			kexec.Command = cmds.Command
			defer checkCmds(t, cmds)
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
		resp        []fexec.Response
		dErr        string
		hErr        string
		ctx         context.Context
		mockKClient func(*fake.Clientset)
	}{{
		desc: "2 replicas",
		i: &IxiaTGSpec{
			Operator:  "/path/to/operator.yaml",
			ConfigMap: "/path/to/configmap.yaml",
		},
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", "/path/to/operator.yaml"}},
			{Cmd: "kubectl", Args: []string{"apply", "-f", "/path/to/configmap.yaml"}},
		},

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
		desc: "1 replica - data over files",
		i: &IxiaTGSpec{
			OperatorData:  []byte("some fake data"),
			ConfigMapData: []byte("some fake data"),
		},
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
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ".*.yaml"}},
			{Cmd: "kubectl", Args: []string{"apply", "-f", ".*.yaml"}},
		},
	}, {
		desc: "no operator",
		i: &IxiaTGSpec{
			ConfigMap: "/path/to/configmap.yaml",
		},
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ""}, Err: "no operator file found"},
		},
		dErr: "no operator file found",
	}, {
		desc: "no configmap",
		i: &IxiaTGSpec{
			Operator: "/path/to/operator.yaml",
		},
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", "/path/to/operator.yaml"}},
		},
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
		desc: "operator deploy error",
		i:    &IxiaTGSpec{},
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ""}, Err: "failed to apply operator"},
		},

		dErr: "failed to apply operator",
	}, {
		desc: "configmap deploy error",
		i: &IxiaTGSpec{
			ConfigMap: "/path/to/configmap.yaml",
		},
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ""}},
			{Cmd: "kubectl", Args: []string{"apply", "-f", "/path/to/configmap.yaml"}, Err: "failed to apply configmap"},
		},
		dErr: "failed to apply configmap",
	}, {
		desc: "context canceled",
		i:    &IxiaTGSpec{},
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ""}},
		},
		ctx:  canceledCtx,
		hErr: "context canceled",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if verbose {
				fexec.LogCommand = func(s string) {
					t.Logf("%s: %s", tt.desc, s)
				}
			}
			cmds := fexec.Commands(tt.resp)
			kexec.Command = cmds.Command
			defer checkCmds(t, cmds)

			ki := fake.NewSimpleClientset(d)
			if tt.mockKClient != nil {
				tt.mockKClient(ki)
			}
			tt.i.SetKClient(ki)

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
		resp        []fexec.Response
		dErr        string
		hErr        string
		ctx         context.Context
		mockKClient func(*fake.Clientset)
	}{{
		desc: "1 replica",
		srl:  &SRLinuxSpec{},
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ""}},
		},
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
		desc: "2 replicas - data over file",
		srl: &SRLinuxSpec{
			OperatorData: []byte("some fake data"),
		},
		resp: []fexec.Response{
			// BUG: Should check to see if the file has the right contents
			{Cmd: "kubectl", Args: []string{"apply", "-f", ".*.yaml"}},
		},
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
		desc: "operator deploy error",
		srl:  &SRLinuxSpec{},
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ""}, Err: "failed to apply operator"},
		},
		dErr: "failed to apply operator",
	}, {
		desc: "context canceled",
		srl:  &SRLinuxSpec{},
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ""}},
		},
		ctx:  canceledCtx,
		hErr: "context canceled",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if verbose {
				fexec.LogCommand = func(s string) {
					t.Logf("%s: %s", tt.desc, s)
				}
			}
			cmds := fexec.Commands(tt.resp)
			kexec.Command = cmds.Command
			defer checkCmds(t, cmds)

			ki := fake.NewSimpleClientset(d)
			if tt.mockKClient != nil {
				tt.mockKClient(ki)
			}
			tt.srl.SetKClient(ki)

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
		resp        []fexec.Response
		dErr        string
		hErr        string
		ctx         context.Context
		mockKClient func(*fake.Clientset)
	}{{
		desc: "1 replica",
		ceos: &CEOSLabSpec{},
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ""}},
		},

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
		desc: "2 replicas - data over file",
		ceos: &CEOSLabSpec{
			OperatorData: []byte("some fake data"),
		},
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ".*.yaml"}},
		},
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
		desc: "operator deploy error",
		ceos: &CEOSLabSpec{},
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ""}, Err: "failed to apply operator"},
		},

		dErr: "failed to apply operator",
	}, {
		desc: "context canceled",
		ceos: &CEOSLabSpec{},
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ""}},
		},

		ctx:  canceledCtx,
		hErr: "context canceled",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if verbose {
				fexec.LogCommand = func(s string) {
					t.Logf("%s: %s", tt.desc, s)
				}
			}
			cmds := fexec.Commands(tt.resp)
			kexec.Command = cmds.Command
			defer checkCmds(t, cmds)

			ki := fake.NewSimpleClientset(d)
			if tt.mockKClient != nil {
				tt.mockKClient(ki)
			}
			tt.ceos.SetKClient(ki)
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

func TestLemmingSpec(t *testing.T) {
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	deploymentName := "foo"
	deploymentNS := "lemming-operator"
	var replicas int32 = 2
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: deploymentNS,
		},
	}
	tests := []struct {
		desc        string
		lemming     *LemmingSpec
		resp        []fexec.Response
		dErr        string
		hErr        string
		ctx         context.Context
		mockKClient func(*fake.Clientset)
	}{{
		desc:    "1 replica",
		lemming: &LemmingSpec{},
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ""}},
		},

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
		desc: "2 replicas - data over file",
		lemming: &LemmingSpec{
			OperatorData: []byte("some fake data"),
		},
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ".*.yaml"}},
		},
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
		desc:    "operator deploy error",
		lemming: &LemmingSpec{},
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ""}, Err: "failed to apply operator"},
		},

		dErr: "failed to apply operator",
	}, {
		desc:    "context canceled",
		lemming: &LemmingSpec{},
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"apply", "-f", ""}},
		},

		ctx:  canceledCtx,
		hErr: "context canceled",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if verbose {
				fexec.LogCommand = func(s string) {
					t.Logf("%s: %s", tt.desc, s)
				}
			}
			cmds := fexec.Commands(tt.resp)
			kexec.Command = cmds.Command
			defer checkCmds(t, cmds)

			ki := fake.NewSimpleClientset(d)
			if tt.mockKClient != nil {
				tt.mockKClient(ki)
			}
			tt.lemming.SetKClient(ki)
			err := tt.lemming.Deploy(context.Background())
			if s := errdiff.Substring(err, tt.dErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if err != nil {
				return
			}
			if tt.ctx == nil {
				tt.ctx = context.Background()
			}
			err = tt.lemming.Healthy(tt.ctx)
			if s := errdiff.Substring(err, tt.hErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
		})
	}
}

func checkCmds(t *testing.T, cmds *fexec.Command) {
	t.Helper()
	if err := cmds.Done(); err != nil {
		t.Errorf("%v", err)
	}
}
