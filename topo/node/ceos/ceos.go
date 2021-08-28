// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package ceos

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
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
)

// ErrIncompatibleCliConn raised when an invalid scrapligo cli transport type is found.
var ErrIncompatibleCliConn = errors.New("incompatible cli connection in use")

func New(pb *topopb.Node) (node.Implementation, error) {
	cfg := defaults(pb)
	proto.Merge(cfg, pb)
	node.FixServices(cfg)
	return &Node{
		pb: cfg,
	}, nil
}

type Node struct {
	pb      *topopb.Node
	cliConn *scraplinetwork.Driver
}

func (n *Node) Proto() *topopb.Node {
	return n.pb
}

// WaitCLIReady attempts to open the transport channel towards a Network OS and perform scrapligo OnOpen actions
// for a given platform. Retries indefinitely till success.
func (n *Node) WaitCLIReady() error {
	transportReady := false
	for !transportReady {
		if err := n.cliConn.Open(); err != nil {
			log.Debugf("%s - Cli not ready - waiting.", n.pb.Name)
			time.Sleep(time.Second * 2)
			continue
		}
		transportReady = true
		log.Debugf("%s - Cli ready.", n.pb.Name)
	}

	return nil
}

// PatchCLIConnOpen sets the OpenCmd and ExecCmd of system transport to work with `kubectl exec` terminal.
func (n *Node) PatchCLIConnOpen(ns string) error {
	t, ok := n.cliConn.Transport.Impl.(scraplitransport.SystemTransport)
	if !ok {
		return ErrIncompatibleCliConn
	}

	t.SetExecCmd("kubectl")
	t.SetOpenCmd([]string{"exec", "-it", "-n", ns, n.pb.Name, "--", "Cli"})

	return nil
}

// SpawnCLIConn spawns a CLI connection towards a Network OS using `kubectl exec` terminal and ensures CLI is ready
// to accept inputs.
func (n *Node) SpawnCLIConn(ns string) error {
	d, err := scraplicore.NewCoreDriver(
		n.pb.Name,
		"arista_eos",
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

	err = n.WaitCLIReady()
	if err != nil {
		return err
	}

	return nil
}

func (n *Node) GenerateSelfSigned(ctx context.Context, ni node.Interface) error {
	selfSigned := n.pb.GetConfig().GetCert().GetSelfSigned()
	if selfSigned == nil {
		log.Infof("%s - no cert config", n.pb.Name)
		return nil
	}
	log.Infof("%s - generating self signed certs", n.pb.Name)
	log.Infof("%s - waiting for pod to be running", n.pb.Name)
	w, err := ni.KubeClient().CoreV1().Pods(ni.Namespace()).Watch(ctx, metav1.ListOptions{
		FieldSelector: fields.SelectorFromSet(
			fields.Set{metav1.ObjectNameField: n.pb.Name},
		).String(),
	})
	if err != nil {
		return err
	}
	for e := range w.ResultChan() {
		p := e.Object.(*corev1.Pod)
		if p.Status.Phase == corev1.PodRunning {
			break
		}
	}
	log.Infof("%s - pod running.", n.pb.Name)

	err = n.SpawnCLIConn(ni.Namespace())
	if err != nil {
		return err
	}

	defer n.cliConn.Close()

	cfgs := []string{
		fmt.Sprintf(
			"security pki key generate rsa %d %s\n",
			selfSigned.KeySize,
			selfSigned.KeyName,
		),
		fmt.Sprintf(
			"security pki certificate generate self-signed %s key %s parameters common-name %s\n",
			selfSigned.CertName,
			selfSigned.KeyName,
			n.pb.Name,
		),
	}

	resp, err := n.cliConn.SendConfigs(cfgs)
	if err != nil {
		return err
	}

	log.Infof("%s - finshed cert generation", n.pb.Name)

	return resp.Failed
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

	resp, err := n.cliConn.SendConfig(cfgs)
	if err != nil {
		return err
	}

	log.Infof("%s - finshed config push", n.pb.Name)

	return resp.Failed
}

func (n *Node) ResetCfg(ctx context.Context, ni node.Interface) error {
	log.Infof("%s resetting config", n.pb.Name)

	err := n.SpawnCLIConn(ni.Namespace())
	if err != nil {
		return err
	}

	defer n.cliConn.Close()

	// this takes a long time sometimes, so we crank timeouts up
	resp, err := n.cliConn.SendCommand(
		"configure replace clean-config",
		scraplibase.WithSendTimeoutOps(300*time.Second),
	)
	if err != nil {
		return err
	}

	log.Infof("%s - finshed resetting config", n.pb.Name)

	return resp.Failed
}

func (n *Node) CreateNodeResource(_ context.Context, _ node.Interface) error {
	return status.Errorf(codes.Unimplemented, "Unimplemented")
}

func (n *Node) DeleteNodeResource(_ context.Context, _ node.Interface) error {
	return status.Errorf(codes.Unimplemented, "Unimplemented")
}

func defaults(pb *topopb.Node) *topopb.Node {
	if pb == nil {
		pb = &topopb.Node{
			Name: "default_ceos_node",
		}
	}
	return &topopb.Node{
		Constraints: map[string]string{
			"cpu":    "0.5",
			"memory": "1Gi",
		},
		Services: map[uint32]*topopb.Service{
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
			6030: {
				Name:     "gnmi",
				Inside:   6030,
				NodePort: node.GetNextPort(),
			},
		},
		Labels: map[string]string{
			"type": topopb.Node_ARISTA_CEOS.String(),
		},
		Config: &topopb.Config{
			Image: "ceos:latest",
			Command: []string{
				"/sbin/init",
				"systemd.setenv=INTFTYPE=eth",
				"systemd.setenv=ETBA=1",
				"systemd.setenv=SKIP_ZEROTOUCH_BARRIER_IN_SYSDBINIT=1",
				"systemd.setenv=CEOS=1",
				"systemd.setenv=EOS_PLATFORM=ceoslab",
				"systemd.setenv=container=docker",
			},
			Env: map[string]string{
				"CEOS":                                "1",
				"EOS_PLATFORM":                        "ceoslab",
				"container":                           "docker",
				"ETBA":                                "1",
				"SKIP_ZEROTOUCH_BARRIER_IN_SYSDBINIT": "1",
				"INTFTYPE":                            "eth",
			},
			EntryCommand: fmt.Sprintf("kubectl exec -it %s -- Cli", pb.Name),
			ConfigPath:   "/mnt/flash",
			ConfigFile:   "startup-config",
		},
	}
}

func init() {
	node.Register(topopb.Node_ARISTA_CEOS, New)
}
