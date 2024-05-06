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
package arista

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	cpb "github.com/openconfig/kne/proto/ceos"
	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	scraplinetwork "github.com/scrapli/scrapligo/driver/network"
	scrapliopts "github.com/scrapli/scrapligo/driver/options"
	scrapliutil "github.com/scrapli/scrapligo/util"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	log "k8s.io/klog/v2"

	ceos "github.com/aristanetworks/arista-ceoslab-operator/v2/api/v1alpha1"
	ceosclient "github.com/aristanetworks/arista-ceoslab-operator/v2/api/v1alpha1/dynamic"
)

const (
	scrapliPlatformName = "arista_eos"
)

var (
	// ErrIncompatibleCliConn raised when an invalid scrapligo cli transport type is found.
	ErrIncompatibleCliConn = errors.New("incompatible cli connection in use")
	// Function to get client, by default this is a proper client. This can be set to a fake
	// for unit testing.
	newClient = ceosclient.NewForConfig
)

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
	if _, err := n.CreateConfig(ctx); err != nil {
		return fmt.Errorf("node %s failed to create config-map %w", n.Name(), err)
	}
	if err := n.CreateCRD(ctx); err != nil {
		return fmt.Errorf("node %s failed to create custom resource definition %w", n.Name(), err)
	}
	return nil
}

func (n *Node) Status(ctx context.Context) (node.Status, error) {
	w, err := n.KubeClient.CoreV1().Pods(n.Namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: fields.SelectorFromSet(fields.Set{metav1.ObjectNameField: n.Name()}).String(),
	})
	if err != nil {
		return node.StatusFailed, err
	}
	status := node.StatusUnknown
	for e := range w.ResultChan() {
		p, ok := e.Object.(*corev1.Pod)
		if !ok {
			continue
		}
		if p.Status.Phase == corev1.PodPending {
			status = node.StatusPending
			break
		}
		if p.Status.Phase == corev1.PodRunning {
			status = node.StatusRunning
			break
		}
	}
	return status, nil
}

func (n *Node) CreateConfig(ctx context.Context) (*corev1.Volume, error) {
	pb := n.Proto
	var data []byte
	switch v := pb.Config.GetConfigData().(type) {
	case *tpb.Config_File:
		var err error
		data, err = os.ReadFile(filepath.Join(n.BasePath, v.File))
		if err != nil {
			return nil, err
		}
	case *tpb.Config_Data:
		data = v.Data
	}
	if data != nil {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("%s-config", pb.Name),
			},
			Data: map[string]string{
				pb.Config.ConfigFile: string(data),
			},
		}
		sCM, err := n.KubeClient.CoreV1().ConfigMaps(n.Namespace).Create(ctx, cm, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
		log.V(1).Infof("Server Config Map:\n%v\n", sCM)
	}
	return nil, nil
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
			NumInterfaces:      int32(len(proto.GetInterfaces())),
			Sleep:              int32(config.GetSleep()),
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
				In:  int32(service.Inside),
				Out: int32(service.Outside),
			}},
		}
	}
	if cert := config.GetCert(); cert != nil {
		if ssCert := cert.GetSelfSigned(); ssCert != nil {
			certConfig := ceos.CertConfig{
				SelfSignedCerts: []ceos.SelfSignedCertConfig{{
					CertName:   ssCert.CertName,
					KeyName:    ssCert.KeyName,
					KeySize:    int32(ssCert.KeySize),
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
	if vendorData := config.GetVendorData(); vendorData != nil {
		ceosLabConfig := &cpb.CEosLabConfig{}
		if err := vendorData.UnmarshalTo(ceosLabConfig); err != nil {
			return err
		}
		if toggleOverrides := ceosLabConfig.GetToggleOverrides(); toggleOverrides != nil {
			device.Spec.ToggleOverrides = toggleOverrides
		}
		if waitForAgents := ceosLabConfig.GetWaitForAgents(); waitForAgents != nil {
			device.Spec.WaitForAgents = waitForAgents
		}
	}
	// Post to k8s
	client, err := newClient(n.RestConfig)
	if err != nil {
		return err
	}
	_, err = client.CEosLabDevices(n.Namespace).Create(ctx, device, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	log.Infof("Created CEosLabDevice CRD for node: %v", n.Name())
	return nil
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

	log.V(1).Info(cfgs)

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
		log.Infof("%s - finished config push", n.Impl.Proto.Name)
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
		"configure replace startup-config",
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
	pb = constraints(pb)
	if pb.Services == nil {
		pb.Services = map[uint32]*tpb.Service{
			443: {
				Names:  []string{"ssl"},
				Inside: 443,
			},
			22: {
				Names:  []string{"ssh"},
				Inside: 22,
			},
			6030: {
				Names:  []string{"gnmi", "gnoi"},
				Inside: 6030,
			},
			9340: {
				Names:  []string{"gribi"},
				Inside: 9340,
			},
			9559: {
				Names:  []string{"p4rt"},
				Inside: 9559,
			},
		}
	}
	if pb.Os == "" {
		pb.Os = "eos"
	}
	if pb.Model == "" {
		pb.Model = "ceos"
	}
	if pb.Labels == nil {
		pb.Labels = map[string]string{}
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
	if pb.Labels[node.OndatraRoleLabel] == "" {
		pb.Labels[node.OndatraRoleLabel] = node.OndatraRoleDUT
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
	if pb.Config.Image == "" {
		pb.Config.Image = "ceos:latest"
	}
	if pb.Config.Cert == nil {
		pb.Config.Cert = &tpb.CertificateCfg{
			Config: &tpb.CertificateCfg_SelfSigned{
				SelfSigned: &tpb.SelfSignedCertCfg{
					CertName: "gnmiCert.pem",
					KeyName:  "gnmiCertKey.pem",
					KeySize:  4096,
				},
			},
		}
	}
	return pb
}

func constraints(pb *tpb.Node) *tpb.Node {
	if pb.Constraints == nil {
		pb.Constraints = map[string]string{}
	}
	if pb.Constraints["cpu"] == "" {
		pb.Constraints["cpu"] = "0.5"
	}
	if pb.Constraints["memory"] == "" {
		pb.Constraints["memory"] = "1Gi"
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
	node.Vendor(tpb.Vendor_ARISTA, New)
}
