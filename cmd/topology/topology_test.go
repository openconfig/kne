package topology

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/h-fam/errdiff"
	tfake "github.com/openconfig/kne/api/clientset/v1beta1/fake"
	cpb "github.com/openconfig/kne/proto/controller"
	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo"
	"github.com/openconfig/kne/topo/node"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/testing/protocmp"
	kfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

func NewNC(impl *node.Impl) (node.Node, error) {
	return &notConfigable{Impl: impl}, nil
}

type notConfigable struct {
	*node.Impl
	configPushErr error
}

func NewNR(impl *node.Impl) (node.Node, error) {
	return &notResettable{&notConfigable{Impl: impl}}, nil
}

type notResettable struct {
	*notConfigable
}

func (nr *notResettable) ConfigPush(_ context.Context, r io.Reader) error {
	b, err := io.ReadAll(r)
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
	return &resettable{&notResettable{&notConfigable{Impl: impl}}}, nil
}

func writeTopology(t *testing.T, topo *tpb.Topology) (*os.File, func()) {
	t.Helper()
	f, err := os.CreateTemp("", "reset")
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
	confFile, err := os.CreateTemp("", "reset")
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
		wantErr: `node "notresettable1" is not a Resetter`,
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
    name: "otg"
    type: IXIA_TG
    version: "0.0.1-9999"
    services: {
        key: 40051
        value: {
            name: "grpc"
            inside: 40051
			inside_ip: "1.1.1.1"
			outside_ip: "100.100.100.100"
			node_port: 20001
        }
    }
    services: {
        key: 50051
        value: {
            name: "gnmi"
            inside: 50051
			inside_ip: "1.1.1.1"
			outside_ip: "100.100.100.100"
			node_port: 20000
        }
    }
}
links: {
  a_node: "r1"
  a_int: "eth9"
  z_node: "otg"
  z_int: "eth1"
}
links: {
  a_node: "r1"
  a_int: "eth9"
  z_node: "otg"
  z_int: "eth1"
}
`
)

type fakeTopologyManager struct {
	topo    *tpb.Topology
	showErr error
}

func (f *fakeTopologyManager) Show(_ context.Context) (*cpb.ShowTopologyResponse, error) {
	if f.showErr != nil {
		return &cpb.ShowTopologyResponse{State: cpb.TopologyState_TOPOLOGY_STATE_ERROR}, f.showErr
	}
	return &cpb.ShowTopologyResponse{
		State:    cpb.TopologyState_TOPOLOGY_STATE_RUNNING,
		Topology: f.topo,
	}, nil
}

func TestService(t *testing.T) {
	validProto := &tpb.Topology{}
	if err := prototext.Unmarshal([]byte(validPbTxt), validProto); err != nil {
		t.Fatalf("failed to build a valid Topology protobuf for testing: %v", err)
	}
	tests := []struct {
		desc        string
		args        []string
		topoManager *fakeTopologyManager
		want        *tpb.Topology
		wantErr     string
	}{
		{
			desc:    "no args",
			wantErr: "missing topology",
			args:    []string{"service"},
		}, {
			desc:        "fail to get topology service",
			topoManager: &fakeTopologyManager{showErr: fmt.Errorf("some error")},
			wantErr:     "some error",
			args:        []string{"service", "testdata/valid_topo.pb.txt"},
		}, {
			desc:        "valid case",
			topoManager: &fakeTopologyManager{topo: validProto},
			want:        validProto,
			args:        []string{"service", "testdata/valid_topo.pb.txt"},
		},
	}

	sCmd := New()
	sCmd.PersistentFlags().String("kubecfg", "", "")
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			origNewTopologyManager := newTopologyManager
			newTopologyManager = func(_ *tpb.Topology, _ ...topo.Option) (TopologyManager, error) {
				return tt.topoManager, nil
			}
			defer func() {
				newTopologyManager = origNewTopologyManager
			}()
			buf := bytes.NewBuffer([]byte{})
			sCmd.SetOut(buf)
			sCmd.SetArgs(tt.args)

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
			if s := cmp.Diff(got, tt.want, protocmp.Transform()); s != "" {
				t.Fatalf("Service failed: %s", s)
			}
		})
	}
}

func TestPush(t *testing.T) {
	confFile, err := os.CreateTemp("", "push")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Fprintln(confFile, "some bytes")
	defer os.Remove(confFile.Name())
	tWithConfig := &tpb.Topology{
		Nodes: []*tpb.Node{{
			Name: "configable",
			Type: tpb.Node_Type(1003),
		}, {
			Name: "notconfigable",
			Type: tpb.Node_Type(1004),
		}},
	}
	fConfig, closer := writeTopology(t, tWithConfig)
	defer closer()
	node.Register(tpb.Node_Type(1003), NewR)
	node.Register(tpb.Node_Type(1004), NewNC)
	tests := []struct {
		desc    string
		args    []string
		tFile   string
		wantErr string
	}{{
		desc:    "no args",
		wantErr: "invalid args",
		args:    []string{"push"},
	}, {
		desc:    "missing args",
		wantErr: "invalid args",
		args:    []string{"push", fConfig.Name(), "configable"},
	}, {
		desc:    "no file",
		args:    []string{"push", fConfig.Name(), "configable", "filedne"},
		wantErr: "no such file",
	}, {
		desc:    "valid file invalid device",
		args:    []string{"push", fConfig.Name(), "foo", confFile.Name()},
		wantErr: `node "foo" not found`,
	}, {
		desc:    "valid file notconfigable device",
		args:    []string{"push", fConfig.Name(), "notconfigable", confFile.Name()},
		wantErr: "does not implement ConfigPusher",
	}, {
		desc: "valid file",
		args: []string{"push", fConfig.Name(), "configable", confFile.Name()},
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
				t.Fatalf("pushFn failed: %s", s)
			}
			if tt.wantErr != "" {
				return
			}
		})
	}
}
