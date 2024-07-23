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

	fpb "github.com/openconfig/kne/proto/forward"
	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	"google.golang.org/protobuf/encoding/prototext"
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

// CreatePod creates a Pod for the Node based on the underlying proto.
func (n *Node) CreatePod(ctx context.Context) error {
	pb := n.Proto
	log.Infof("Creating Pod:\n %+v", pb)
	initContainerImage := pb.Config.InitImage
	if initContainerImage == "" {
		initContainerImage = node.DefaultInitContainerImage
	}

	if vendorData := pb.Config.GetVendorData(); vendorData != nil {
		fwdCfg := &fpb.ForwardConfig{}
		if err := vendorData.UnmarshalTo(fwdCfg); err != nil {
			return err
		}
		// TODO: Validate and use fwdCfg to pass arguments to the container.
		log.Infof("Got fwdCfg: %v", prototext.Format(fwdCfg))
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
			Containers: []corev1.Container{{
				Name:            pb.Name,
				Image:           pb.Config.Image,
				Command:         pb.Config.Command,
				Args:            pb.Config.Args,
				Env:             node.ToEnvVar(pb.Config.Env),
				Resources:       node.ToResourceRequirements(pb.Constraints),
				ImagePullPolicy: "IfNotPresent",
				SecurityContext: &corev1.SecurityContext{
					Privileged: pointer.Bool(true),
				},
			}},
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
	if pb.Config == nil {
		pb.Config = &tpb.Config{}
	}
	if len(pb.GetConfig().GetCommand()) == 0 {
		pb.Config.Command = []string{"/bin/sh", "-c", "sleep 2000000000000"}
	}
	if pb.Config.EntryCommand == "" {
		pb.Config.EntryCommand = fmt.Sprintf("kubectl exec -it %s -- sh", pb.Name)
	}
	if pb.Config.Image == "" {
		pb.Config.Image = "forward:latest"
	}
	if pb.Config.ConfigPath == "" {
		pb.Config.ConfigPath = "/etc"
	}
	if pb.Config.ConfigFile == "" {
		pb.Config.ConfigFile = "config"
	}
	return pb
}

func init() {
	node.Vendor(tpb.Vendor_FORWARD, New)
}
