package forward

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/openconfig/gnmi/errdiff"
	fpb "github.com/openconfig/kne/proto/forward"
	topopb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kfake "k8s.io/client-go/kubernetes/fake"
)

func TestNew(t *testing.T) {
	tests := []struct {
		desc    string
		nImpl   *node.Impl
		want    *topopb.Node
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
			Proto: &topopb.Node{},
		},
		want: &topopb.Node{
			Config: &topopb.Config{
				EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", ""),
				Image:        DefaultImage,
				ConfigPath:   "/etc",
				ConfigFile:   "config",
			},
		},
	}, {
		desc: "provided service",
		nImpl: &node.Impl{
			Proto: &topopb.Node{
				Config: &topopb.Config{
					Command: []string{"do", "run"},
				},
				Services: map[uint32]*topopb.Service{
					2000: {
						Name:      "Service",
						Inside:    2000,
						Outside:   20001,
						InsideIp:  "1.1.1.1",
						OutsideIp: "10.10.10.10",
					},
				},
			},
		},
		want: &topopb.Node{
			Config: &topopb.Config{
				Command:      []string{"do", "run"},
				EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", ""),
				Image:        DefaultImage,
				ConfigPath:   "/etc",
				ConfigFile:   "config",
			},
			Services: map[uint32]*topopb.Service{
				2000: {
					Name:      "Service",
					Inside:    2000,
					Outside:   20001,
					InsideIp:  "1.1.1.1",
					OutsideIp: "10.10.10.10",
				},
			},
		},
	}, {
		desc: "provided config command",
		nImpl: &node.Impl{
			Proto: &topopb.Node{
				Config: &topopb.Config{
					Command: []string{"do", "run"},
				},
			},
		},
		want: &topopb.Node{
			Config: &topopb.Config{
				Command:      []string{"do", "run"},
				EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", ""),
				Image:        DefaultImage,
				ConfigPath:   "/etc",
				ConfigFile:   "config",
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

func TestWirePlan(t *testing.T) {
	tests := []struct {
		desc    string
		cfg     *fpb.ForwardConfig
		want    []bridgeProcess
		wantErr string
	}{{
		desc: "no wires",
		cfg:  &fpb.ForwardConfig{},
	}, {
		desc: "server only, client is external",
		cfg: &fpb.ForwardConfig{
			Wires: []*fpb.Wire{{
				Z: &fpb.Endpoint{Endpoint: &fpb.Endpoint_Interface{
					Interface: &fpb.Interface{Name: "eth1"},
				}},
			}},
		},
		want: []bridgeProcess{{
			nameSuffix: "server",
			args:       []string{"server"},
		}},
	}, {
		desc: "server serving several interfaces stays one process",
		cfg: &fpb.ForwardConfig{
			Wires: []*fpb.Wire{{
				Z: &fpb.Endpoint{Endpoint: &fpb.Endpoint_Interface{
					Interface: &fpb.Interface{Name: "eth1"},
				}},
			}, {
				Z: &fpb.Endpoint{Endpoint: &fpb.Endpoint_Interface{
					Interface: &fpb.Interface{Name: "eth2"},
				}},
			}},
		},
		want: []bridgeProcess{{
			nameSuffix: "server",
			args:       []string{"server"},
		}},
	}, {
		desc: "client dialing a node in this topology",
		cfg: &fpb.ForwardConfig{
			Wires: []*fpb.Wire{{
				A: &fpb.Endpoint{Endpoint: &fpb.Endpoint_Interface{
					Interface: &fpb.Interface{Name: "eth1"},
				}},
				Z: &fpb.Endpoint{Endpoint: &fpb.Endpoint_LocalNode{
					LocalNode: &fpb.LocalNode{Name: "peer", Interface: "eth3"},
				}},
			}},
		},
		want: []bridgeProcess{{
			nameSuffix: "eth1",
			args:       []string{"client", "--peer=peer:50058", "--interface=eth1", "--remote_interface=eth3"},
		}},
	}, {
		desc: "client defaults the remote interface to the local one",
		cfg: &fpb.ForwardConfig{
			Wires: []*fpb.Wire{{
				A: &fpb.Endpoint{Endpoint: &fpb.Endpoint_Interface{
					Interface: &fpb.Interface{Name: "eth1"},
				}},
				Z: &fpb.Endpoint{Endpoint: &fpb.Endpoint_LocalNode{
					LocalNode: &fpb.LocalNode{Name: "peer"},
				}},
			}},
		},
		want: []bridgeProcess{{
			nameSuffix: "eth1",
			args:       []string{"client", "--peer=peer:50058", "--interface=eth1", "--remote_interface=eth1"},
		}},
	}, {
		desc: "client dialing outside the cluster, port assumed",
		cfg: &fpb.ForwardConfig{
			Wires: []*fpb.Wire{{
				A: &fpb.Endpoint{Endpoint: &fpb.Endpoint_Interface{
					Interface: &fpb.Interface{Name: "eth1"},
				}},
				Z: &fpb.Endpoint{Endpoint: &fpb.Endpoint_RemoteNode{
					RemoteNode: &fpb.RemoteNode{Addr: "far.away.example.com", Interface: "eth9"},
				}},
			}},
		},
		want: []bridgeProcess{{
			nameSuffix: "eth1",
			args:       []string{"client", "--peer=far.away.example.com:50058", "--interface=eth1", "--remote_interface=eth9"},
		}},
	}, {
		desc: "client dialing outside the cluster, port given",
		cfg: &fpb.ForwardConfig{
			Wires: []*fpb.Wire{{
				A: &fpb.Endpoint{Endpoint: &fpb.Endpoint_Interface{
					Interface: &fpb.Interface{Name: "eth1"},
				}},
				Z: &fpb.Endpoint{Endpoint: &fpb.Endpoint_RemoteNode{
					RemoteNode: &fpb.RemoteNode{Addr: "far.away.example.com:12345", Interface: "eth9"},
				}},
			}},
		},
		want: []bridgeProcess{{
			nameSuffix: "eth1",
			args:       []string{"client", "--peer=far.away.example.com:12345", "--interface=eth1", "--remote_interface=eth9"},
		}},
	}, {
		desc: "serving and dialing needs a process each",
		cfg: &fpb.ForwardConfig{
			Wires: []*fpb.Wire{{
				A: &fpb.Endpoint{Endpoint: &fpb.Endpoint_Interface{
					Interface: &fpb.Interface{Name: "eth1"},
				}},
				Z: &fpb.Endpoint{Endpoint: &fpb.Endpoint_LocalNode{
					LocalNode: &fpb.LocalNode{Name: "peer", Interface: "eth1"},
				}},
			}, {
				Z: &fpb.Endpoint{Endpoint: &fpb.Endpoint_Interface{
					Interface: &fpb.Interface{Name: "eth2"},
				}},
			}},
		},
		want: []bridgeProcess{{
			nameSuffix: "server",
			args:       []string{"server"},
		}, {
			nameSuffix: "eth1",
			args:       []string{"client", "--peer=peer:50058", "--interface=eth1", "--remote_interface=eth1"},
		}},
	}, {
		desc: "both endpoints local",
		cfg: &fpb.ForwardConfig{
			Wires: []*fpb.Wire{{
				A: &fpb.Endpoint{Endpoint: &fpb.Endpoint_Interface{
					Interface: &fpb.Interface{Name: "eth1"},
				}},
				Z: &fpb.Endpoint{Endpoint: &fpb.Endpoint_Interface{
					Interface: &fpb.Interface{Name: "eth2"},
				}},
			}},
		},
		wantErr: "cannot both be interfaces",
	}, {
		desc: "neither endpoint local",
		cfg: &fpb.ForwardConfig{
			Wires: []*fpb.Wire{{
				A: &fpb.Endpoint{Endpoint: &fpb.Endpoint_LocalNode{
					LocalNode: &fpb.LocalNode{Name: "a"},
				}},
				Z: &fpb.Endpoint{Endpoint: &fpb.Endpoint_LocalNode{
					LocalNode: &fpb.LocalNode{Name: "z"},
				}},
			}},
		},
		wantErr: "must be an interface",
	}, {
		desc: "peer node unnamed",
		cfg: &fpb.ForwardConfig{
			Wires: []*fpb.Wire{{
				A: &fpb.Endpoint{Endpoint: &fpb.Endpoint_Interface{
					Interface: &fpb.Interface{Name: "eth1"},
				}},
				Z: &fpb.Endpoint{Endpoint: &fpb.Endpoint_LocalNode{
					LocalNode: &fpb.LocalNode{},
				}},
			}},
		},
		wantErr: "local_node endpoint must set name",
	}, {
		desc: "remote peer without an address",
		cfg: &fpb.ForwardConfig{
			Wires: []*fpb.Wire{{
				A: &fpb.Endpoint{Endpoint: &fpb.Endpoint_Interface{
					Interface: &fpb.Interface{Name: "eth1"},
				}},
				Z: &fpb.Endpoint{Endpoint: &fpb.Endpoint_RemoteNode{
					RemoteNode: &fpb.RemoteNode{},
				}},
			}},
		},
		wantErr: "remote_node endpoint must set addr",
	}, {
		desc: "peer left unspecified",
		cfg: &fpb.ForwardConfig{
			Wires: []*fpb.Wire{{
				A: &fpb.Endpoint{Endpoint: &fpb.Endpoint_Interface{
					Interface: &fpb.Interface{Name: "eth1"},
				}},
			}},
		},
		wantErr: "must be a local_node or a remote_node",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got, err := wirePlan(tt.cfg)
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("wirePlan() unexpected error: %s", s)
			}
			if tt.wantErr != "" {
				return
			}
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(bridgeProcess{})); diff != "" {
				t.Errorf("wirePlan() diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCreatePeerService(t *testing.T) {
	ki := kfake.NewSimpleClientset()
	n, err := New(&node.Impl{
		Namespace:  "test",
		KubeClient: ki,
		Proto: &topopb.Node{
			Name: "fwd1",
		},
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	fn := n.(*Node)
	if err := fn.createPeerService(context.Background()); err != nil {
		t.Fatalf("createPeerService() failed: %v", err)
	}
	// A wire naming this node resolves "fwd1", so the Service must carry
	// exactly that name.
	got, err := ki.CoreV1().Services("test").Get(context.Background(), "fwd1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get peer service: %v", err)
	}
	if got.Spec.ClusterIP != corev1.ClusterIPNone {
		t.Errorf("peer service ClusterIP: got %q, want %q", got.Spec.ClusterIP, corev1.ClusterIPNone)
	}
	if len(got.Spec.Ports) != 0 {
		// Ports would make this Service show up in `kne show` and collide
		// with the wire port the topology may already declare.
		t.Errorf("peer service declares ports %v, want none", got.Spec.Ports)
	}
	if diff := cmp.Diff(map[string]string{"app": "fwd1"}, got.Spec.Selector); diff != "" {
		t.Errorf("peer service selector diff (-want +got):\n%s", diff)
	}
	// Impl.DeleteService finds Services by this label, and it is not
	// dynamically dispatched, so the label is the only thing that gets this
	// Service torn down with the node.
	if got.ObjectMeta.Labels["pod"] != "fwd1" {
		t.Errorf("peer service pod label: got %q, want %q", got.ObjectMeta.Labels["pod"], "fwd1")
	}
}
