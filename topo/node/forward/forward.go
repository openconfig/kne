// Copyright 2024 Google LLC
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
package forward

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	fpb "github.com/openconfig/kne/proto/forward"
	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	log "k8s.io/klog/v2"
	"k8s.io/utils/pointer"
)

const (
	// wirePort is the port the bridge daemon serves the Wire service on, and
	// therefore the port peers dial when a wire names this node via local_node.
	wirePort = 50058
)

var (
	// DefaultImage is the container image used for FORWARD nodes that do not
	// specify one. It must provide the `kne bridge` daemon as its entrypoint.
	DefaultImage = "us-west1-docker.pkg.dev/kne-external/kne/bridge:ga"

	defaultNode = tpb.Node{
		Name: "default_forward_node",
		Config: &tpb.Config{
			Image:        DefaultImage,
			ConfigPath:   "/etc",
			ConfigFile:   "config",
			EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", "default_forward_node"),
		},
	}
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
	return n, nil
}

type Node struct {
	*node.Impl
}

func (n *Node) Create(ctx context.Context) error {
	if err := n.ValidateConstraints(); err != nil {
		return fmt.Errorf("node %s failed to validate node with errors: %s", n.Name(), err)
	}
	if err := n.CreatePod(ctx); err != nil {
		return fmt.Errorf("node %s failed to create pod %w", n.Name(), err)
	}
	if err := n.CreateService(ctx); err != nil {
		return fmt.Errorf("node %s failed to create service %w", n.Name(), err)
	}
	return nil
}

// CreateService creates the services declared in the topology, plus a headless
// Service named after the node so that peers naming it with local_node can
// reach it as "<node>:<wirePort>".
func (n *Node) CreateService(ctx context.Context) error {
	if err := n.createPeerService(ctx); err != nil {
		return err
	}
	if err := n.Impl.CreateService(ctx); err != nil {
		_ = n.KubeClient.CoreV1().Services(n.Namespace).Delete(ctx, n.Name(), metav1.DeleteOptions{})
		return err
	}
	return nil
}

// createPeerService creates the headless Service giving this node a stable
// in-cluster DNS name, so that a wire referring to it does not have to know how
// KNE happens to name the node's other Services.
//
// It deliberately declares no ports. A headless Service needs only a selector
// to get A records for its pods, and leaving ports unset keeps this Service out
// of the port map that `kne show` reports. It carries the same "pod" label as
// the node's other Services so that DeleteService tears it down with them.
func (n *Node) createPeerService(ctx context.Context) error {
	s := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: n.Name(),
			Labels: map[string]string{
				"pod": n.Name(),
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: corev1.ClusterIPNone,
			Selector: map[string]string{
				"app": n.Name(),
			},
		},
	}
	sS, err := n.KubeClient.CoreV1().Services(n.Namespace).Create(ctx, s, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create peer service for node %s: %w", n.Name(), err)
	}
	log.Infof("Created peer Service:\n%v\n", sS)
	return nil
}

// bridgeProcess is a single bridge daemon invocation. The daemon runs in
// exactly one mode per process, so a node that both serves and dials needs more
// than one, and each becomes its own container in the node's pod.
type bridgeProcess struct {
	// nameSuffix disambiguates the container when a node needs several.
	nameSuffix string
	args       []string
}

// wirePlan translates the wires declared for a node into the bridge daemon
// invocations that implement them.
//
// Within a Wire, the Interface endpoint is always this node: if it is the "a"
// (client) endpoint then this node dials out, and if it is the "z" (server)
// endpoint then this node listens and the peer dials in. A wire with no "a" at
// all is a server wire whose client lives outside the topology entirely, which
// is how an external peer such as a Borg job attaches.
//
// One bridge server handles every interface asked of it over a single port, so
// all server wires collapse into one process, whereas each client wire dials a
// distinct peer and needs its own.
func wirePlan(cfg *fpb.ForwardConfig) ([]bridgeProcess, error) {
	var serves []string
	var procs []bridgeProcess
	seen := make(map[string]struct{})
	for _, w := range cfg.GetWires() {
		aIntf := w.GetA().GetInterface().GetName()
		zIntf := w.GetZ().GetInterface().GetName()
		switch {
		case aIntf != "" && zIntf != "":
			return nil, fmt.Errorf("endpoints a and z cannot both be interfaces")
		case aIntf != "":
			if _, dup := seen[aIntf]; dup {
				return nil, fmt.Errorf("duplicate wire for local interface %q", aIntf)
			}
			seen[aIntf] = struct{}{}
			addr, remote, err := peerEndpoint(w.GetZ())
			if err != nil {
				return nil, fmt.Errorf("wire for local interface %q: %w", aIntf, err)
			}
			if remote == "" {
				remote = aIntf
			}
			procs = append(procs, bridgeProcess{
				nameSuffix: "client-" + aIntf,
				args: []string{
					"client",
					fmt.Sprintf("--peer=%s", addr),
					fmt.Sprintf("--interface=%s", aIntf),
					fmt.Sprintf("--remote_interface=%s", remote),
				},
			})
		case zIntf != "":
			if _, dup := seen[zIntf]; dup {
				return nil, fmt.Errorf("duplicate wire for local interface %q", zIntf)
			}
			seen[zIntf] = struct{}{}
			serves = append(serves, zIntf)
		default:
			return nil, fmt.Errorf("one of endpoints a and z must be an interface")
		}
	}
	if len(serves) > 0 {
		// The server opens interfaces on demand, keyed by the name the client
		// requests, so serving N interfaces needs no per-interface flags.
		log.Infof("Serving wire endpoints for interfaces %v", serves)
		procs = append([]bridgeProcess{{nameSuffix: "server", args: []string{"server"}}}, procs...)
	}
	return procs, nil
}

// peerEndpoint resolves the far end of a client wire to a dialable address and
// to the interface name to request on that peer.
func peerEndpoint(e *fpb.Endpoint) (string, string, error) {
	switch {
	case e.GetLocalNode() != nil:
		ln := e.GetLocalNode()
		if ln.GetName() == "" {
			return "", "", fmt.Errorf("local_node endpoint must set name")
		}
		// Resolvable because every forward node gets a headless Service named
		// after it; see createPeerService.
		return net.JoinHostPort(ln.GetName(), strconv.Itoa(wirePort)), ln.GetInterface(), nil
	case e.GetRemoteNode() != nil:
		rn := e.GetRemoteNode()
		addr := rn.GetAddr()
		if addr == "" {
			return "", "", fmt.Errorf("remote_node endpoint must set addr")
		}
		if _, _, err := net.SplitHostPort(addr); err != nil {
			// No port given, so assume the peer serves on the default port.
			addr = net.JoinHostPort(strings.Trim(addr, "[]"), strconv.Itoa(wirePort))
		}
		return addr, rn.GetInterface(), nil
	default:
		return "", "", fmt.Errorf("endpoint must be a local_node or a remote_node")
	}
}

// CreatePod creates a Pod for the Node based on the underlying proto.
func (n *Node) CreatePod(ctx context.Context) error {
	pb := n.Proto
	log.Infof("Creating Pod:\n %+v", pb)
	initContainerImage := pb.Config.InitImage
	if initContainerImage == "" {
		initContainerImage = node.DefaultInitContainerImage
	}

	// A node with no declared wires falls back to whatever args the topology
	// supplies, which keeps hand-rolled configurations working.
	procs := []bridgeProcess{{args: pb.Config.Args}}
	if vendorData := pb.Config.GetVendorData(); vendorData != nil {
		fwdCfg := &fpb.ForwardConfig{}
		if err := vendorData.UnmarshalTo(fwdCfg); err != nil {
			return err
		}
		log.Infof("Got fwdCfg: %v", prototext.Format(fwdCfg))
		planned, err := wirePlan(fwdCfg)
		if err != nil {
			return fmt.Errorf("node %s: %w", pb.Name, err)
		}
		if len(planned) > 0 {
			for i := range planned {
				planned[i].args = append(planned[i].args, pb.Config.Args...)
			}
			procs = planned
		}
	}

	// Each bridge process gets its own container. Constraints are applied to
	// every one of them rather than divided between them, because each process
	// carries its own traffic and so needs the stated budget in full.
	containers := make([]corev1.Container, 0, len(procs))
	for i, p := range procs {
		// The first process keeps the plain node name so `kubectl exec <node>`
		// and Impl.Exec work without a container selector; subsequent containers
		// append their disambiguating suffix.
		name := pb.Name
		if i > 0 {
			name = fmt.Sprintf("%s-%s", pb.Name, p.nameSuffix)
		}
		log.Infof("Container %q args: %v", name, p.args)
		containers = append(containers, corev1.Container{
			Name:            name,
			Image:           pb.Config.Image,
			Command:         pb.Config.Command,
			Args:            p.args,
			Env:             node.ToEnvVar(pb.Config.Env),
			Resources:       node.ToResourceRequirements(pb.Constraints),
			ImagePullPolicy: "IfNotPresent",
			SecurityContext: &corev1.SecurityContext{
				Privileged: pointer.Bool(true),
			},
		})
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: pb.Name,
			Labels: map[string]string{
				"app":  pb.Name,
				"topo": n.Namespace,
			},
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{
				Name:  fmt.Sprintf("init-%s", pb.Name),
				Image: initContainerImage,
				Args: []string{
					fmt.Sprintf("%d", len(n.Proto.Interfaces)+1),
					fmt.Sprintf("%d", pb.Config.Sleep),
				},
				ImagePullPolicy: "IfNotPresent",
			}},
			Containers:                    containers,
			TerminationGracePeriodSeconds: pointer.Int64(0),
			NodeSelector:                  map[string]string{},
			Affinity: &corev1.Affinity{
				PodAntiAffinity: &corev1.PodAntiAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{{
						Weight: 100,
						PodAffinityTerm: corev1.PodAffinityTerm{
							LabelSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{{
									Key:      "topo",
									Operator: "In",
									Values:   []string{n.Namespace},
								}},
							},
							TopologyKey: "kubernetes.io/hostname",
						},
					}},
				},
			},
		},
	}
	for label, v := range n.GetProto().GetLabels() {
		pod.ObjectMeta.Labels[label] = v
	}
	if pb.Config.ConfigData != nil {
		vol, err := n.CreateConfig(ctx)
		if err != nil {
			return err
		}
		pod.Spec.Volumes = append(pod.Spec.Volumes, *vol)
		vm := corev1.VolumeMount{
			Name:      node.ConfigVolumeName,
			MountPath: pb.Config.ConfigPath + "/" + pb.Config.ConfigFile,
			ReadOnly:  true,
		}
		if vol.VolumeSource.ConfigMap != nil {
			vm.SubPath = pb.Config.ConfigFile
		}
		for i, c := range pod.Spec.Containers {
			pod.Spec.Containers[i].VolumeMounts = append(c.VolumeMounts, vm)
		}
	}
	sPod, err := n.KubeClient.CoreV1().Pods(n.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	log.Infof("Pod created:\n%+v\n", sPod)
	return nil
}

func defaults(pb *tpb.Node) *tpb.Node {
	defaultNodeClone := proto.Clone(&defaultNode).(*tpb.Node)
	if pb.Config == nil {
		pb.Config = &tpb.Config{}
	}
	if pb.Config.EntryCommand == "" {
		pb.Config.EntryCommand = fmt.Sprintf("kubectl exec -it %s -- sh", pb.Name)
	}
	if pb.Config.Image == "" {
		pb.Config.Image = defaultNodeClone.Config.Image
	}
	if pb.Config.ConfigPath == "" {
		pb.Config.ConfigPath = defaultNodeClone.Config.ConfigPath
	}
	if pb.Config.ConfigFile == "" {
		pb.Config.ConfigFile = defaultNodeClone.Config.ConfigFile
	}
	return pb
}

func init() {
	node.Vendor(tpb.Vendor_FORWARD, New)
}
