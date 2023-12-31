package drivenets

import (
	"testing"

	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
)

func TestNew(t *testing.T) {
    tests := []struct {
        name      string
        nodeImpl  *node.Impl
        wantErr   bool
        errString string
    }{
        {
            name:      "nil nodeImpl",
            nodeImpl:  nil,
            wantErr:   true,
            errString: "nodeImpl cannot be nil",
        },
        {
            name: "nil Proto",
            nodeImpl: &node.Impl{
                Proto: nil,
            },
            wantErr:   true,
            errString: "nodeImpl.Proto cannot be nil",
        },
        {
            name: "no model specified",
            nodeImpl: &node.Impl{
                Proto: &tpb.Node{},
            },
            wantErr:   true,
            errString: "unknown model",
        },
    }

	for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            _, err := New(tt.nodeImpl)
            if tt.wantErr {
                if err == nil {
                    t.Errorf("New() error = nil, wantErr %v", tt.wantErr)
                    return
                }
                if err.Error() != tt.errString {
                    t.Errorf("New() error = %v, wantErr %v", err, tt.errString)
                }
            } else if err != nil {
                t.Errorf("New() unexpected error = %v", err)
            }
        })
    }
}

func TestCdnosDefaults(t *testing.T) {
    pb := &tpb.Node{
        Name: "testNode",
    }

    pb = cdnosDefaults(pb)

    if pb.Config == nil {
        t.Errorf("Config is nil")
    }

    if pb.Config.Image == "" {
        t.Errorf("Image is empty")
    }

    if pb.Config.InitImage == "" {
        t.Errorf("InitImage is empty")
    }

    if len(pb.GetConfig().GetCommand()) == 0 {
        t.Errorf("Command is empty")
    }

    if pb.Config.EntryCommand == "" {
        t.Errorf("EntryCommand is empty")
    }

    if pb.Config.Cert == nil {
        t.Errorf("Cert is nil")
    }

    if pb.Constraints == nil {
        t.Errorf("Constraints is nil")
    }

    if pb.Constraints["cpu"] == "" {
        t.Errorf("CPU constraint is empty")
    }

    if pb.Constraints["memory"] == "" {
        t.Errorf("Memory constraint is empty")
    }

    if pb.Labels == nil {
        t.Errorf("Labels is nil")
    }

    if pb.Labels["vendor"] == "" {
        t.Errorf("Vendor label is empty")
    }

    if pb.Labels[node.OndatraRoleLabel] != node.OndatraRoleDUT {
        t.Errorf("OndatraRoleLabel is not DUT")
    }

    if pb.Services == nil {
        t.Errorf("Services is nil")
    }
}