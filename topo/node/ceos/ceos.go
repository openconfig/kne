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
	"strings"
	"time"

	tpb "github.com/google/kne/proto/topo"
	"github.com/google/kne/topo/node"
	scraplibase "github.com/scrapli/scrapligo/driver/base"
	scraplicore "github.com/scrapli/scrapligo/driver/core"
	scraplinetwork "github.com/scrapli/scrapligo/driver/network"
	scraplitransport "github.com/scrapli/scrapligo/transport"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
)

// ErrIncompatibleCliConn raised when an invalid scrapligo cli transport type is found.
var ErrIncompatibleCliConn = errors.New("incompatible cli connection in use")

type Node struct {
	*node.Impl
	cliConn *scraplinetwork.Driver
}

// Add validations for interfaces the node provides
var (
	_ node.Certer       = (*Node)(nil)
	_ node.ConfigPusher = (*Node)(nil)
	_ node.Resetter     = (*Node)(nil)
)

func New(nodeImpl *node.Impl) (node.Node, error) {
	if nodeImpl == nil {
		return nil, fmt.Errorf("nodeImpl cannot be nil")
	}
	if nodeImpl.Proto == nil {
		return nil, fmt.Errorf("nodeImpl.Proto cannot be nil")
	}
	cfg := defaults(nodeImpl.Proto)
	nodeImpl.Proto = cfg
	n := &Node{
		Impl: nodeImpl,
	}
	n.FixInterfaces()
	return n, nil
}

// WaitCLIReady attempts to open the transport channel towards a Network OS and perform scrapligo OnOpen actions
// for a given platform. Retries indefinitely till success.
func (n *Node) WaitCLIReady() error {
	transportReady := false
	for !transportReady {
		if err := n.cliConn.Open(); err != nil {
			log.Debugf("%s - Cli not ready - waiting.", n.Name())
			time.Sleep(time.Second * 2)
			continue
		}
		transportReady = true
		log.Debugf("%s - Cli ready.", n.Name())
	}
	return nil
}

// PatchCLIConnOpen sets the OpenCmd and ExecCmd of system transport to work with `kubectl exec` terminal.
func (n *Node) PatchCLIConnOpen() error {
	t, ok := n.cliConn.Transport.Impl.(scraplitransport.SystemTransport)
	if !ok {
		return ErrIncompatibleCliConn
	}

	t.SetExecCmd("kubectl")
	var args []string
	if n.Kubecfg != "" {
		args = append(args, fmt.Sprintf("--kubeconfig=%s", n.Kubecfg))
	}
	args = append(args, "exec", "-it", "-n", n.Namespace, n.Name(), "--", "Cli")
	t.SetOpenCmd(args)
	return nil
}

// SpawnCLIConn spawns a CLI connection towards a Network OS using `kubectl exec` terminal and ensures CLI is ready
// to accept inputs.
func (n *Node) SpawnCLIConn() error {
	d, err := scraplicore.NewCoreDriver(
		n.Name(),
		"arista_eos",
		scraplibase.WithAuthBypass(true),
		// disable transport timeout
		scraplibase.WithTimeoutTransport(0),
	)
	if err != nil {
		return err
	}

	n.cliConn = d

	err = n.PatchCLIConnOpen()
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

func (n *Node) GenerateSelfSigned(ctx context.Context) error {
	selfSigned := n.Proto.GetConfig().GetCert().GetSelfSigned()
	if selfSigned == nil {
		log.Infof("%s - no cert config", n.Name())
		return nil
	}
	log.Infof("%s - generating self signed certs", n.Name())
	log.Infof("%s - waiting for pod to be running", n.Name())
	w, err := n.KubeClient.CoreV1().Pods(n.Namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: fields.SelectorFromSet(
			fields.Set{metav1.ObjectNameField: n.Name()},
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
	log.Infof("%s - pod running.", n.Name())

	err = n.SpawnCLIConn()
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
			n.Name(),
		),
	}

	resp, err := n.cliConn.SendConfigs(cfgs)
	if err != nil {
		return err
	}

	if resp.Failed == nil {
		log.Infof("%s - finshed cert generation", n.Name())
	}

	return resp.Failed
}

func (n *Node) ConfigPush(ctx context.Context, r io.Reader) error {
	log.Infof("%s - pushing config", n.Name())

	cfg, err := ioutil.ReadAll(r)
	cfgs := string(cfg)

	log.Debug(cfgs)

	if err != nil {
		return err
	}

	err = n.SpawnCLIConn()
	if err != nil {
		return err
	}

	defer n.cliConn.Close()

	resp, err := n.cliConn.SendConfig(cfgs)
	if err != nil {
		return err
	}

	if resp.Failed == nil {
		log.Infof("%s - finshed config push", n.Impl.Proto.Name)
	}

	return resp.Failed
}

func (n *Node) ResetCfg(ctx context.Context) error {
	log.Infof("%s resetting config", n.Name())

	err := n.SpawnCLIConn()
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

	if resp.Failed == nil {
		log.Infof("%s - finshed resetting config", n.Name())
	}

	return resp.Failed
}

func defaults(pb *tpb.Node) *tpb.Node {
	if pb == nil {
		pb = &tpb.Node{
			Name: "default_ceos_node",
		}
	}
	if pb.Constraints == nil {
		pb.Constraints = map[string]string{
			"cpu":    "0.5",
			"memory": "1Gi",
		}
	}
	if pb.Services == nil {
		pb.Services = map[uint32]*tpb.Service{
			443: {
				Name:     "ssl",
				Inside:   443,
			},
			22: {
				Name:     "ssh",
				Inside:   22,
			},
			6030: {
				Name:     "gnmi",
				Inside:   6030,
			},
		}
	}
	if pb.Labels == nil {
		pb.Labels = map[string]string{
			"type":    tpb.Node_ARISTA_CEOS.String(),
			"vendor":  tpb.Vendor_ARISTA.String(),
			"model":   pb.Model,
			"os":      pb.Os,
			"version": pb.Version,
		}
	} else {
		if pb.Labels["type"] == "" {
			pb.Labels["type"] = tpb.Node_ARISTA_CEOS.String()
		}
		if pb.Labels["vendor"] == "" {
			pb.Labels["vendor"] = tpb.Vendor_ARISTA.String()
		}
		if pb.Labels["model"] == "" {
			pb.Labels["model"] = pb.Model
		}
		if pb.Labels["os"] == "" {
			pb.Labels["os"] = pb.Os
		}
		if pb.Labels["version"] == "" {
			pb.Labels["version"] = pb.Version
		}
	}
	if pb.Config == nil {
		pb.Config = &tpb.Config{}
	}
	if len(pb.Config.GetCommand()) == 0 {
		pb.Config.Command = []string{
			"/sbin/init",
			"systemd.setenv=INTFTYPE=eth",
			"systemd.setenv=ETBA=1",
			"systemd.setenv=SKIP_ZEROTOUCH_BARRIER_IN_SYSDBINIT=1",
			"systemd.setenv=CEOS=1",
			"systemd.setenv=EOS_PLATFORM=ceoslab",
			"systemd.setenv=container=docker",
		}
	}
	if pb.Config.Image == "" {
		pb.Config.Image = "ceos:latest"
	}
	if pb.Config.Env == nil {
		pb.Config.Env = map[string]string{
			"CEOS":                                "1",
			"EOS_PLATFORM":                        "ceoslab",
			"container":                           "docker",
			"ETBA":                                "1",
			"SKIP_ZEROTOUCH_BARRIER_IN_SYSDBINIT": "1",
			"INTFTYPE":                            "eth",
		}
	} else {
		if pb.Config.Env["CEOS"] == "" {
			pb.Config.Env["CEOS"] = "1"
		}
		if pb.Config.Env["EOS_PLATFORM"] == "" {
			pb.Config.Env["EOS_PLATFORM"] = "ceoslab"
		}
		if pb.Config.Env["container"] == "" {
			pb.Config.Env["container"] = "docker"
		}
		if pb.Config.Env["ETBA"] == "" {
			pb.Config.Env["ETBA"] = "1"
		}
		if pb.Config.Env["SKIP_ZEROTOUCH_BARRIER_IN_SYSDBINIT"] == "" {
			pb.Config.Env["SKIP_ZEROTOUCH_BARRIER_IN_SYSDBINIT"] = "1"
		}
		if pb.Config.Env["INTFTYPE"] == "" {
			pb.Config.Env["INTFTYPE"] = "eth"
		}
	}
	if pb.Config.EntryCommand == "" {
		pb.Config.EntryCommand = fmt.Sprintf("kubectl exec -it %s -- Cli", pb.Name)
	}
	if pb.Config.ConfigPath == "" {
		pb.Config.ConfigPath = "/mnt/flash"
	}
	if pb.Config.ConfigFile == "" {
		pb.Config.ConfigFile = "startup-config"
	}
	return pb
}

func (n *Node) FixInterfaces() {
	for k, v := range n.Proto.Interfaces {
		if !strings.HasPrefix(k, "eth") {
			continue
		}
		if v.Name == "" {
			n.Proto.Interfaces[k].Name = fmt.Sprintf("Ethernet%s", strings.TrimPrefix(k, "eth"))
		}
	}
}

func init() {
	node.Register(tpb.Node_ARISTA_CEOS, New)
	node.Vendor(tpb.Vendor_ARISTA, New)
}
