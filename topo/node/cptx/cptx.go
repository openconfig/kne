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

	tpb "github.com/google/kne/proto/topo"
	"github.com/google/kne/topo/node"
	scraplicfg "github.com/scrapli/scrapligo/cfg"
	scraplibase "github.com/scrapli/scrapligo/driver/base"
	scraplicore "github.com/scrapli/scrapligo/driver/core"
	scraplinetwork "github.com/scrapli/scrapligo/driver/network"
	scraplitransport "github.com/scrapli/scrapligo/transport"
	scraplitest "github.com/scrapli/scrapligo/util/testhelper"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

// ErrIncompatibleCliConn raised when an invalid scrapligo cli transport type is found.
var ErrIncompatibleCliConn = errors.New("incompatible cli connection in use")

// Approx timeout while we wait for cli to get ready
const timeout = 500

func New(impl *node.Impl) (node.Node, error) {
	cfg := defaults(impl.Proto)
	proto.Merge(cfg, impl.Proto)
	node.FixServices(cfg)
	n := &Node{
		Impl: impl,
	}
	proto.Merge(n.Impl.Proto, cfg)
	return n, nil
}

type Node struct {
	*node.Impl
	cliConn *scraplinetwork.Driver
}

// Add validations for interfaces the node provides
var (
	_ node.ConfigPusher = (*Node)(nil)
)

// WaitCLIReady attempts to open the transport channel towards a Network OS and perform scrapligo OnOpen actions
// for a given platform. Retries with exponential backoff.
func (n *Node) WaitCLIReady(ctx context.Context) error {
	var err error
	sleep := 1 * time.Second
	for {
		select {
		case <-ctx.Done():
			log.Debugf("%s - Timed out - cli still not ready.", n.Name())
			return fmt.Errorf("context cancelled for target %q with cli not ready: %w", n.Name(), err)
		default:
		}

		err = n.cliConn.Open()
		if err == nil {
			log.Debugf("%s - cli ready.", n.Name())
			return nil
		}
		log.Debugf("%s - cli not ready - waiting %d seconds.", n.Name(), sleep)
		time.Sleep(sleep)
		sleep *= 2
	}
}

// PatchCLIConnOpen sets the OpenCmd and ExecCmd of system transport to work with `kubectl exec` terminal.
func (n *Node) PatchCLIConnOpen(ns string) error {
	t, ok := n.cliConn.Transport.Impl.(scraplitransport.SystemTransport)
	if !ok {
		return ErrIncompatibleCliConn
	}

	t.SetExecCmd("kubectl")
	t.SetOpenCmd([]string{"exec", "-it", "-n", ns, n.Name(), "--", "cli", "-c"})

	return nil
}

// SpawnCLIConn spawns a CLI connection towards a Network OS using `kubectl exec` terminal and ensures CLI is ready
// to accept inputs.
func (n *Node) SpawnCLIConn(ns string) error {
	d, err := scraplicore.NewCoreDriver(
		n.Name(),
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

	ctx, cancel := context.WithTimeout(context.Background(), timeout*time.Second)
	defer cancel()
	err = n.WaitCLIReady(ctx)
	if err != nil {
		return err
	}

	return nil
}

func (n *Node) ConfigPush(ctx context.Context, r io.Reader) error {
	log.Infof("%s - pushing config", n.Name())

	cfg, err := ioutil.ReadAll(r)
	cfgs := string(cfg)

	log.Debug(cfgs)

	if err != nil {
		return err
	}

	err = n.SpawnCLIConn(n.Namespace)
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

	c, err := scraplicfg.NewCfgDriver(
		n.cliConn,
		"juniper_junos",
		scraplicfg.WithCandidateConfigFilename(candidateConfigFile),
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

	log.Infof("%s - finshed config push", n.Name())

	return nil
}

func defaults(pb *tpb.Node) *tpb.Node {
	return &tpb.Node{
		Constraints: map[string]string{
			"cpu":    "8",
			"memory": "8Gi",
		},
		Services: map[uint32]*tpb.Service{
			443: {
				Name:     "ssl",
				Inside:   443,
				NodePort: node.GetNextPort(),
			},
			22: {
				Name:     "ssh",
				Inside:   22,
				NodePort: node.GetNextPort(),
			},
			50051: {
				Name:     "gnmi",
				Inside:   50051,
				NodePort: node.GetNextPort(),
			},
		},
		Labels: map[string]string{
			"type": tpb.Node_JUNIPER_CEVO.String(),
		},
		Config: &tpb.Config{
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
	node.Register(tpb.Node_JUNIPER_CEVO, New)
	node.Vendor(tpb.Vendor_JUNIPER, New)
}
