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
	"google.golang.org/protobuf/types/known/anypb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kfake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
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
			nameSuffix: "client-eth1",
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
			nameSuffix: "client-eth1",
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
			nameSuffix: "client-eth1",
			args:       []string{"client", "--peer=far.away.example.com:50058", "--interface=eth1", "--remote_interface=eth9"},
		}},
	}, {
		desc: "client dialing outside the cluster with bracketed IPv6 literal, port assumed",
		cfg: &fpb.ForwardConfig{
			Wires: []*fpb.Wire{{
				A: &fpb.Endpoint{Endpoint: &fpb.Endpoint_Interface{
					Interface: &fpb.Interface{Name: "eth1"},
				}},
				Z: &fpb.Endpoint{Endpoint: &fpb.Endpoint_RemoteNode{
					RemoteNode: &fpb.RemoteNode{Addr: "[2001:db8::1]", Interface: "eth9"},
				}},
			}},
		},
		want: []bridgeProcess{{
			nameSuffix: "client-eth1",
			args:       []string{"client", "--peer=[2001:db8::1]:50058", "--interface=eth1", "--remote_interface=eth9"},
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
			nameSuffix: "client-eth1",
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
			nameSuffix: "client-eth1",
			args:       []string{"client", "--peer=peer:50058", "--interface=eth1", "--remote_interface=eth1"},
		}},
	}, {
		desc: "duplicate client local interface rejected",
		cfg: &fpb.ForwardConfig{
			Wires: []*fpb.Wire{{
				A: &fpb.Endpoint{Endpoint: &fpb.Endpoint_Interface{
					Interface: &fpb.Interface{Name: "eth1"},
				}},
				Z: &fpb.Endpoint{Endpoint: &fpb.Endpoint_LocalNode{
					LocalNode: &fpb.LocalNode{Name: "peer1"},
				}},
			}, {
				A: &fpb.Endpoint{Endpoint: &fpb.Endpoint_Interface{
					Interface: &fpb.Interface{Name: "eth1"},
				}},
				Z: &fpb.Endpoint{Endpoint: &fpb.Endpoint_LocalNode{
					LocalNode: &fpb.LocalNode{Name: "peer2"},
				}},
			}},
		},
		wantErr: "duplicate wire for local interface",
	}, {
		desc: "collision between client and server local interface rejected",
		cfg: &fpb.ForwardConfig{
			Wires: []*fpb.Wire{{
				A: &fpb.Endpoint{Endpoint: &fpb.Endpoint_Interface{
					Interface: &fpb.Interface{Name: "eth1"},
				}},
				Z: &fpb.Endpoint{Endpoint: &fpb.Endpoint_LocalNode{
					LocalNode: &fpb.LocalNode{Name: "peer"},
				}},
			}, {
				Z: &fpb.Endpoint{Endpoint: &fpb.Endpoint_Interface{
					Interface: &fpb.Interface{Name: "eth1"},
				}},
			}},
		},
		wantErr: "duplicate wire for local interface",
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

func TestCreatePodMultiProcessArgsAndNaming(t *testing.T) {
	ki := kfake.NewSimpleClientset()
	vd, err := anypb.New(&fpb.ForwardConfig{
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
	})
	if err != nil {
		t.Fatalf("anypb.New() failed: %v", err)
	}
	n, err := New(&node.Impl{
		Namespace:  "test",
		KubeClient: ki,
		Proto: &topopb.Node{
			Name: "fwd1",
			Config: &topopb.Config{
				Args:       []string{"--alts"},
				VendorData: vd,
			},
		},
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	fn := n.(*Node)
	if err := fn.CreatePod(context.Background()); err != nil {
		t.Fatalf("CreatePod() failed: %v", err)
	}
	pod, err := ki.CoreV1().Pods("test").Get(context.Background(), "fwd1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get pod: %v", err)
	}
	if len(pod.Spec.Containers) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(pod.Spec.Containers))
	}
	// First container must retain the plain node name so Impl.Exec and kubectl exec work.
	if pod.Spec.Containers[0].Name != "fwd1" {
		t.Errorf("container[0].Name = %q, want %q", pod.Spec.Containers[0].Name, "fwd1")
	}
	if diff := cmp.Diff([]string{"server", "--alts"}, pod.Spec.Containers[0].Args); diff != "" {
		t.Errorf("container[0].Args diff (-want +got):\n%s", diff)
	}
	if pod.Spec.Containers[1].Name != "fwd1-client-eth1" {
		t.Errorf("container[1].Name = %q, want %q", pod.Spec.Containers[1].Name, "fwd1-client-eth1")
	}
	wantClientArgs := []string{"client", "--peer=peer:50058", "--interface=eth1", "--remote_interface=eth1", "--alts"}
	if diff := cmp.Diff(wantClientArgs, pod.Spec.Containers[1].Args); diff != "" {
		t.Errorf("container[1].Args diff (-want +got):\n%s", diff)
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
	if got.Labels["pod"] != "fwd1" {
		t.Errorf("peer service pod label: got %q, want %q", got.Labels["pod"], "fwd1")
	}
}

func TestCreateServiceRollbackPeerService(t *testing.T) {
	ki := kfake.NewSimpleClientset()
	ki.PrependReactor("create", "services", func(action ktesting.Action) (bool, runtime.Object, error) {
		createAction := action.(ktesting.CreateAction)
		svc := createAction.GetObject().(*corev1.Service)
		if svc.Name != "fwd1" {
			return true, nil, fmt.Errorf("injected service creation failure")
		}
		return false, nil, nil
	})
	n, err := New(&node.Impl{
		Namespace:  "test",
		KubeClient: ki,
		Proto: &topopb.Node{
			Name: "fwd1",
			Services: map[uint32]*topopb.Service{
				wirePort: {
					Name:   "wire",
					Inside: wirePort,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	fn := n.(*Node)
	if err := fn.CreateService(context.Background()); err == nil {
		t.Fatalf("expected CreateService() to fail when Impl.CreateService fails")
	}
	svcList, err := ki.CoreV1().Services("test").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("failed to list services: %v", err)
	}
	if len(svcList.Items) != 0 {
		t.Errorf("expected peer service to be rolled back on error, found %d services", len(svcList.Items))
	}
}
