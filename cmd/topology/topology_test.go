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
	topopb "github.com/google/kne/proto/topo"
	"github.com/google/kne/topo"
	"github.com/google/kne/topo/node"
	"github.com/h-fam/errdiff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/prototext"
	kfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

func NewNR(pb *topopb.Node) (node.Implementation, error) {
	return &notResettable{pb: pb}, nil
}

type notResettable struct {
	pb            *topopb.Node
	configPushErr error
}

func (nr *notResettable) Proto() *topopb.Node {
	return nr.pb
}

func (nr *notResettable) CreateNodeResource(_ context.Context, _ node.Interface) error {
	return status.Errorf(codes.Unimplemented, "Unimplemented")
}

func (nr *notResettable) DeleteNodeResource(_ context.Context, _ node.Interface) error {
	return status.Errorf(codes.Unimplemented, "Unimplemented")
}

func (nr *notResettable) ConfigPush(_ context.Context, s string, r io.Reader) error {
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
	resetErr error
}

func (r *resettable) ResetCfg(ctx context.Context, ni node.Interface) error {
	return r.resetErr
}

func NewR(pb *topopb.Node) (node.Implementation, error) {
	return &resettable{&notResettable{pb: pb}, nil}, nil
}

func writeTopology(t *testing.T, topo *topopb.Topology) (*os.File, func()) {
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
	tInstance := &topopb.Topology{
		Nodes: []*topopb.Node{{
			Name: "resettable1",
			Type: topopb.Node_Type(1001),
		}, {
			Name: "resettable2",
			Type: topopb.Node_Type(1001),
		}, {
			Name: "notresettable1",
			Type: topopb.Node_Type(1002),
		}},
	}
	fNoConfig, closer := writeTopology(t, tInstance)
	defer closer()
	tWithConfig := &topopb.Topology{
		Nodes: []*topopb.Node{{
			Name: "resettable1",
			Type: topopb.Node_Type(1001),
			Config: &topopb.Config{
				ConfigData: &topopb.Config_Data{
					Data: []byte("somebytes"),
				},
			},
		}, {
			Name: "resettable2",
			Type: topopb.Node_Type(1001),
			Config: &topopb.Config{
				ConfigData: &topopb.Config_File{
					File: confFile.Name(),
				},
			},
		}, {
			Name: "notresettable1",
			Type: topopb.Node_Type(1002),
		}},
	}
	fConfig, closer := writeTopology(t, tWithConfig)
	defer closer()
	tWithConfigRelative := &topopb.Topology{
		Nodes: []*topopb.Node{{
			Name: "resettable1",
			Type: topopb.Node_Type(1001),
			Config: &topopb.Config{
				ConfigData: &topopb.Config_Data{
					Data: []byte("somebytes"),
				},
			},
		}, {
			Name: "resettable2",
			Type: topopb.Node_Type(1001),
			Config: &topopb.Config{
				ConfigData: &topopb.Config_File{
					File: filepath.Base(confFile.Name()),
				},
			},
		}, {
			Name: "notresettable1",
			Type: topopb.Node_Type(1002),
		}},
	}
	fConfigRelative, closer := writeTopology(t, tWithConfigRelative)
	defer closer()
	tWithConfigDNE := &topopb.Topology{
		Nodes: []*topopb.Node{{
			Name: "resettable1",
			Type: topopb.Node_Type(1001),
			Config: &topopb.Config{
				ConfigData: &topopb.Config_Data{
					Data: []byte("somebytes"),
				},
			},
		}, {
			Name: "resettable2",
			Type: topopb.Node_Type(1001),
			Config: &topopb.Config{
				ConfigData: &topopb.Config_File{
					File: "dne",
				},
			},
		}, {
			Name: "notresettable1",
			Type: topopb.Node_Type(1002),
		}},
	}
	fConfigDNE, closer := writeTopology(t, tWithConfigDNE)
	defer closer()
	node.Register(topopb.Node_Type(1001), NewR)
	node.Register(topopb.Node_Type(1002), NewNR)
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
	var s string
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
	rCmd.PersistentFlags().StringVar(&s, "kubecfg", "", "")
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
