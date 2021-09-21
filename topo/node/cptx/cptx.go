// Juniper cPTX for KNE
// Copyright (c) Juniper Networks, Inc., 2021. All rights reserved.

package cptx

import (
    "context"
    "errors"
    "fmt"
    "io"
    "io/ioutil"
    "time"

    topopb "github.com/google/kne/proto/topo"
    "github.com/google/kne/topo/node"
    scraplibase "github.com/scrapli/scrapligo/driver/base"
    scraplicore "github.com/scrapli/scrapligo/driver/core"
    scraplinetwork "github.com/scrapli/scrapligo/driver/network"
    scraplitransport "github.com/scrapli/scrapligo/transport"
    scraplicfg "github.com/scrapli/scrapligo/cfg"
    log "github.com/sirupsen/logrus"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
    "google.golang.org/protobuf/proto"
    scraplitest "github.com/scrapli/scrapligo/util/testhelper"
)

// ErrIncompatibleCliConn raised when an invalid scrapligo cli transport type is found.
var ErrIncompatibleCliConn = errors.New("incompatible cli connection in use")
// Approx timeout while we wait for cli to get ready
const timeout = 500

func New(pb *topopb.Node) (node.Implementation, error) {
    cfg := defaults(pb)
    proto.Merge(cfg, pb)
    node.FixServices(cfg)
    return &Node{
        pb: cfg,
    }, nil
}

type Node struct {
    pb *topopb.Node
    cliConn *scraplinetwork.Driver
}

func (n *Node) Proto() *topopb.Node {
    return n.pb
}

// WaitCLIReady attempts to open the transport channel towards a Network OS and perform scrapligo OnOpen actions
// for a given platform. Retries with exponential backoff.
func (n *Node) WaitCLIReady(ctx context.Context) error {
    var err error
    var sleep time.Duration = 1
    for {
        select {
        case <-ctx.Done():
            log.Debugf("%s - Timed out - cli still not ready.", n.pb.Name)
            return err
        default:
            err = n.cliConn.Open()
            if err == nil {
                log.Debugf("%s - cli ready.", n.pb.Name)
                return nil
            }
            log.Debugf("%s - cli not ready - waiting %d seconds.", n.pb.Name, sleep)
            time.Sleep(sleep * time.Second)
            sleep *= 2
        }
    }
    return err
}

// PatchCLIConnOpen sets the OpenCmd and ExecCmd of system transport to work with `kubectl exec` terminal.
func (n *Node) PatchCLIConnOpen(ns string) error {
    t, ok := n.cliConn.Transport.Impl.(scraplitransport.SystemTransport)
    if !ok {
        return ErrIncompatibleCliConn
    }

    t.SetExecCmd("kubectl")
    t.SetOpenCmd([]string{"exec", "-it", "-n", ns, n.pb.Name, "--", "cli", "-c"})

    return nil
}

// SpawnCLIConn spawns a CLI connection towards a Network OS using `kubectl exec` terminal and ensures CLI is ready
// to accept inputs.
func (n *Node) SpawnCLIConn(ns string) error {
    d, err := scraplicore.NewCoreDriver(
        n.pb.Name,
        "juniper_junos",
        scraplibase.WithAuthBypass(true),
        // disable transport timeout
        scraplibase.WithTimeoutTransport(0),
    )
    if err != nil {
        return err
    }

    n.cliConn = d

    err = n.PatchCLIConnOpen(ns)
    if err != nil {
        n.cliConn = nil

        return err
    }

    ctx, cancel := context.WithTimeout(context.Background(), timeout * time.Second)
    defer cancel()
    err = n.WaitCLIReady(ctx)
    if err != nil {
        return err
    }

    return nil
}

func (n *Node) GenerateSelfSigned(ctx context.Context, ni node.Interface) error {
        return status.Errorf(codes.Unimplemented, "Unimplemented")
}

func (n *Node) ConfigPush(ctx context.Context, ns string, r io.Reader) error {
    log.Infof("%s - pushing config", n.pb.Name)

    cfg, err := ioutil.ReadAll(r)
    cfgs := string(cfg)

    log.Debug(cfgs)

    if err != nil {
        return err
    }

    err = n.SpawnCLIConn(ns)
    if err != nil {
        return err
    }

    defer n.cliConn.Close()


    // use a static candidate file name for test transport
    var candidateConfigFile string
    switch interface{}(n.cliConn.Transport.Impl).(type) {
    case *scraplitest.TestingTransport:
        candidateConfigFile = "scrapli_cfg_testing"
    default:
        // non testing transport
        candidateConfigFile = ""
    }

    //delete this assignment after scrapligo related fixes are in
    _ = candidateConfigFile

    c, err := scraplicfg.NewCfgDriver(
        n.cliConn,
        "juniper_junos",
        //uncomment this after scrapligo related fixes are in
        //scraplicfg.WithCandidateConfigFilename(candidateConfigFile),
    )
    if err != nil {
        return err
    }

    err = c.Prepare()
    if err != nil {
        return err
    }

    resp, err := c.LoadConfig(
        cfgs,
        true, //load replace
    )
    if err != nil {
        return err
    }
    if resp.Failed != nil {
        return resp.Failed
    }

    resp, err = c.CommitConfig()
    if err != nil {
        return err
    }
    if resp.Failed != nil {
        return resp.Failed
    }

    log.Infof("%s - finshed config push", n.pb.Name)

    return nil
}

func (n *Node) CreateNodeResource(_ context.Context, _ node.Interface) error {
    return status.Errorf(codes.Unimplemented, "Unimplemented")
}

func (n *Node) DeleteNodeResource(_ context.Context, _ node.Interface) error {
    return status.Errorf(codes.Unimplemented, "Unimplemented")
}

func defaults(pb *topopb.Node) *topopb.Node {
    return &topopb.Node{
        Constraints: map[string]string{
            "cpu":    "8",
            "memory": "8Gi",
        },
        Services: map[uint32]*topopb.Service{
            443: &topopb.Service{
                Name:     "ssl",
                Inside:   443,
                NodePort: node.GetNextPort(),
            },
            22: &topopb.Service{
                Name:     "ssh",
                Inside:   22,
                NodePort: node.GetNextPort(),
            },
            50051: &topopb.Service{
                Name:     "gnmi",
                Inside:   50051,
                NodePort: node.GetNextPort(),
            },
        },
        Labels: map[string]string{
            "type": topopb.Node_JUNIPER_CEVO.String(),
        },
        Config: &topopb.Config{
            Image: "cptx:latest",
            Command: []string{
                "/entrypoint.sh",
            },
            Env: map[string]string{
                 "CPTX": "1",
            },
            EntryCommand: fmt.Sprintf("kubectl exec -it %s -- cli -c", pb.Name),
            ConfigPath:   "/home/evo/configdisk",
            ConfigFile:   "juniper.conf",
        },
    }
}

func init() {
    node.Register(topopb.Node_JUNIPER_CEVO, New)
}
