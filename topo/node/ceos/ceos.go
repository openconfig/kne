// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
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
	"regexp"
	"strings"
	"time"

	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	scraplinetwork "github.com/scrapli/scrapligo/driver/network"
	scrapliopts "github.com/scrapli/scrapligo/driver/options"
	scrapliutil "github.com/scrapli/scrapligo/util"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"

	ceos "github.com/aristanetworks/arista-ceoslab-operator/api/v1alpha1"
	ceosclient "github.com/aristanetworks/arista-ceoslab-operator/api/v1alpha1/clientset"
)

const (
	scrapliPlatformName = "arista_eos"
)

// ErrIncompatibleCliConn raised when an invalid scrapligo cli transport type is found.
var ErrIncompatibleCliConn = errors.New("incompatible cli connection in use")

type Node struct {
	*node.Impl
	cliConn *scraplinetwork.Driver

	// scrapli options used in testing
	testOpts []scrapliutil.Option
}

// Add validations for interfaces the node provides
var (
	_ node.Certer       = (*Node)(nil)
	_ node.ConfigPusher = (*Node)(nil)
	_ node.Resetter     = (*Node)(nil)

	ethIntfRe  = regexp.MustCompile(`^Ethernet\d+(?:/\d+)?(?:/\d+)?$`)
	mgmtIntfRe = regexp.MustCompile(`^Management\d+(?:/\d+)?$`)
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
	err := n.FixInterfaces()
	if err != nil {
		return nil, err
	}
	return n, nil
}

func (n *Node) Create(ctx context.Context) error {
	if err := n.CreateConfig(ctx); err != nil {
		return fmt.Errorf("node %s failed to create config-map %w", n.Name(), err)
	}
	if err := n.CreateCRD(ctx); err != nil {
		return fmt.Errorf("node %s failed to create custom resource definition %w", n.Name(), err)
	}
	return nil
}

func (n *Node) CreateCRD(ctx context.Context) error {
	log.Infof("Creating new CEosLabDevice CRD for node: %v", n.Name())
	proto := n.GetProto()
	config := proto.GetConfig()
	device := &ceos.CEosLabDevice{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "ceoslab.arista.com/v1alpha1",
			Kind:       "CEosLabDevice",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      n.Name(),
			Namespace: n.GetNamespace(),
			Labels: map[string]string{
				"app":  n.Name(),
				"topo": n.GetNamespace(),
			},
		},
		Spec: ceos.CEosLabDeviceSpec{
			EnvVar:             config.GetEnv(),
			Image:              config.GetImage(),
			InitContainerImage: config.GetInitImage(),
			Args:               config.GetArgs(),
			Resources:          proto.GetConstraints(),
			NumInterfaces:      uint32(len(proto.GetInterfaces())),
			Sleep:              config.GetSleep(),
		},
	}
	for label, v := range proto.GetLabels() {
		device.ObjectMeta.Labels[label] = v
	}
	for _, service := range proto.GetServices() {
		if device.Spec.Services == nil {
			device.Spec.Services = map[string]ceos.ServiceConfig{}
		}
		device.Spec.Services[service.Name] = ceos.ServiceConfig{
			TCPPorts: []ceos.PortConfig{{
				In:  service.Inside,
				Out: service.Outside,
			}},
		}
	}
	if cert := config.GetCert(); cert != nil {
		if ssCert := cert.GetSelfSigned(); ssCert != nil {
			certConfig := ceos.CertConfig{
				SelfSignedCerts: []ceos.SelfSignedCertConfig{{
					CertName:   ssCert.CertName,
					KeyName:    ssCert.KeyName,
					KeySize:    ssCert.KeySize,
					CommonName: ssCert.CommonName,
				}},
			}
			device.Spec.CertConfig = certConfig
		}
	}
	for k, v := range n.GetProto().GetInterfaces() {
		if device.Spec.IntfMapping == nil {
			device.Spec.IntfMapping = map[string]string{}
		}
		device.Spec.IntfMapping[k] = v.GetName()
	}
	// Post to k8s
	client, err := ceosclient.NewForConfig(n.RestConfig)
	if err != nil {
		return err
	}
	_, err = client.CEosLabDevices(n.Namespace).Create(ctx, device, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	// Wait for pods
	w, err := n.KubeClient.CoreV1().Pods(n.Namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: fields.SelectorFromSet(fields.Set{metav1.ObjectNameField: n.Name()}).String(),
	})
	if err != nil {
		return err
	}
	for e := range w.ResultChan() {
		p := e.Object.(*corev1.Pod)
		if p.Status.Phase == corev1.PodPending {
			break
		}
	}
	log.Infof("Created CEosLabDevice CRD for node: %v", n.Name())
	return err
}

func (n *Node) Delete(ctx context.Context) error {
	client, err := ceosclient.NewForConfig(n.RestConfig)
	if err != nil {
		return err
	}
	err = client.CEosLabDevices(n.Namespace).Delete(ctx, n.Name(), metav1.DeleteOptions{})
	if err != nil {
		return err
	}
	if err := n.DeleteConfig(ctx); err != nil {
		return err
	}
	log.Infof("Deleted CEosLabDevice resources of node: %v", n.Name())
	return nil
}

// SpawnCLIConn spawns a CLI connection towards a Network OS using `kubectl exec` terminal and ensures CLI is ready
// to accept inputs.
// scrapligo options can be provided to this function for a caller to modify scrapligo platform.
// For example, mock transport can be set via options
func (n *Node) SpawnCLIConn() error {
	opts := []scrapliutil.Option{
		scrapliopts.WithAuthBypass(),
	}

	// add options defined in test package
	opts = append(opts, n.testOpts...)

	opts = n.PatchCLIConnOpen("kubectl", []string{"Cli"}, opts)

	var err error
	n.cliConn, err = n.GetCLIConn(scrapliPlatformName, opts)

	return err
}

func (n *Node) GenerateSelfSigned(ctx context.Context) error {
	return status.Errorf(codes.Unimplemented, "Node %q does not implement Certer interface. "+
		"To configure a certificate on a cEOS-lab device, define the certificate in the "+
		"topology file or patch the certificate configuration into the node's "+
		"CEosLabDevice custom resource instance.", n.Name())
}

func (n *Node) ConfigPush(ctx context.Context, r io.Reader) error {
	log.Infof("%s - pushing config", n.Name())

	cfg, err := io.ReadAll(r)
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
		scrapliopts.WithTimeoutOps(300*time.Second),
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
				Name:   "ssl",
				Inside: 443,
			},
			22: {
				Name:   "ssh",
				Inside: 22,
			},
			6030: {
				Name:   "gnmi",
				Inside: 6030,
			},
		}
	}
	if pb.Labels == nil {
		pb.Labels = map[string]string{}
	}
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
	if pb.Config == nil {
		pb.Config = &tpb.Config{}
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

func (n *Node) FixInterfaces() error {
	for k, v := range n.Proto.Interfaces {
		switch {
		default:
			return fmt.Errorf("Unrecognized interface name: %s", v.Name)
		case !strings.HasPrefix(k, "eth"), ethIntfRe.MatchString(v.Name), mgmtIntfRe.MatchString(v.Name):
		case v.Name == "":
			n.Proto.Interfaces[k].Name = fmt.Sprintf("Ethernet%s", strings.TrimPrefix(k, "eth"))
		}
	}
	return nil
}

func init() {
	node.Register(tpb.Node_ARISTA_CEOS, New)
	node.Vendor(tpb.Vendor_ARISTA, New)
}
