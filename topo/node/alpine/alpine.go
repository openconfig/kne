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
package alpine

import (
	"context"
	"fmt"
	"strings"

	apb "github.com/openconfig/kne/proto/alpine"
	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	log "k8s.io/klog/v2"
	"k8s.io/utils/pointer"
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
	n.Proto.Interfaces = renameInterfaces(nodeImpl.Proto.Interfaces)
	return n, nil
}

func renameInterfaces(in map[string]*tpb.Interface) map[string]*tpb.Interface {
	idx := 1
	intf := map[string]*tpb.Interface{}
	for k, v := range in {
		if strings.HasPrefix(k, "Ethernet") {
			name := fmt.Sprintf("eth%d", idx)
			idx++
			intf[name] = v
		} else {
			intf[k] = v
		}
	}
	return intf
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

// CreatePod creates a Pod for the Node based on the underlying proto.
func (n *Node) CreatePod(ctx context.Context) error {
	pb := n.Proto
	log.Infof("Creating Pod:\n %+v", pb)

	initContainerImage := pb.Config.InitImage
	if initContainerImage == "" {
		initContainerImage = node.DefaultInitContainerImage
	}

	alpineContainers := []corev1.Container{{
		Name:    pb.Name,
		Image:   pb.Config.Image,
		Command: pb.Config.Command,
		Args:    pb.Config.Args,
		Env:     node.ToEnvVar(pb.Config.Env),
		// TODO: Update resources to the containers as per the constraints
		Resources:       node.ToResourceRequirements(pb.Constraints),
		ImagePullPolicy: "IfNotPresent",
		SecurityContext: &corev1.SecurityContext{
			Privileged: pointer.Bool(true),
		},
	}}

	var intfMap []string
	for k, v := range pb.Interfaces {
		intfMap = append(intfMap, fmt.Sprintf("%s:%s", v.IntName, k))
	}

	if vendorData := pb.Config.GetVendorData(); vendorData != nil {
		alpineConfig := &apb.AlpineConfig{}

		if err := vendorData.UnmarshalTo(alpineConfig); err != nil {
			return err
		}

		switch numContainers := len(alpineConfig.Containers); numContainers {
		case 0:
			log.Infof("Alpine custom containers not found.")
		case 1:
			dpContainer := alpineConfig.Containers[0]
			if len(intfMap) != 0 {
				dpContainer.Args = append(dpContainer.Args, "--portMap="+strings.Join(intfMap, ","))
			}
			alpineContainers = append(alpineContainers,
				corev1.Container{
					Name:    dpContainer.Name,
					Image:   dpContainer.Image,
					Command: dpContainer.Command,
					Args:    dpContainer.Args,
					Env:     node.ToEnvVar(pb.Config.Env),
					// TODO: Update resources to the containers as per the constraints
					Resources:       node.ToResourceRequirements(pb.Constraints),
					ImagePullPolicy: "IfNotPresent",
					SecurityContext: &corev1.SecurityContext{
						Privileged: pointer.Bool(true),
					},
				},
			)
		default:
			// Only Dataplane container is supported as the custom container
			return fmt.Errorf("Alpine supports only 1 custom container, %d provided.", numContainers)
		}
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
			Containers:                    alpineContainers,
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
									Values:   []string{pb.Name},
								}},
							},
							TopologyKey: "kubernetes.io/hostname",
						},
					}},
				},
			},
		},
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
			pod.Spec.Containers[i].Args = append(pod.Spec.Containers[i].Args, fmt.Sprintf("--config_file=%s/%s", pb.Config.ConfigPath, pb.Config.ConfigFile))
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
	if pb.Config == nil {
		pb.Config = &tpb.Config{}
	}
	if len(pb.GetConfig().GetCommand()) == 0 {
		pb.Config.Command = []string{"go", "run", "main.go"}
	}
	if pb.Config.EntryCommand == "" {
		pb.Config.EntryCommand = fmt.Sprintf("kubectl exec -it %s -- sh", pb.Name)
	}
	if pb.Config.Image == "" {
		pb.Config.Image = "alpine:latest"
	}
	if pb.Services == nil {
		pb.Services = map[uint32]*tpb.Service{
			22: {
				Name:   "ssh",
				Inside: 22,
			},
		}
	}
	// TODO: Add appropriate default constraints for the Alpine KNE node

	return pb
}

func init() {
	node.Vendor(tpb.Vendor_ALPINE, New)
}
