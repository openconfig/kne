// Copyright 2022 Google LLC
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

// Package openconfig implmements node definitions for nodes from the
// OpenConfig vendor. It implements both a device (model: lemming) and
// an ATE (model: magna).
package openconfig

import (
	"context"
	"fmt"
	"io"
	"math"

	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	"github.com/openconfig/lemming/operator/api/clientset"
	lemmingv1 "github.com/openconfig/lemming/operator/api/lemming/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	log "k8s.io/klog/v2"
)

const (
	// modelMagna is a string used in the topology to specify that a magna (github.com/openconfig/magna)
	// ATE instance should be created.
	modelMagna string = "MAGNA"
	// modelLemming is a string used in the topology to specify that a lemming (github.com/openconfig/lemming)
	// device instance should be created.
	modelLemming string = "LEMMING"

	defaultLemmingCPU = "0.5"
	defaultLemmingMem = "1Gi"
)

var (
	defaultLemmingNode = tpb.Node{
		Name: "default_lemming_node",
		Services: map[uint32]*tpb.Service{
			// https://www.iana.org/assignments/service-names-port-numbers/service-names-port-numbers.xhtml?search=gnmi
			9339: {
				Names:  []string{"gnmi", "gnoi", "gnsi"},
				Inside: 9339,
			},
			9340: {
				Names:  []string{"gribi"},
				Inside: 9340,
			},
		},
		Labels: map[string]string{
			"vendor":              tpb.Vendor_OPENCONFIG.String(),
			node.OndatraRoleLabel: node.OndatraRoleDUT,
		},
		Constraints: map[string]string{
			"cpu":    defaultLemmingCPU,
			"memory": defaultLemmingMem,
		},
		Config: &tpb.Config{
			Image:        "us-west1-docker.pkg.dev/openconfig-lemming/release/lemming:ga",
			InitImage:    node.DefaultInitContainerImage,
			Command:      []string{"/lemming/lemming"},
			EntryCommand: fmt.Sprintf("kubectl exec -it %s -- /bin/bash", "default_lemming_node"),
			Cert: &tpb.CertificateCfg{
				Config: &tpb.CertificateCfg_SelfSigned{
					SelfSigned: &tpb.SelfSignedCertCfg{
						CommonName: "default_lemming_node",
						KeySize:    2048,
					},
				},
			},
		},
	}

	defaultMagnaNode = tpb.Node{
		Name: "default_magna_node",
		Services: map[uint32]*tpb.Service{
			40051: {
				Names:  []string{"grpc"},
				Inside: 40051,
			},
			50051: {
				Names:  []string{"gnmi"},
				Inside: 50051,
			},
		},
		Labels: map[string]string{
			"vendor":              tpb.Vendor_OPENCONFIG.String(),
			node.OndatraRoleLabel: node.OndatraRoleATE,
		},
		Config: &tpb.Config{
			Image: "magna:latest",
			Command: []string{
				"/app/magna",
				"-v=2",
				"-alsologtostderr",
				"-port=40051",
				"-telemetry_port=50051",
				"-certfile=/data/cert.pem",
				"-keyfile=/data/key.pem",
			},
			EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", "default_magna_node"),
		},
	}
)

// New creates a new instance of a node based on the specified model.
func New(nodeImpl *node.Impl) (node.Node, error) {
	if nodeImpl == nil {
		return nil, fmt.Errorf("nodeImpl cannot be nil")
	}
	if nodeImpl.Proto == nil {
		return nil, fmt.Errorf("nodeImpl.Proto cannot be nil")
	}

	var cfg *tpb.Node
	switch nodeImpl.Proto.Model {
	case modelLemming:
		cfg = lemmingDefaults(nodeImpl.Proto)
	case modelMagna:
		cfg = magnaDefaults(nodeImpl.Proto)
	default:
		return nil, fmt.Errorf("a model must be specified")
	}

	nodeImpl.Proto = cfg
	n := &Node{
		Impl: nodeImpl,
	}
	return n, nil
}

type Node struct {
	*node.Impl
}

// Add validations for interfaces the node provides
var (
	_ node.Certer       = (*Node)(nil)
	_ node.ConfigPusher = (*Node)(nil)
	_ node.Resetter     = (*Node)(nil)
)

var clientFn = func(c *rest.Config) (clientset.Interface, error) {
	return clientset.NewForConfig(c)
}

func (n *Node) Create(ctx context.Context) error {
	switch n.Impl.Proto.Model {
	case modelLemming:
		return n.lemmingCreate(ctx)
	case modelMagna:
		// magna uses the standard pod creation as though it were a host.
		return n.Impl.Create(ctx)
	default:
		return fmt.Errorf("cannot create an instance of an unknown model")
	}
}

// lemmingCreate implements the Create function for the lemming model devices.
func (n *Node) lemmingCreate(ctx context.Context) error {
	nodeSpec := n.GetProto()
	config := nodeSpec.GetConfig()
	log.Infof("create lemming %q", nodeSpec.Name)

	ports := map[string]lemmingv1.ServicePort{}

	for k, v := range n.Proto.Services {
		insidePort := v.Inside
		if insidePort > math.MaxUint16 {
			return fmt.Errorf("inside port %d out of range (max: %d)", insidePort, math.MaxUint16)
		}
		if k > math.MaxUint16 {
			return fmt.Errorf("outside port %d out of range (max: %d)", k, math.MaxUint16)
		}
		ports[v.Name] = lemmingv1.ServicePort{
			InnerPort: int32(insidePort),
			OuterPort: int32(k),
		}
	}

	dut := &lemmingv1.Lemming{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nodeSpec.Name,
			Namespace: n.Namespace,
			Labels:    nodeSpec.Labels,
		},
		Spec: lemmingv1.LemmingSpec{
			Image:          config.Image,
			Command:        config.Command[0],
			Args:           config.Args,
			Env:            node.ToEnvVar(config.Env),
			ConfigPath:     config.ConfigPath,
			ConfigFile:     config.ConfigFile,
			InitImage:      config.InitImage,
			Ports:          ports,
			InterfaceCount: len(nodeSpec.Interfaces) + 1,
			InitSleep:      int(config.Sleep),
			Resources:      node.ToResourceRequirements(nodeSpec.Constraints),
		},
	}
	if config.Cert != nil {
		switch tls := config.Cert.Config.(type) {
		case *tpb.CertificateCfg_SelfSigned:
			dut.Spec.TLS = &lemmingv1.TLSSpec{
				SelfSigned: &lemmingv1.SelfSignedSpec{
					CommonName: tls.SelfSigned.CommonName,
					KeySize:    int(tls.SelfSigned.KeySize),
				},
			}
		}
	}

	cs, err := clientFn(n.RestConfig)
	if err != nil {
		return fmt.Errorf("failed to get kubernetes client: %v", err)
	}
	if _, err := cs.LemmingV1alpha1().Lemmings(n.Namespace).Create(ctx, dut, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("failed to create lemming: %v", err)
	}
	return nil
}

func (n *Node) Status(ctx context.Context) (node.Status, error) {
	switch n.Impl.Proto.Model {
	case modelMagna:
		// magna's status uses the standard underlying node implementation.
		return n.Impl.Status(ctx)
	case modelLemming:
		return n.lemmingStatus(ctx)
	default:
		return node.StatusUnknown, fmt.Errorf("invalid model specified.")
	}
}

func (n *Node) DefaultNodeSpec() *tpb.Node {
	switch n.Impl.Proto.Model {
	case modelMagna:
		return proto.Clone(&defaultMagnaNode).(*tpb.Node)
	case modelLemming:
		return proto.Clone(&defaultLemmingNode).(*tpb.Node)
	default:
		return nil
	}
}

func (n *Node) lemmingStatus(ctx context.Context) (node.Status, error) {
	cs, err := clientFn(n.RestConfig)
	if err != nil {
		return node.StatusUnknown, err
	}
	got, err := cs.LemmingV1alpha1().Lemmings(n.Namespace).Get(ctx, n.Name(), metav1.GetOptions{})
	if err != nil {
		return node.StatusUnknown, err
	}
	switch got.Status.Phase {
	case lemmingv1.Running:
		return node.StatusRunning, nil
	case lemmingv1.Failed:
		return node.StatusFailed, nil
	case lemmingv1.Pending:
		return node.StatusPending, nil
	default:
		return node.StatusUnknown, nil
	}
}

func (n *Node) Delete(ctx context.Context) error {
	switch n.Impl.Proto.Model {
	case modelMagna:
		// magna's implementation uses the standard underlying node implementation.
		return n.Impl.Delete(ctx)
	case modelLemming:
		return n.lemmingDelete(ctx)
	default:
		return fmt.Errorf("unknown model")
	}
}

func (n *Node) lemmingDelete(ctx context.Context) error {
	cs, err := clientFn(n.RestConfig)
	if err != nil {
		return err
	}
	return cs.LemmingV1alpha1().Lemmings(n.Namespace).Delete(ctx, n.Name(), metav1.DeleteOptions{})
}

func (n *Node) ResetCfg(ctx context.Context) error {
	log.Info("ResetCfg is a noop.")
	return nil
}

func (n *Node) ConfigPush(context.Context, io.Reader) error {
	return status.Errorf(codes.Unimplemented, "config push is not implemented using gNMI to configure device")
}

func (n *Node) GenerateSelfSigned(context.Context) error {
	return status.Errorf(codes.Unimplemented, "certificate generation is not supported")
}

func lemmingDefaults(pb *tpb.Node) *tpb.Node {
	defaultNodeClone := proto.Clone(&defaultLemmingNode).(*tpb.Node)
	if pb.Config == nil {
		pb.Config = &tpb.Config{}
	}
	if pb.Config.Image == "" {
		pb.Config.Image = defaultNodeClone.Config.Image
	}
	if pb.Config.InitImage == "" {
		pb.Config.InitImage = node.DefaultInitContainerImage
	}
	if len(pb.GetConfig().GetCommand()) == 0 {
		pb.Config.Command = defaultNodeClone.Config.Command
	}
	if pb.Config.EntryCommand == "" {
		pb.Config.EntryCommand = fmt.Sprintf("kubectl exec -it %s -- /bin/bash", pb.Name)
	}
	if pb.Config.Cert == nil {
		pb.Config.Cert = &tpb.CertificateCfg{
			Config: &tpb.CertificateCfg_SelfSigned{
				SelfSigned: &tpb.SelfSignedCertCfg{
					CommonName: pb.Name,
					KeySize:    2048,
				},
			},
		}
	}
	if pb.Constraints == nil {
		pb.Constraints = defaultNodeClone.Constraints
	}
	if pb.Constraints["cpu"] == "" {
		pb.Constraints["cpu"] = defaultLemmingCPU
	}
	if pb.Constraints["memory"] == "" {
		pb.Constraints["memory"] = defaultLemmingMem
	}
	if pb.Labels == nil {
		pb.Labels = defaultNodeClone.Labels
	}
	if pb.Labels["vendor"] == "" {
		pb.Labels["vendor"] = defaultNodeClone.Labels["vendor"]
	}

	// Always explicitly specify that lemming is a DUT, this cannot be overridden by the user.
	pb.Labels[node.OndatraRoleLabel] = node.OndatraRoleDUT

	if pb.Services == nil {
		pb.Services = defaultNodeClone.Services
	}
	return pb
}

func magnaDefaults(pb *tpb.Node) *tpb.Node {
	if pb.Config == nil {
		pb.Config = &tpb.Config{}
	}
	if pb.Services == nil {
		pb.Services = map[uint32]*tpb.Service{}
	}
	if len(pb.GetConfig().GetCommand()) == 0 {
		pb.Config.Command = []string{
			"/app/magna",
			"-v=2",
			"-alsologtostderr",
			"-port=40051",
			"-telemetry_port=50051",
			"-certfile=/data/cert.pem",
			"-keyfile=/data/key.pem",
		}
	}
	if pb.Config.EntryCommand == "" {
		pb.Config.EntryCommand = fmt.Sprintf("kubectl exec -it %s -- sh", pb.Name)
	}
	if pb.Config.Image == "" {
		// TODO(robjs): add public container location once the first iteration is pushed.
		// Currently, this image can be built from github.com/openconfig/magna.
		pb.Config.Image = "magna:latest"
	}

	if _, ok := pb.Services[40051]; !ok {
		pb.Services[40051] = &tpb.Service{
			Names:  []string{"grpc"},
			Inside: 40051,
		}
	}

	if _, ok := pb.Services[50051]; !ok {
		pb.Services[50051] = &tpb.Service{
			Names:  []string{"gnmi"},
			Inside: 50051,
		}
	}

	if pb.Labels == nil {
		pb.Labels = map[string]string{
			"vendor": tpb.Vendor_OPENCONFIG.String(),
		}
	}

	// Always explicitly specify that magna nodes are ATEs, this cannot be overridden by the user.
	pb.Labels[node.OndatraRoleLabel] = node.OndatraRoleATE

	return pb
}

func init() {
	node.Vendor(tpb.Vendor_OPENCONFIG, New)
}
