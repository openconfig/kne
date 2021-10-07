package topology

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"testing"

	tfake "github.com/google/kne/api/clientset/v1beta1/fake"
	"github.com/google/kne/topo"
	"github.com/google/kne/topo/node"
	"github.com/h-fam/errdiff"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	kfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	tpb "github.com/google/kne/proto/topo"
)

func NewNR(impl *node.Impl) (node.Node, error) {
	return &notResettable{Impl: impl}, nil
}

type notResettable struct {
	*node.Impl
	configPushErr error
}

func (nr *notResettable) ConfigPush(_ context.Context, r io.Reader) error {
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}
	if string(b) == "error" {
		return fmt.Errorf("error")
	}
	return nr.configPushErr
}

type resettable struct {
	*notResettable
}

func (r *resettable) ResetCfg(ctx context.Context) error {
	return nil
}

func NewR(impl *node.Impl) (node.Node, error) {
	return &resettable{&notResettable{Impl: impl}}, nil
}

func writeTopology(t *testing.T, topo *tpb.Topology) (*os.File, func()) {
	t.Helper()
	f, err := ioutil.TempFile("", "reset")
	if err != nil {
		log.Fatal(err)
	}
	b, err := prototext.Marshal(topo)
	if err != nil {
		t.Fatalf("failed to marshal topology")
	}
	if _, err := f.Write(b); err != nil {
		t.Fatalf("failed to write topology")
	}
	return f, func() { os.Remove(f.Name()) }
}

func TestReset(t *testing.T) {
	confFile, err := ioutil.TempFile("", "reset")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Fprintln(confFile, "some bytes")
	defer os.Remove(confFile.Name())
	tInstance := &tpb.Topology{
		Nodes: []*tpb.Node{{
			Name: "resettable1",
			Type: tpb.Node_Type(1001),
		}, {
			Name: "resettable2",
			Type: tpb.Node_Type(1001),
		}, {
			Name: "notresettable1",
			Type: tpb.Node_Type(1002),
		}},
	}
	fNoConfig, closer := writeTopology(t, tInstance)
	defer closer()
	tWithConfig := &tpb.Topology{
		Nodes: []*tpb.Node{{
			Name: "resettable1",
			Type: tpb.Node_Type(1001),
			Config: &tpb.Config{
				ConfigData: &tpb.Config_Data{
					Data: []byte("somebytes"),
				},
			},
		}, {
			Name: "resettable2",
			Type: tpb.Node_Type(1001),
			Config: &tpb.Config{
				ConfigData: &tpb.Config_File{
					File: confFile.Name(),
				},
			},
		}, {
			Name: "notresettable1",
			Type: tpb.Node_Type(1002),
		}},
	}
	fConfig, closer := writeTopology(t, tWithConfig)
	defer closer()
	tWithConfigRelative := &tpb.Topology{
		Nodes: []*tpb.Node{{
			Name: "resettable1",
			Type: tpb.Node_Type(1001),
			Config: &tpb.Config{
				ConfigData: &tpb.Config_Data{
					Data: []byte("somebytes"),
				},
			},
		}, {
			Name: "resettable2",
			Type: tpb.Node_Type(1001),
			Config: &tpb.Config{
				ConfigData: &tpb.Config_File{
					File: filepath.Base(confFile.Name()),
				},
			},
		}, {
			Name: "notresettable1",
			Type: tpb.Node_Type(1002),
		}},
	}
	fConfigRelative, closer := writeTopology(t, tWithConfigRelative)
	defer closer()
	tWithConfigDNE := &tpb.Topology{
		Nodes: []*tpb.Node{{
			Name: "resettable1",
			Type: tpb.Node_Type(1001),
			Config: &tpb.Config{
				ConfigData: &tpb.Config_Data{
					Data: []byte("somebytes"),
				},
			},
		}, {
			Name: "resettable2",
			Type: tpb.Node_Type(1001),
			Config: &tpb.Config{
				ConfigData: &tpb.Config_File{
					File: "dne",
				},
			},
		}, {
			Name: "notresettable1",
			Type: tpb.Node_Type(1002),
		}},
	}
	fConfigDNE, closer := writeTopology(t, tWithConfigDNE)
	defer closer()
	node.Register(tpb.Node_Type(1001), NewR)
	node.Register(tpb.Node_Type(1002), NewNR)
	tests := []struct {
		desc    string
		args    []string
		tFile   string
		wantErr string
	}{{
		desc:    "no args",
		wantErr: "invalid args",
		args:    []string{"reset"},
	}, {
		desc:    "no file",
		args:    []string{"reset", "filedne"},
		wantErr: "no such file",
	}, {
		desc:    "valid topology no skip",
		args:    []string{"reset", fNoConfig.Name(), "--skip=false"},
		wantErr: "node notresettable1 is not resettable",
	}, {
		desc: "valid topology no skip",
		args: []string{"reset", fNoConfig.Name(), "--skip"},
	}, {
		desc: "valid topology no skip nothing to push",
		args: []string{"reset", fNoConfig.Name(), "--skip", "--push"},
	}, {
		desc: "valid topology no skip config to push",
		args: []string{"reset", fConfig.Name(), "--skip", "--push"},
	}, {
		desc: "valid topology push with relative file location",
		args: []string{"reset", fConfigRelative.Name(), "--skip", "--push"},
	}, {
		desc:    "valid topology push with config DNE",
		args:    []string{"reset", fConfigDNE.Name(), "--skip", "--push"},
		wantErr: "no such file or directory",
	}, {
		desc: "valid topology push with config DNE single device",
		args: []string{"reset", fConfigDNE.Name(), "--skip", "--push", "resettable1"},
	}, {
		desc:    "valid topology push with config DNE single device invalid",
		args:    []string{"reset", fConfigDNE.Name(), "--skip", "--push", "dne"},
		wantErr: "not found",
	}}

	rCmd := New()
	origOpts := opts
	tf, err := tfake.NewSimpleClientset()
	if err != nil {
		t.Fatalf("cannot create fake topology clientset")
	}
	opts = []topo.Option{
		topo.WithClusterConfig(&rest.Config{}),
		topo.WithKubeClient(kfake.NewSimpleClientset()),
		topo.WithTopoClient(tf),
	}
	defer func() {
		opts = origOpts
	}()
	rCmd.PersistentFlags().String("kubecfg", "", "")
	buf := bytes.NewBuffer([]byte{})
	rCmd.SetOut(buf)
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			rCmd.SetArgs(tt.args)
			err := rCmd.ExecuteContext(context.Background())
			if s := errdiff.Check(err, tt.wantErr); s != "" {
				t.Fatalf("resetCfgCmd failed: %s", s)
			}
			if tt.wantErr != "" {
				return
			}
		})
	}
}

type fakeTopology struct {
	resources *topo.Resources
	rErr      error
	lErr      error
}

func (f *fakeTopology) Load(context.Context) error {
	return f.lErr
}

func (f *fakeTopology) Resources(context.Context) (*topo.Resources, error) {
	return f.resources, f.rErr
}

var (
	validPbTxt = `
name: "test-data-topology"
nodes: {
  name: "r1"
  type: ARISTA_CEOS
  services: {
	key: 1002
	value: {
  name: "ssh"
	  inside: 1002
	  outside: 22
	  inside_ip: "1.1.1.2"
	  outside_ip: "100.100.100.101"
	  node_port: 22
	}
  }
}
nodes: {
  name: "ate1"
  type: IXIA_TG
  services: {
	key: 1000
	value: {
	  name: "gnmi"
	  inside: 1000
	  inside_ip: "1.1.1.1"
	  outside_ip: "100.100.100.100"
	  node_port: 20000
	}
  }
  services: {
	key: 1001
	value: {
	  name: "grpc"
	  inside: 1001
	  inside_ip: "1.1.1.1"
	  outside_ip: "100.100.100.100"
	  node_port: 20001
	}
  }
  services: {
	key: 5555
	value: {
	  name: "port-5555"
	  inside: 5555
	  outside: 5555
	  inside_ip: "1.1.1.3"
	  outside_ip: "100.100.100.102"
	  node_port: 30010
	}
  }
  services: {
	key: 50071
	value: {
	  name: "port-50071"
	  inside: 50071
	  outside: 50071
	  inside_ip: "1.1.1.3"
	  outside_ip: "100.100.100.102"
	  node_port: 30011
	}
  }
  version: "0.0.1-9999"
}
links: {
  a_node: "r1"
  a_int: "eth9"
  z_node: "ate1"
  z_int: "eth1"
}
links: {
  a_node: "r1"
  a_int: "eth9"
  z_node: "ate2"
  z_int: "eth1"
}	
`
)

func TestService(t *testing.T) {
	sCmd := New()
	origOpts := opts
	defer func() {
		opts = origOpts
	}()
	sCmd.PersistentFlags().String("kubecfg", "", "")
	buf := bytes.NewBuffer([]byte{})
	sCmd.SetOut(buf)
	validTopoOut := &tpb.Topology{}
	if err := prototext.Unmarshal([]byte(validPbTxt), validTopoOut); err != nil {
		t.Fatalf("cannot Unmarshal validTopo: %v", err)
	}
	tests := []struct {
		desc    string
		args    []string
		tFile   string
		want    *tpb.Topology
		wantErr string
		topoNew func(string, *tpb.Topology, ...topo.Option) (resourcer, error)
	}{{
		desc:    "no args",
		wantErr: "missing topology",
		args:    []string{"service"},
	}, {
		desc:    "empty resources",
		wantErr: "not found",
		args:    []string{"service", "testdata/valid_topo.pb.txt"},
		topoNew: func(string, *tpb.Topology, ...topo.Option) (resourcer, error) {
			return &fakeTopology{
				resources: &topo.Resources{},
			}, nil
		},
	}, {
		desc:    "Load fail",
		wantErr: "load failed",
		args:    []string{"service", "testdata/valid_topo.pb.txt"},
		topoNew: func(string, *tpb.Topology, ...topo.Option) (resourcer, error) {
			return &fakeTopology{
				lErr:      fmt.Errorf("load failed"),
				resources: &topo.Resources{},
			}, nil
		},
	}, {
		desc: "newTopo valid",
		args: []string{"service", "testdata/valid_topo.pb.txt"},
		want: validTopoOut,
		topoNew: func(string, *tpb.Topology, ...topo.Option) (resourcer, error) {
			return &fakeTopology{
				resources: &topo.Resources{
					Services: map[string]*corev1.Service{
						"gnmi-service": {
							Spec: corev1.ServiceSpec{
								ClusterIP: "1.1.1.1",
								Ports: []corev1.ServicePort{{
									Port:     1000,
									NodePort: 20000,
									Name:     "gnmi",
								}},
							},
							Status: corev1.ServiceStatus{
								LoadBalancer: corev1.LoadBalancerStatus{
									Ingress: []corev1.LoadBalancerIngress{{IP: "100.100.100.100"}},
								},
							},
						},
						"grpc-service": {
							Spec: corev1.ServiceSpec{
								ClusterIP: "1.1.1.1",
								Ports: []corev1.ServicePort{{
									Port:     1001,
									NodePort: 20001,
									Name:     "grpc",
								}},
							},
							Status: corev1.ServiceStatus{
								LoadBalancer: corev1.LoadBalancerStatus{
									Ingress: []corev1.LoadBalancerIngress{{IP: "100.100.100.100"}},
								},
							},
						},
						"service-r1": {
							Spec: corev1.ServiceSpec{
								ClusterIP: "1.1.1.2",
								Ports: []corev1.ServicePort{{
									Port:       1002,
									NodePort:   22,
									Name:       "ssh",
									TargetPort: intstr.IntOrString{IntVal: 22},
								}},
							},
							Status: corev1.ServiceStatus{
								LoadBalancer: corev1.LoadBalancerStatus{
									Ingress: []corev1.LoadBalancerIngress{{IP: "100.100.100.101"}},
								},
							},
						},
						"service-ate1": {
							Spec: corev1.ServiceSpec{
								ClusterIP: "1.1.1.3",
								Ports: []corev1.ServicePort{{
									Port:       5555,
									NodePort:   30010,
									Name:       "port-5555",
									TargetPort: intstr.IntOrString{IntVal: 5555},
								}, {
									Port:       50071,
									NodePort:   30011,
									Name:       "port-50071",
									TargetPort: intstr.IntOrString{IntVal: 50071},
								}},
							},
							Status: corev1.ServiceStatus{
								LoadBalancer: corev1.LoadBalancerStatus{
									Ingress: []corev1.LoadBalancerIngress{{IP: "100.100.100.102"}},
								},
							},
						},
					},
				},
			}, nil
		},
	}, {
		desc:    "load topo error",
		wantErr: "no such file or directory",
		args:    []string{"service", "dne.txt"},
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			buf := bytes.NewBuffer([]byte{})
			sCmd.SetOut(buf)
			sCmd.SetArgs(tt.args)
			topoNew = tt.topoNew
			defer func() {
				topoNew = defaultNewTopo
			}()
			err := sCmd.ExecuteContext(context.Background())
			if s := errdiff.Check(err, tt.wantErr); s != "" {
				t.Fatalf("serviceCmd failed: %s", s)
			}
			if tt.wantErr != "" {
				return
			}
			got := &tpb.Topology{}
			if err := prototext.Unmarshal(buf.Bytes(), got); err != nil {
				t.Fatalf("Invalid buffer output: %v", err)
			}
			if !proto.Equal(got, tt.want) {
				t.Fatalf("Service failed: got:\n%s\n, want:\n%s\n", got, tt.want)
			}
		})
	}
}
