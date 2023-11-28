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
package host

import (
	"fmt"

	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
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

// CreatePod creates a Pod for the Node based on the underlying proto.
func (n *Impl) CreatePod(ctx context.Context) error {
	pb := n.Proto
	log.Infof("Creating Pod:\n %+v", pb)
	initContainerImage := pb.Config.InitImage
	if initContainerImage == "" {
		initContainerImage = DefaultInitContainerImage
	}
	if vendorData := config.GetVendorData(); vendorData != nil {
		alpineConfig := &apb.AlpineConfig{}
		if err := vendorData.UnmarshalTo(alpineConfig); err != nil {
			return err
		}
		if sonicContainer := alpineConfig.GetSonicContainer(); sonicContainer != nil {
			sonicName := sonicContainer.Name
			sonicImage := sonicContainer.Image
			sonicCommand := sonicContainer.Command
			sonicArgs := sonicContainer.Args
		}
		if dataplaneContainer := alpineConfig.GetDataplaneContainer(); dataplaneContainer != nil {
			dataplaneName := dataplaneContainer.Name
			dataplaneImage := dataplaneContainer.Image
			dataplaneCommand := dataplaneContainer.Command
			dataplaneArgs := dataplaneContainer.Args
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
			Containers: []corev1.Container{
			{
				Name:            sonicName,
				Image:           sonicImage,
				Command:         sonicCommand,
				Args:            sonicArgs,
				Env:             ToEnvVar(pb.Config.Env),
				Resources:       ToResourceRequirements(pb.Constraints),
				ImagePullPolicy: "IfNotPresent",
				SecurityContext: &corev1.SecurityContext{
					Privileged: pointer.Bool(true),
				},
			},
			{
				Name:            dataplaneName,
				Image:           dataplaneImage,
				Command:         dataplaneCommand,
				Args:            dataplaneArgs,
				Env:             ToEnvVar(pb.Config.Env),
				Resources:       ToResourceRequirements(pb.Constraints),
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
	sPod, err := n.KubeClient.CoreV1().Pods(n.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	log.V(1).Infof("Pod created:\n%+v\n", sPod)
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
	if pb.Config.SonicImage == "" {
		pb.Config.SonicImage = "alpinevs:latest"
	}
	if pb.Config.DataplaneImage == "" {
		pb.Config.DataplaneImage = "lucius:latest"
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
	node.Vendor(tpb.Vendor_ALPINE, New)
}