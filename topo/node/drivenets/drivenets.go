/*
Copyright 2023 nhadar-dn.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package drivenets implmements node definitions for nodes from the
// Drivenets vendor. It implements a device from model cdnos
package drivenets

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	cdnosv1 "github.com/drivenets/cdnos-controller/api/v1"
	"github.com/drivenets/cdnos-controller/api/v1/clientset"
	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	log "k8s.io/klog/v2"
)

const (
	// modelCdnos is a string used in the topology to specify that a cdnos
	// device instance should be created.
	modelCdnos string = "CDNOS"
)

// New creates a new instance of a node based on the specified model.
func New(nodeImpl *node.Impl) (node.Node, error) {
	if nodeImpl == nil {
		return nil, fmt.Errorf("nodeImpl cannot be nil")
	}
	if nodeImpl.Proto == nil {
		return nil, fmt.Errorf("nodeImpl.Proto cannot be nil")
	}

	if nodeImpl.Proto.Model != modelCdnos {
		return nil, fmt.Errorf("unknown model")
	}

	nodeImpl.Proto = cdnosDefaults(nodeImpl.Proto)
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
	if n.Impl.Proto.Model != modelCdnos {
		return fmt.Errorf("cannot create an instance of an unknown model")
	}
	return n.cdnosCreate(ctx)
}

// cdnosCreate implements the Create function for the cdnos model devices.
func (n *Node) cdnosCreate(ctx context.Context) error {
	log.Infof("Creating Cdnos node resource %s", n.Name())

	if _, err := n.CreateConfig(ctx); err != nil {
		return fmt.Errorf("node %s failed to create config-map %w", n.Name(), err)
	}
	log.Infof("Created Cdnos %s configmap", n.Name())

	nodeSpec := n.GetProto()
	config := nodeSpec.GetConfig()
	log.Infof("create cdnos %q", nodeSpec.Name)

	ports := map[string]cdnosv1.ServicePort{}

	for k, v := range n.Proto.Services {
		ports[v.Name] = cdnosv1.ServicePort{
			InnerPort: int32(v.Inside),
			OuterPort: int32(k),
		}
	}

	dut := &cdnosv1.Cdnos{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nodeSpec.Name,
			Namespace: n.Namespace,
			Labels:    nodeSpec.Labels,
		},
		Spec: cdnosv1.CdnosSpec{
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
			Labels:         nodeSpec.Labels,
		},
	}
	if config.Cert != nil {
		switch tls := config.Cert.Config.(type) {
		case *tpb.CertificateCfg_SelfSigned:
			dut.Spec.TLS = &cdnosv1.TLSSpec{
				SelfSigned: &cdnosv1.SelfSignedSpec{
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
	if _, err := cs.CdnosV1alpha1().Cdnoss(n.Namespace).Create(ctx, dut, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("failed to create cdnos: %v", err)
	}
	return nil
}

func (n *Node) Status(ctx context.Context) (node.Status, error) {
	if n.Impl.Proto.Model != modelCdnos {
		return node.StatusUnknown, fmt.Errorf("invalid model specified")
	}
	return n.cdnosStatus(ctx)
}

func (n *Node) cdnosStatus(ctx context.Context) (node.Status, error) {
	cs, err := clientFn(n.RestConfig)
	if err != nil {
		return node.StatusUnknown, err
	}
	got, err := cs.CdnosV1alpha1().Cdnoss(n.Namespace).Get(ctx, n.Name(), metav1.GetOptions{})
	if err != nil {
		return node.StatusUnknown, err
	}
	switch got.Status.Phase {
	case cdnosv1.Running:
		return node.StatusRunning, nil
	case cdnosv1.Failed:
		return node.StatusFailed, nil
	case cdnosv1.Pending:
		return node.StatusPending, nil
	default:
		return node.StatusUnknown, nil
	}
}

func (n *Node) Delete(ctx context.Context) error {
	if n.Impl.Proto.Model != modelCdnos {
		return fmt.Errorf("unknown model")
	}
	return n.cdnosDelete(ctx)
}

func (n *Node) cdnosDelete(ctx context.Context) error {
	cs, err := clientFn(n.RestConfig)
	if err != nil {
		return err
	}
	return cs.CdnosV1alpha1().Cdnoss(n.Namespace).Delete(ctx, n.Name(), metav1.DeleteOptions{})
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

func cdnosDefaults(pb *tpb.Node) *tpb.Node {
	if pb.Config == nil {
		pb.Config = &tpb.Config{}
	}
	if pb.Config.Image == "" {
		pb.Config.Image = "registry.dev.drivenets.net/devops/cdnos:latest"
	}
	if pb.Config.InitImage == "" {
		pb.Config.InitImage = node.DefaultInitContainerImage
	}
	if len(pb.GetConfig().GetCommand()) == 0 {
		pb.Config.Command = []string{"/define_notif_net.sh"}
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
		pb.Constraints = map[string]string{}
	}
	if pb.Constraints["cpu"] == "" {
		pb.Constraints["cpu"] = "0.5"
	}
	if pb.Constraints["memory"] == "" {
		pb.Constraints["memory"] = "1Gi"
	}
	if pb.Labels == nil {
		pb.Labels = map[string]string{}
	}
	if pb.Labels["vendor"] == "" {
		pb.Labels["vendor"] = tpb.Vendor_DRIVENETS.String()
	}

	// Always explicitly specify that cdnos is a DUT, this cannot be overridden by the user.
	pb.Labels[node.OndatraRoleLabel] = node.OndatraRoleDUT

	if pb.Services == nil {
		pb.Services = map[uint32]*tpb.Service{
			// https://www.iana.org/assignments/service-names-port-numbers/service-names-port-numbers.xhtml?search=gnmi
			9339: {
				Names:  []string{"gnmi", "gnoi", "gnsi"},
				Inside: 9339,
			},
			9340: {
				Names:  []string{"gribi"},
				Inside: 9340,
			},
			830: {
				Names:  []string{"netconf"},
				Inside: 830,
			},
		}
	}
	return pb
}

func init() {
	node.Vendor(tpb.Vendor_DRIVENETS, New)
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
	if data == nil {
		return nil, nil
	}
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
	volume := &corev1.Volume{
		Name: fmt.Sprintf("%s-config-volume", pb.Name),
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: sCM.Name, // reference to your ConfigMap
				},
			},
		},
	}
	return volume, nil
}
