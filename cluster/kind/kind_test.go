package kind

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/openconfig/gnmi/errdiff"
	kexec "github.com/openconfig/kne/exec"
	fexec "github.com/openconfig/kne/exec/fake"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

func TestClusterIsKind(t *testing.T) {
	tests := []struct {
		desc    string
		resp    []fexec.Response
		want    bool
		wantErr string
	}{{
		desc: "is kind",
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"config", "current-context"}, Stdout: "kind-kne"},
		},
		want: true,
	}, {
		desc: "is not kind",
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"config", "current-context"}, Stdout: "kne"},
		},
	}, {
		desc: "failed to get name",
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"config", "current-context"}, Err: "failed to get name"},
		},
		wantErr: "unable to determine cluster name",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			fexec.LogCommand = func(s string) {
				t.Logf("%s: %s", tt.desc, s)
			}
			cmds := fexec.Commands(tt.resp)
			kexec.Command = cmds.Command
			defer checkCmds(t, cmds)

			got, err := ClusterIsKind()
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Errorf("unexpected error: %s", s)
			}
			if got != tt.want {
				t.Errorf("ClusterIsKind() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClusterKindName(t *testing.T) {
	tests := []struct {
		desc    string
		resp    []fexec.Response
		want    string
		wantErr string
	}{{
		desc: "is kind",
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"config", "current-context"}, Stdout: "kind-kne"},
		},
		want: "kne",
	}, {
		desc: "is not kind",
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"config", "current-context"}, Stdout: "kne"},
		},
		wantErr: "not a kind cluster",
	}, {
		desc: "failed to get name",
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"config", "current-context"}, Err: "failed to get name"},
		},
		wantErr: "unable to determine cluster name",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			fexec.LogCommand = func(s string) {
				t.Logf("%s: %s", tt.desc, s)
			}
			cmds := fexec.Commands(tt.resp)
			kexec.Command = cmds.Command
			defer checkCmds(t, cmds)

			got, err := clusterKindName()
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Errorf("unexpected error: %s", s)
			}
			if got != tt.want {
				t.Errorf("clusterKindName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClusterKindNodes(t *testing.T) {
	tests := []struct {
		desc    string
		resp    []fexec.Response
		want    []string
		wantErr string
	}{{
		desc: "is kind",
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"config", "current-context"}, Stdout: "kind-kne"},
			{Cmd: "kind", Args: []string{"get", "nodes", "--name", "kne"}, Stdout: "kne-control-plane"},
		},
		want: []string{"kne-control-plane"},
	}, {
		desc: "is kind multinode",
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"config", "current-context"}, Stdout: "kind-kne"},
			{Cmd: "kind", Args: []string{"get", "nodes", "--name", "kne"}, Stdout: "kne-control-plane kne-worker"},
		},
		want: []string{"kne-control-plane", "kne-worker"},
	}, {
		desc: "is not kind",
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"config", "current-context"}, Stdout: "kne"},
		},
		wantErr: "not a kind cluster",
	}, {
		desc: "failed to get name",
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"config", "current-context"}, Err: "failed to get name"},
		},
		wantErr: "unable to determine cluster name",
	}, {
		desc: "failed to get nodes",
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"config", "current-context"}, Err: "failed to get name"},
		},
		wantErr: "unable to determine cluster name",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			fexec.LogCommand = func(s string) {
				t.Logf("%s: %s", tt.desc, s)
			}
			cmds := fexec.Commands(tt.resp)
			kexec.Command = cmds.Command
			defer checkCmds(t, cmds)

			got, err := clusterKindNodes()
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Errorf("unexpected error: %s", s)
			}
			if s := cmp.Diff(tt.want, got, cmpopts.SortSlices(func(a, b string) bool { return a < b })); s != "" {
				t.Errorf("clusterKindNodes() unexpected diff (-want +got):\n%s", s)
			}
		})
	}
}

func TestSetupGARAccess(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		desc           string
		regs           []string
		resp           []fexec.Response
		findCredsErr   bool
		tokenSourceErr bool
		wantErr        string
	}{{
		desc: "1 reg",
		regs: []string{"us-west1-docker.pkg.dev"},
		resp: []fexec.Response{
			{Cmd: "docker", Args: []string{"login", "-u", "oauth2accesstoken", "--password-stdin", "https://us-west1-docker.pkg.dev"}},
			{Cmd: "kubectl", Args: []string{"config", "current-context"}, Stdout: "kind-kne"},
			{Cmd: "kind", Args: []string{"get", "nodes", "--name", "kne"}, Stdout: "kne-control-plane"},
			{Cmd: "docker", Args: []string{"cp", ".*/config.json", "kne-control-plane:/var/lib/kubelet/config.json"}},
			{Cmd: "docker", Args: []string{"exec", "kne-control-plane", "systemctl", "restart", "kubelet.service"}},
		},
	}, {
		desc: "1 reg, 2 nodes",
		regs: []string{"us-west1-docker.pkg.dev"},
		resp: []fexec.Response{
			{Cmd: "docker", Args: []string{"login", "-u", "oauth2accesstoken", "--password-stdin", "https://us-west1-docker.pkg.dev"}},
			{Cmd: "kubectl", Args: []string{"config", "current-context"}, Stdout: "kind-kne"},
			{Cmd: "kind", Args: []string{"get", "nodes", "--name", "kne"}, Stdout: "kne-control-plane kne-worker"},
			{Cmd: "docker", Args: []string{"cp", ".*/config.json", "kne-control-plane:/var/lib/kubelet/config.json"}},
			{Cmd: "docker", Args: []string{"exec", "kne-control-plane", "systemctl", "restart", "kubelet.service"}},
			{Cmd: "docker", Args: []string{"cp", ".*/config.json", "kne-worker:/var/lib/kubelet/config.json"}},
			{Cmd: "docker", Args: []string{"exec", "kne-worker", "systemctl", "restart", "kubelet.service"}},
		},
	}, {
		desc: "2 regs",
		regs: []string{"us-west1-docker.pkg.dev", "us-central1-docker.pkg.dev"},
		resp: []fexec.Response{
			{Cmd: "docker", Args: []string{"login", "-u", "oauth2accesstoken", "--password-stdin", "https://us-west1-docker.pkg.dev"}},
			{Cmd: "docker", Args: []string{"login", "-u", "oauth2accesstoken", "--password-stdin", "https://us-central1-docker.pkg.dev"}},
			{Cmd: "kubectl", Args: []string{"config", "current-context"}, Stdout: "kind-kne"},
			{Cmd: "kind", Args: []string{"get", "nodes", "--name", "kne"}, Stdout: "kne-control-plane"},
			{Cmd: "docker", Args: []string{"cp", ".*/config.json", "kne-control-plane:/var/lib/kubelet/config.json"}},
			{Cmd: "docker", Args: []string{"exec", "kne-control-plane", "systemctl", "restart", "kubelet.service"}},
		},
	}, {
		desc:         "failed to get creds",
		regs:         []string{"us-west1-docker.pkg.dev"},
		findCredsErr: true,
		wantErr:      "failed to find gcloud credentials",
	}, {
		desc:           "failed to get access token",
		regs:           []string{"us-west1-docker.pkg.dev"},
		tokenSourceErr: true,
		wantErr:        "unable to generate token",
	}, {
		desc: "failed docker login",
		regs: []string{"us-west1-docker.pkg.dev"},
		resp: []fexec.Response{
			{Cmd: "docker", Args: []string{"login", "-u", "oauth2accesstoken", "--password-stdin", "https://us-west1-docker.pkg.dev"}, Err: "failed to login to docker"},
		},
		wantErr: "failed to login to docker",
	}, {
		desc: "failed to get name",
		regs: []string{"us-west1-docker.pkg.dev"},
		resp: []fexec.Response{
			{Cmd: "docker", Args: []string{"login", "-u", "oauth2accesstoken", "--password-stdin", "https://us-west1-docker.pkg.dev"}},
			{Cmd: "kubectl", Args: []string{"config", "current-context"}, Err: "failed to get name"},
		},
		wantErr: "failed to get name",
	}, {
		desc: "failed to get nodes",
		regs: []string{"us-west1-docker.pkg.dev"},
		resp: []fexec.Response{
			{Cmd: "docker", Args: []string{"login", "-u", "oauth2accesstoken", "--password-stdin", "https://us-west1-docker.pkg.dev"}},
			{Cmd: "kubectl", Args: []string{"config", "current-context"}, Stdout: "kind-kne"},
			{Cmd: "kind", Args: []string{"get", "nodes", "--name", "kne"}, Err: "failed to get nodes"},
		},
		wantErr: "failed to get nodes",
	}, {
		desc: "failed to cp config to node",
		regs: []string{"us-west1-docker.pkg.dev"},
		resp: []fexec.Response{
			{Cmd: "docker", Args: []string{"login", "-u", "oauth2accesstoken", "--password-stdin", "https://us-west1-docker.pkg.dev"}},
			{Cmd: "kubectl", Args: []string{"config", "current-context"}, Stdout: "kind-kne"},
			{Cmd: "kind", Args: []string{"get", "nodes", "--name", "kne"}, Stdout: "kne-control-plane"},
			{Cmd: "docker", Args: []string{"cp", ".*/config.json", "kne-control-plane:/var/lib/kubelet/config.json"}, Err: "failed to cp config to node"},
		},
		wantErr: "failed to cp config to node",
	}, {
		desc: "failed to restart kubelet",
		regs: []string{"us-west1-docker.pkg.dev"},
		resp: []fexec.Response{
			{Cmd: "docker", Args: []string{"login", "-u", "oauth2accesstoken", "--password-stdin", "https://us-west1-docker.pkg.dev"}},
			{Cmd: "kubectl", Args: []string{"config", "current-context"}, Stdout: "kind-kne"},
			{Cmd: "kind", Args: []string{"get", "nodes", "--name", "kne"}, Stdout: "kne-control-plane"},
			{Cmd: "docker", Args: []string{"cp", ".*/config.json", "kne-control-plane:/var/lib/kubelet/config.json"}},
			{Cmd: "docker", Args: []string{"exec", "kne-control-plane", "systemctl", "restart", "kubelet.service"}, Err: "failed to restart kubelet"},
		},
		wantErr: "failed to restart kubelet",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			fexec.LogCommand = func(s string) {
				t.Logf("%s: %s", tt.desc, s)
			}
			cmds := fexec.Commands(tt.resp)
			kexec.Command = cmds.Command
			defer checkCmds(t, cmds)

			googleFindDefaultCredentials = func(_ context.Context, _ ...string) (*google.Credentials, error) {
				if tt.findCredsErr {
					return nil, errors.New("unable to find default credentials")
				}
				return &google.Credentials{TokenSource: &fakeTokenSource{tokenErr: tt.tokenSourceErr}}, nil
			}

			err := SetupGARAccess(ctx, tt.regs)
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
		})
	}
}

type fakeTokenSource struct {
	tokenErr bool
}

func (f *fakeTokenSource) Token() (*oauth2.Token, error) {
	if f.tokenErr {
		return nil, errors.New("unable to generate token")
	}
	return &oauth2.Token{AccessToken: "some-fake-token"}, nil
}

func TestRefreshGARAccess(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		desc    string
		resp    []fexec.Response
		wantErr string
	}{{
		desc: "1 reg",
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"config", "current-context"}, Stdout: "kind-kne"},
			{Cmd: "kind", Args: []string{"get", "nodes", "--name", "kne"}, Stdout: "kne-control-plane"},
			{Cmd: "docker", Args: []string{"exec", "kne-control-plane", "cat", "/var/lib/kubelet/config.json"}, Stdout: `{"auths": {"us-west1-docker.pkg.dev": {}}}`},
			{Cmd: "docker", Args: []string{"login", "-u", "oauth2accesstoken", "--password-stdin", "https://us-west1-docker.pkg.dev"}},
			{Cmd: "kubectl", Args: []string{"config", "current-context"}, Stdout: "kind-kne"},
			{Cmd: "kind", Args: []string{"get", "nodes", "--name", "kne"}, Stdout: "kne-control-plane"},
			{Cmd: "docker", Args: []string{"cp", ".*/config.json", "kne-control-plane:/var/lib/kubelet/config.json"}},
			{Cmd: "docker", Args: []string{"exec", "kne-control-plane", "systemctl", "restart", "kubelet.service"}},
		},
	}, {
		desc: "2 regs",
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"config", "current-context"}, Stdout: "kind-kne"},
			{Cmd: "kind", Args: []string{"get", "nodes", "--name", "kne"}, Stdout: "kne-control-plane"},
			{Cmd: "docker", Args: []string{"exec", "kne-control-plane", "cat", "/var/lib/kubelet/config.json"}, Stdout: `{"auths": {"us-west1-docker.pkg.dev": {}, "us-central1-docker.pkg.dev": {}}}`},
			{Cmd: "docker", Args: []string{"login", "-u", "oauth2accesstoken", "--password-stdin", "https://us-west1-docker.pkg.dev"}},
			{Cmd: "docker", Args: []string{"login", "-u", "oauth2accesstoken", "--password-stdin", "https://us-central1-docker.pkg.dev"}},
			{Cmd: "kubectl", Args: []string{"config", "current-context"}, Stdout: "kind-kne"},
			{Cmd: "kind", Args: []string{"get", "nodes", "--name", "kne"}, Stdout: "kne-control-plane"},
			{Cmd: "docker", Args: []string{"cp", ".*/config.json", "kne-control-plane:/var/lib/kubelet/config.json"}},
			{Cmd: "docker", Args: []string{"exec", "kne-control-plane", "systemctl", "restart", "kubelet.service"}},
		},
	}, {
		desc: "0 regs",
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"config", "current-context"}, Stdout: "kind-kne"},
			{Cmd: "kind", Args: []string{"get", "nodes", "--name", "kne"}, Stdout: "kne-control-plane"},
			{Cmd: "docker", Args: []string{"exec", "kne-control-plane", "cat", "/var/lib/kubelet/config.json"}, Err: "no file"},
		},
	}, {
		desc: "invalid docker cfg",
		resp: []fexec.Response{
			{Cmd: "kubectl", Args: []string{"config", "current-context"}, Stdout: "kind-kne"},
			{Cmd: "kind", Args: []string{"get", "nodes", "--name", "kne"}, Stdout: "kne-control-plane"},
			{Cmd: "docker", Args: []string{"exec", "kne-control-plane", "cat", "/var/lib/kubelet/config.json"}, Stdout: `{"auths": {"us-west1-docker.pkg.dev": {`},
		},
		wantErr: "unexpected end of JSON input",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			fexec.LogCommand = func(s string) {
				t.Logf("%s: %s", tt.desc, s)
			}
			cmds := fexec.Commands(tt.resp)
			kexec.Command = cmds.Command
			defer checkCmds(t, cmds)

			googleFindDefaultCredentials = func(_ context.Context, _ ...string) (*google.Credentials, error) {
				return &google.Credentials{TokenSource: &fakeTokenSource{}}, nil
			}

			err := RefreshGARAccess(ctx)
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
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
