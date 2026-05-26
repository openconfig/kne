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
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/drivenets/cdnos-controller/api/v1/clientset"
	"github.com/openconfig/kne/topo/node"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	cdnosv1 "github.com/drivenets/cdnos-controller/api/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	log "k8s.io/klog/v2"

	tpb "github.com/openconfig/kne/proto/topo"
)

var (
	// modelCdnos is a list of model names that should be treated as cdnos devices.
	// Accept both CDNOS and MCDNOS (case-insensitive).
	modelCdnos = []string{"CDNOS", "MCDNOS"}
)

// isModelCdnos returns true if the provided model is one of the supported
// cdnos-family models (cdnos or mcdnos), case-insensitive.
func isModelCdnos(model string) bool {
	upper := strings.ToUpper(model)
	for _, m := range modelCdnos {
		if upper == m {
			return true
		}
	}
	return false
}

// extractNodeSelector extracts nodeSelector from constraints map.
// Supports two formats:
// - Node selector via key prefix "nodeSelector.<labelKey>" => "<value>"
// - Node selector via key "nodeSelector" => "key1=val1,key2=val2"
func extractNodeSelector(constraints map[string]string) map[string]string {
	if constraints == nil {
		return nil
	}
	nodeSelector := make(map[string]string)
	// nodeSelector.<key>: value
	for k, v := range constraints {
		if strings.HasPrefix(k, "nodeSelector.") {
			labelKey := strings.TrimPrefix(k, "nodeSelector.")
			if labelKey != "" {
				nodeSelector[labelKey] = v
			}
		}
	}
	// nodeSelector: "k1=v1,k2=val2"
	if raw := constraints["nodeSelector"]; raw != "" {
		pairs := strings.Split(raw, ",")
		for _, p := range pairs {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			kv := strings.SplitN(p, "=", 2)
			if len(kv) == 2 {
				nodeSelector[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
			}
		}
	}
	if len(nodeSelector) == 0 {
		return nil
	}
	return nodeSelector
}

var (
	defaultConstraints = node.Constraints{
		CPU:    "2",
		Memory: "4Gi",
	}
	defaultNode = tpb.Node{
		Services: map[uint32]*tpb.Service{
			// https://www.iana.org/assignments/service-names-port-numbers/service-names-port-numbers.xhtml?search=gnmi
			22: {
				Names:  []string{"ssh"},
				Inside: 22,
			},
			830: {
				Names:  []string{"netconf"},
				Inside: 830,
			},
			50051: {
				Names:  []string{"gnmi"},
				Inside: 50051,
			},
		},
		Config: &tpb.Config{
			ConfigFile: "default",
			ConfigPath: "/config_load",
			Image:      "registry.dev.drivenets.net/devops/cdnos:latest",
			Command:    []string{"/define_notif_net.sh"},
		},
		Constraints: map[string]string{
			"cpu":    defaultConstraints.CPU,
			"memory": defaultConstraints.Memory,
		},
		Labels: map[string]string{
			"vendor":              tpb.Vendor_DRIVENETS.String(),
			node.OndatraRoleLabel: node.OndatraRoleDUT,
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

	if !isModelCdnos(nodeImpl.Proto.Model) {
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
	if !isModelCdnos(n.Impl.Proto.Model) {
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
		insidePort := v.Inside
		if insidePort > math.MaxUint16 {
			return fmt.Errorf("inside port %d out of range (max: %d)", insidePort, math.MaxUint16)
		}
		if k > math.MaxUint16 {
			return fmt.Errorf("outside port %d out of range (max: %d)", k, math.MaxUint16)
		}
		ports[v.Name] = cdnosv1.ServicePort{
			InnerPort: int32(insidePort),
			OuterPort: int32(k),
		}
	}

	// Add model to labels so the controller can access it
	labels := make(map[string]string)
	for k, v := range nodeSpec.Labels {
		labels[k] = v
	}
	labels["model"] = strings.ToUpper(nodeSpec.Model)

	dut := &cdnosv1.Cdnos{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nodeSpec.Name,
			Namespace: n.Namespace,
			Labels:    labels,
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
			InterfaceCount: len(nodeSpec.Interfaces),
			InitSleep:      int(config.Sleep),
			Resources:      node.ToResourceRequirements(nodeSpec.Constraints),
			Labels:         nodeSpec.Labels,
			NodeSelector:   extractNodeSelector(nodeSpec.Constraints),
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
	// Ensure the controller-created Service has required Azure LB annotations.
	// Azure annotations are required for proper LoadBalancer behavior.
	if err := n.annotateCdnosService(ctx); err != nil {
		return fmt.Errorf("failed to annotate service for %s: %v", n.Name(), err)
	}
	return nil
}

// annotateCdnosService waits for the controller-created Service named "service-<node>"
// and adds Azure LoadBalancer annotations when running on AKS.
func (n *Node) annotateCdnosService(ctx context.Context) error {
	if !isAzureAKS(n.KubeClient) {
		log.V(1).Infof("Not running on Azure AKS; skipping Azure LB annotations for %q", n.Name())
		return nil
	}
	log.Infof("Azure AKS detected; annotating controller-managed Services for %q", n.Name())
	deadline := time.Now().Add(10 * time.Minute)
	desired := map[string]string{
		"service.beta.kubernetes.io/azure-load-balancer-internal": "true",
	}
	// Build no-probe rules from this node's services (outside ports).
	for port := range n.Proto.Services {
		key := fmt.Sprintf("service.beta.kubernetes.io/port_%d_no_probe_rule", port)
		desired[key] = "true"
	}
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting to annotate services for %q", n.Name())
		}
		svcs, err := n.servicesForNode(ctx)
		if err != nil || len(svcs) == 0 {
			time.Sleep(1 * time.Second)
			continue
		}
		allAnnotated := true
		for i := range svcs {
			s := &svcs[i]
			changed := false
			if s.Annotations == nil {
				s.Annotations = map[string]string{}
				changed = true
			}
			for k, v := range desired {
				if s.Annotations[k] != v {
					s.Annotations[k] = v
					changed = true
				}
			}
			if changed {
				// Use a short-lived background context to avoid parent ctx cancellations.
				updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_, err := n.KubeClient.CoreV1().Services(n.Namespace).Update(updateCtx, s, metav1.UpdateOptions{})
				cancel()
				if err != nil {
					// Retry once on conflict with a fresh GET
					if apierrors.IsConflict(err) {
						getCtx, cancelGet := context.WithTimeout(context.Background(), 5*time.Second)
						fresh, gerr := n.KubeClient.CoreV1().Services(n.Namespace).Get(getCtx, s.Name, metav1.GetOptions{})
						cancelGet()
						if gerr == nil {
							if fresh.Annotations == nil {
								fresh.Annotations = map[string]string{}
							}
							for k, v := range desired {
								fresh.Annotations[k] = v
							}
							updateCtx2, cancelUpd2 := context.WithTimeout(context.Background(), 5*time.Second)
							_, uerr := n.KubeClient.CoreV1().Services(n.Namespace).Update(updateCtx2, fresh, metav1.UpdateOptions{})
							cancelUpd2()
							if uerr == nil {
								log.Infof("Annotated Service %q with Azure LB annotations (after conflict retry)", s.Name)
								continue
							}
						}
					}
					allAnnotated = false
					continue
				}
				log.Infof("Annotated Service %q with Azure LB annotations", s.Name)
			}
			// Verify
			getCtx, cancelGet := context.WithTimeout(context.Background(), 5*time.Second)
			got, err := n.KubeClient.CoreV1().Services(n.Namespace).Get(getCtx, s.Name, metav1.GetOptions{})
			cancelGet()
			if err != nil {
				allAnnotated = false
				continue
			}
			for k, v := range desired {
				if got.Annotations[k] != v {
					allAnnotated = false
					break
				}
			}
		}
		if allAnnotated {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (n *Node) Status(ctx context.Context) (node.Status, error) {
	if !isModelCdnos(n.Impl.Proto.Model) {
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
	if !isModelCdnos(n.Impl.Proto.Model) {
		return fmt.Errorf("unknown model")
	}
	return n.cdnosDelete(ctx)
}

func (n *Node) cdnosDelete(ctx context.Context) error {
	cs, err := clientFn(n.RestConfig)
	if err != nil {
		return err
	}
	// 1) Start teardown by deleting all Cdnos CRs in the namespace
	//    (controller will clean up owned objects for each).
	list, err := cs.CdnosV1alpha1().Cdnoss(n.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if len(list.Items) == 0 {
		log.V(1).Infof("No Cdnos CRs found in namespace %q", n.Namespace)
	} else {
		var crNames []string
		for _, item := range list.Items {
			crNames = append(crNames, item.Name)
		}
		log.Infof("Deleting Cdnos CRs in %q: %v", n.Namespace, crNames)
		for _, item := range list.Items {
			if err := cs.CdnosV1alpha1().Cdnoss(n.Namespace).Delete(ctx, item.Name, metav1.DeleteOptions{}); err != nil {
				return err
			}
		}
	}

	// 2) Monitor Services associated with this node until the controller removes them.
	svcs, _ := n.servicesForNode(ctx)
	if len(svcs) == 0 {
		log.V(1).Infof("No Services found for node %q", n.Name())
	} else {
		var svcNames []string
		for _, s := range svcs {
			svcNames = append(svcNames, s.Name)
		}
		log.Infof("Monitoring Services for %q to be removed by controller: %v", n.Name(), svcNames)
	}
	// Wait for Services to be removed (longer on AKS due to LoadBalancer cleanup).
	waitDeadline := time.Now().Add(2 * time.Minute)
	if isAzureAKS(n.KubeClient) {
		waitDeadline = time.Now().Add(10 * time.Minute)
		log.Infof("AKS detected; waiting up to %v for all Services to be removed", time.Until(waitDeadline).Truncate(time.Second))
	} else {
		log.V(1).Infof("Azure AKS not detected; waiting up to %v for all Services to be removed", time.Until(waitDeadline).Truncate(time.Second))
	}
	start := time.Now()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		if time.Now().After(waitDeadline) {
			log.Warningf("Timeout waiting for Services removal; continuing teardown")
			break
		}
		svcs, _ = n.servicesForNode(ctx)
		remaining := len(svcs)
		if remaining == 0 {
			log.Infof("All Services for %q removed after %v", n.Name(), time.Since(start).Truncate(time.Second))
			break
		}
		select {
		case <-ticker.C:
			var names []string
			for _, s := range svcs {
				names = append(names, s.Name)
			}
			log.Infof("Waiting for Services removal for %q (%d remaining: %v, %v elapsed)", n.Name(), remaining, names, time.Since(start).Truncate(time.Second))
		default:
		}
		time.Sleep(2 * time.Second)
	}
	return nil
}

// servicesForNode lists Services in the namespace that are associated with this node.
// It matches by:
// - name equals "service-<node>"
// - label "name" equals node name (per controller)
// - selector app == node name
// - ownerReference is Cdnos/<node>
func (n *Node) servicesForNode(ctx context.Context) ([]corev1.Service, error) {
	// Use a short-lived background context for API calls to avoid parent ctx deadline cancellations.
	_ = ctx // ctx is intentionally unused to avoid parent deadline cancellations
	listCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	list, err := n.KubeClient.CoreV1().Services(n.Namespace).List(listCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var out []corev1.Service
	wantName := fmt.Sprintf("service-%s", n.Name())
	for _, s := range list.Items {
		if s.Name == wantName {
			out = append(out, s)
			continue
		}
		if s.Labels["name"] == n.Name() {
			out = append(out, s)
			continue
		}
		if s.Spec.Selector != nil && s.Spec.Selector["app"] == n.Name() {
			out = append(out, s)
			continue
		}
		for _, or := range s.OwnerReferences {
			if or.Kind == "Cdnos" && or.Name == n.Name() {
				out = append(out, s)
				break
			}
		}
	}
	return out, nil
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
	defaultNodeClone := proto.Clone(&defaultNode).(*tpb.Node)
	if pb.Config == nil {
		pb.Config = &tpb.Config{}
	}
	if pb.Config.ConfigFile == "" {
		pb.Config.ConfigFile = defaultNodeClone.Config.ConfigFile
	}
	if pb.Config.ConfigPath == "" {
		pb.Config.ConfigPath = defaultNodeClone.Config.ConfigPath
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
		pb.Constraints["cpu"] = defaultConstraints.CPU
	}
	if pb.Constraints["memory"] == "" {
		pb.Constraints["memory"] = defaultConstraints.Memory
	}
	if pb.Labels == nil {
		pb.Labels = defaultNodeClone.Labels
	}
	if pb.Labels["vendor"] == "" {
		pb.Labels["vendor"] = defaultNodeClone.Labels["vendor"]
	}

	// Always explicitly specify that cdnos is a DUT, this cannot be overridden by the user.
	pb.Labels[node.OndatraRoleLabel] = defaultNodeClone.Labels[node.OndatraRoleLabel]

	if pb.Services == nil {
		pb.Services = defaultNodeClone.Services
	}
	return pb
}

func (n *Node) DefaultNodeConstraints() node.Constraints {
	return defaultConstraints
}

func init() {
	node.Vendor(tpb.Vendor_DRIVENETS, New)
}

// isAzureAKS attempts to detect whether the current cluster is Azure AKS.
// It returns true if any node has a providerID starting with "azure://"
// or has any label prefixed with "kubernetes.azure.com/".
func isAzureAKS(k kubernetes.Interface) bool {
	// Allow manual override for environments where listing nodes is restricted.
	if v := os.Getenv("KNE_FORCE_AKS"); v == "1" || strings.ToLower(v) == "true" {
		log.V(1).Infof("AKS detection overridden via KNE_FORCE_AKS")
		return true
	}
	if v := os.Getenv("KNE_FORCE_AZURE_ANNOTATIONS"); v == "1" || strings.ToLower(v) == "true" {
		log.V(1).Infof("AKS detection overridden via KNE_FORCE_AZURE_ANNOTATIONS")
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	nodes, err := k.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		log.V(1).Infof("AKS detection: failed to list nodes: %v", err)
		return false
	}
	if len(nodes.Items) == 0 {
		log.V(1).Infof("AKS detection: no nodes found in cluster")
		return false
	}
	for _, n := range nodes.Items {
		if strings.HasPrefix(n.Spec.ProviderID, "azure://") {
			log.V(1).Infof("AKS detection: node %q providerID %q indicates Azure", n.Name, n.Spec.ProviderID)
			return true
		}
		for key := range n.Labels {
			if strings.HasPrefix(key, "kubernetes.azure.com/") {
				log.V(1).Infof("AKS detection: node %q has Azure label %q", n.Name, key)
				return true
			}
		}
	}
	log.V(1).Infof("AKS detection: no Azure providerID or labels found on any node")
	return false
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
					Name: sCM.Name,
				},
			},
		},
	}
	return volume, nil
}
