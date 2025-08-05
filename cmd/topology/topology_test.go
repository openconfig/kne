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
	tfake "github.com/networkop/meshnet-cni/api/clientset/v1beta1/fake"
	"github.com/openconfig/gnmi/errdiff"
	cpb "github.com/openconfig/kne/proto/controller"
	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo"
	"github.com/openconfig/kne/topo/node"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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
			Name:   "resettable1",
			Vendor: tpb.Vendor(1001),
		}, {
			Name:   "resettable2",
			Vendor: tpb.Vendor(1001),
		}, {
			Name:   "notresettable1",
			Vendor: tpb.Vendor(1002),
		}},
	}
	fNoConfig, closer := writeTopology(t, tInstance)
	defer closer()
	tWithConfig := &tpb.Topology{
		Nodes: []*tpb.Node{{
			Name:   "resettable1",
			Vendor: tpb.Vendor(1001),
			Config: &tpb.Config{
				ConfigData: &tpb.Config_Data{
					Data: []byte("somebytes"),
				},
			},
		}, {
			Name:   "resettable2",
			Vendor: tpb.Vendor(1001),
			Config: &tpb.Config{
				ConfigData: &tpb.Config_File{
					File: confFile.Name(),
				},
			},
		}, {
			Name:   "notresettable1",
			Vendor: tpb.Vendor(1002),
		}},
	}
	fConfig, closer := writeTopology(t, tWithConfig)
	defer closer()
	tWithConfigRelative := &tpb.Topology{
		Nodes: []*tpb.Node{{
			Name:   "resettable1",
			Vendor: tpb.Vendor(1001),
			Config: &tpb.Config{
				ConfigData: &tpb.Config_Data{
					Data: []byte("somebytes"),
				},
			},
		}, {
			Name:   "resettable2",
			Vendor: tpb.Vendor(1001),
			Config: &tpb.Config{
				ConfigData: &tpb.Config_File{
					File: filepath.Base(confFile.Name()),
				},
			},
		}, {
			Name:   "notresettable1",
			Vendor: tpb.Vendor(1002),
		}},
	}
	fConfigRelative, closer := writeTopology(t, tWithConfigRelative)
	defer closer()
	tWithConfigDNE := &tpb.Topology{
		Nodes: []*tpb.Node{{
			Name:   "resettable1",
			Vendor: tpb.Vendor(1001),
			Config: &tpb.Config{
				ConfigData: &tpb.Config_Data{
					Data: []byte("somebytes"),
				},
			},
		}, {
			Name:   "resettable2",
			Vendor: tpb.Vendor(1001),
			Config: &tpb.Config{
				ConfigData: &tpb.Config_File{
					File: "dne",
				},
			},
		}, {
			Name:   "notresettable1",
			Vendor: tpb.Vendor(1002),
		}},
	}
	fConfigDNE, closer := writeTopology(t, tWithConfigDNE)
	defer closer()
	node.Vendor(tpb.Vendor(1001), NewR)
	node.Vendor(tpb.Vendor(1002), NewNR)
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
		desc: "valid topology with skip",
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
	rCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		viper.BindPFlags(cmd.Flags())
		return nil
	}
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
  model: "ceos"
  os: "eos"
  vendor: ARISTA
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
    vendor: KEYSIGHT
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

func TestGenerateRing(t *testing.T) {
	tests := []struct {
		desc       string
		args       []string
		inTopo     *tpb.Topology
		wantErr    string
		wantTopo   *tpb.Topology
		outputFile string
	}{{
		desc:    "no args",
		args:    []string{"generate", "ring"},
		wantErr: "invalid args",
	}, {
		desc:    "missing links arg",
		args:    []string{"generate", "ring", "a", "b"},
		wantErr: "invalid args",
	}, {
		desc:    "too many args",
		args:    []string{"generate", "ring", "a", "b", "c", "d"},
		wantErr: "invalid args",
	}, {
		desc:    "invalid count",
		args:    []string{"generate", "ring", "a", "b", "8"},
		inTopo:  &tpb.Topology{Nodes: []*tpb.Node{{Name: "n1"}}},
		wantErr: "invalid count",
	}, {
		desc:    "zero count",
		args:    []string{"generate", "ring", "a", "0", "8"},
		inTopo:  &tpb.Topology{Nodes: []*tpb.Node{{Name: "n1"}}},
		wantErr: "count must be greater than 1",
	}, {
		desc:    "invalid links",
		args:    []string{"generate", "ring", "a", "3", "b"},
		inTopo:  &tpb.Topology{Nodes: []*tpb.Node{{Name: "n1"}}},
		wantErr: "invalid links",
	}, {
		desc:    "zero links",
		args:    []string{"generate", "ring", "a", "3", "0"},
		inTopo:  &tpb.Topology{Nodes: []*tpb.Node{{Name: "n1"}}},
		wantErr: "links must be positive",
	}, {
		desc:    "file not found",
		args:    []string{"generate", "ring", "dne.textproto", "2", "8"},
		wantErr: "no such file",
	}, {
		desc:    "empty topology",
		args:    []string{"generate", "ring", "a", "2", "8"},
		inTopo:  &tpb.Topology{},
		wantErr: "no nodes in topology",
	}, {
		desc: "valid",
		args: []string{"generate", "ring", "a", "3", "8"},
		inTopo: &tpb.Topology{
			Name: "test",
			Nodes: []*tpb.Node{{
				Name:   "r1",
				Vendor: tpb.Vendor_ALPINE,
			}},
		},
		wantTopo: &tpb.Topology{
			Name: "test-ring",
			Nodes: []*tpb.Node{{
				Name:   "r1-0",
				Vendor: tpb.Vendor_ALPINE,
			}, {
				Name:   "r1-1",
				Vendor: tpb.Vendor_ALPINE,
			}, {
				Name:   "r1-2",
				Vendor: tpb.Vendor_ALPINE,
			}},
			Links: []*tpb.Link{
				{ANode: "r1-0", AInt: "eth1", ZNode: "r1-1", ZInt: "eth9"},
				{ANode: "r1-0", AInt: "eth2", ZNode: "r1-1", ZInt: "eth10"},
				{ANode: "r1-0", AInt: "eth3", ZNode: "r1-1", ZInt: "eth11"},
				{ANode: "r1-0", AInt: "eth4", ZNode: "r1-1", ZInt: "eth12"},
				{ANode: "r1-0", AInt: "eth5", ZNode: "r1-1", ZInt: "eth13"},
				{ANode: "r1-0", AInt: "eth6", ZNode: "r1-1", ZInt: "eth14"},
				{ANode: "r1-0", AInt: "eth7", ZNode: "r1-1", ZInt: "eth15"},
				{ANode: "r1-0", AInt: "eth8", ZNode: "r1-1", ZInt: "eth16"},
				{ANode: "r1-1", AInt: "eth1", ZNode: "r1-2", ZInt: "eth9"},
				{ANode: "r1-1", AInt: "eth2", ZNode: "r1-2", ZInt: "eth10"},
				{ANode: "r1-1", AInt: "eth3", ZNode: "r1-2", ZInt: "eth11"},
				{ANode: "r1-1", AInt: "eth4", ZNode: "r1-2", ZInt: "eth12"},
				{ANode: "r1-1", AInt: "eth5", ZNode: "r1-2", ZInt: "eth13"},
				{ANode: "r1-1", AInt: "eth6", ZNode: "r1-2", ZInt: "eth14"},
				{ANode: "r1-1", AInt: "eth7", ZNode: "r1-2", ZInt: "eth15"},
				{ANode: "r1-1", AInt: "eth8", ZNode: "r1-2", ZInt: "eth16"},
				{ANode: "r1-2", AInt: "eth1", ZNode: "r1-0", ZInt: "eth9"},
				{ANode: "r1-2", AInt: "eth2", ZNode: "r1-0", ZInt: "eth10"},
				{ANode: "r1-2", AInt: "eth3", ZNode: "r1-0", ZInt: "eth11"},
				{ANode: "r1-2", AInt: "eth4", ZNode: "r1-0", ZInt: "eth12"},
				{ANode: "r1-2", AInt: "eth5", ZNode: "r1-0", ZInt: "eth13"},
				{ANode: "r1-2", AInt: "eth6", ZNode: "r1-0", ZInt: "eth14"},
				{ANode: "r1-2", AInt: "eth7", ZNode: "r1-0", ZInt: "eth15"},
				{ANode: "r1-2", AInt: "eth8", ZNode: "r1-0", ZInt: "eth16"},
			},
		},
	}, {
		desc: "valid custom links",
		args: []string{"generate", "ring", "a", "2", "4"},
		inTopo: &tpb.Topology{
			Name: "test",
			Nodes: []*tpb.Node{{
				Name:   "r1",
				Vendor: tpb.Vendor_ALPINE,
			}},
		},
		wantTopo: &tpb.Topology{
			Name: "test-ring",
			Nodes: []*tpb.Node{{
				Name:   "r1-0",
				Vendor: tpb.Vendor_ALPINE,
			}, {
				Name:   "r1-1",
				Vendor: tpb.Vendor_ALPINE,
			}},
			Links: []*tpb.Link{
				{ANode: "r1-0", AInt: "eth1", ZNode: "r1-1", ZInt: "eth5"},
				{ANode: "r1-0", AInt: "eth2", ZNode: "r1-1", ZInt: "eth6"},
				{ANode: "r1-0", AInt: "eth3", ZNode: "r1-1", ZInt: "eth7"},
				{ANode: "r1-0", AInt: "eth4", ZNode: "r1-1", ZInt: "eth8"},
				{ANode: "r1-1", AInt: "eth1", ZNode: "r1-0", ZInt: "eth5"},
				{ANode: "r1-1", AInt: "eth2", ZNode: "r1-0", ZInt: "eth6"},
				{ANode: "r1-1", AInt: "eth3", ZNode: "r1-0", ZInt: "eth7"},
				{ANode: "r1-1", AInt: "eth4", ZNode: "r1-0", ZInt: "eth8"},
			},
		},
	}}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			cmd := New()
			buf := bytes.NewBuffer([]byte{})
			cmd.SetOut(buf)

			var inFile *os.File
			var closer func()
			if tt.inTopo != nil {
				inFile, closer = writeTopology(t, tt.inTopo)
				tt.args[2] = inFile.Name()
				defer closer()
			}

			cmd.SetArgs(tt.args)
			err := cmd.ExecuteContext(context.Background())
			if s := errdiff.Check(err, tt.wantErr); s != "" {
				t.Fatalf("generateRingFn() failed: %s", s)
			}
			if tt.wantErr != "" {
				return
			}

			ext := filepath.Ext(inFile.Name())
			outFileName := inFile.Name()[0:len(inFile.Name())-len(ext)] + "-output" + ext
			defer os.Remove(outFileName)

			outBytes, err := os.ReadFile(outFileName)
			if err != nil {
				t.Fatalf("Failed to read output file: %v", err)
			}
			gotTopo := &tpb.Topology{}
			if err := prototext.Unmarshal(outBytes, gotTopo); err != nil {
				t.Fatalf("Failed to unmarshal output topology: %v", err)
			}
			if diff := cmp.Diff(tt.wantTopo, gotTopo, protocmp.Transform()); diff != "" {
				t.Errorf("generateRingFn() output topology diff (-want +got):\n%s", diff)
			}
		})
	}
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
			Name:   "configable",
			Vendor: tpb.Vendor(1003),
		}, {
			Name:   "notconfigable",
			Vendor: tpb.Vendor(1004),
		}},
	}
	fConfig, closer := writeTopology(t, tWithConfig)
	defer closer()
	node.Vendor(tpb.Vendor(1003), NewR)
	node.Vendor(tpb.Vendor(1004), NewNC)
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
