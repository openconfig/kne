// Copyright 2025 Ciena Corporation
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
package ciena

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	cpb "github.com/openconfig/kne/proto/ciena"
	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	log "k8s.io/klog/v2"
	"k8s.io/utils/pointer"
)

const (
	ModelWR = "waverouter"
	ModelSAOS = "5132" // Default SAOS model type
)

var (
	defaultNode = tpb.Node{
		Name: "default_waverouter_node",
		Services: map[uint32]*tpb.Service{
			22: {
				Names:  []string{"ssh"},
				Inside: 22,
			},
			179: {
				Names:  []string{"bgp"},
				Inside: 179,
			},
			225: {
				Names:  []string{"debug"},
				Inside: 225,
			},
			443: {
				Names:  []string{"ssl"},
				Inside: 443,
			},
			830: {
				Names:  []string{"netconf"},
				Inside: 830,
			},
			4243: {
				Names:  []string{"docker-daemon"},
				Inside: 4243,
			},
			9339: {
				Names:  []string{"gnmi"},
				Inside: 9339,
			},
			9340: {
				Names:  []string{"gribi"},
				Inside: 9340,
			},
			9559: {
				Names:  []string{"p4rt"},
				Inside: 9559,
			},
			10161: {
				Names:  []string{"gnmi-gnoi-alternate"},
				Inside: 10161,
			},
		},
		Model: ModelSAOS,
		Os:    "saos",
		Labels: map[string]string{
			"vendor":              tpb.Vendor_CIENA.String(),
			node.OndatraRoleLabel: node.OndatraRoleDUT,
		},
		Config: &tpb.Config{
			ConfigPath:   "config/",
			ConfigFile:   "config.cfg",
			Image:     "artifactory.ciena.com/psa/saos-containerlab:latest",
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
	cfg, err := defaults(nodeImpl.Proto)
	if err != nil {
		return nil, err
	}
	nodeImpl.Proto = cfg
	n := &Node{
		Impl: nodeImpl,
	}
	n.Proto.Interfaces = renameInterfaces(nodeImpl.Proto.Interfaces)
	return n, nil
}

// renameInterfaces renames the interfaces port naming to ethX format
// e.g., eth1 -> eth1, 1/5/1 -> eth1, 2 -> eth2, etc
func renameInterfaces(interfaces map[string]*tpb.Interface) map[string]*tpb.Interface {
    intf := map[string]*tpb.Interface{}
    reFull := regexp.MustCompile(`^\d+/\d+/\d+$`)
    reSingle := regexp.MustCompile(`^\d+$`)
    for k, v := range interfaces {
        switch {
        case reFull.MatchString(k):
            parts := regexp.MustCompile(`/`).Split(k, -1)
            lastNum := parts[len(parts)-1]
            name := fmt.Sprintf("eth%s", lastNum)
            intf[name] = v
        case reSingle.MatchString(k):
            name := fmt.Sprintf("eth%s", k)
            intf[name] = v
        default:
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
		return fmt.Errorf("node %s failed to validate node with errors: %w", n.Name(), err)
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

	var extraVolumes []corev1.Volume
	var extraMounts []corev1.VolumeMount
	mountPath := "/"
	fileName := "setup.json"

	if vendorData := pb.Config.GetVendorData(); vendorData != nil {
		cienaConfig := &cpb.CienaConfig{}

		if err := vendorData.UnmarshalTo(cienaConfig); err != nil {
			return err
		}

		if cienaConfig.GetSystemEquipment().GetMountDir() != "" {
			mountPath = cienaConfig.GetSystemEquipment().GetMountDir()
		}
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("%s-file", n.Proto.Name),
			},
			Data: map[string]string{},
		}
		equipmentFile := cienaConfig.GetSystemEquipment().GetEquipmentJson()

		contents, err := os.ReadFile(filepath.Join(n.BasePath, equipmentFile))
		if err != nil {
			return err
		}
		cm.Data[fileName] = string(contents)
		extraMounts = append(extraMounts, corev1.VolumeMount{
			Name:      "equipment-file",
			MountPath: fmt.Sprintf("%s/%s", mountPath, fileName),
			SubPath:   fileName,
			ReadOnly:  true,
		})
		extraVolumes = append(extraVolumes, corev1.Volume{
			Name: "equipment-file",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: fmt.Sprintf("%s-file", n.Proto.Name),
					},
				}},
		})

		_, err = n.KubeClient.CoreV1().ConfigMaps(n.Namespace).Create(ctx, cm, metav1.CreateOptions{})
		if err != nil {
			return err
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
			Containers: []corev1.Container{{
				Name:            pb.Name,
				Image:           pb.Config.Image,
				Command:         pb.Config.Command,
				Args:            pb.Config.Args,
				Env:             node.ToEnvVar(pb.Config.Env),
				Resources:       node.ToResourceRequirements(pb.Constraints),
				ImagePullPolicy: corev1.PullIfNotPresent,
				SecurityContext: &corev1.SecurityContext{
					Privileged: pointer.Bool(true),
				},
				VolumeMounts: extraMounts,
			}},
			Volumes:                       extraVolumes,
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
			MountPath: fmt.Sprintf("%s/%s", pb.Config.ConfigPath, pb.Config.ConfigFile),
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
		return fmt.Errorf("failed to create pod for %q: %w", pb.Name, err)
	}
	log.V(1).Infof("Pod created:\n%+v\n", sPod)
	return nil
}

func defaults(pb *tpb.Node) (*tpb.Node, error) {
	defaultNodeClone := proto.Clone(&defaultNode).(*tpb.Node)
	if pb == nil {
		pb = &tpb.Node{
			Name: defaultNodeClone.Name,
		}
	}
	if pb.Config == nil {
		pb.Config = &tpb.Config{}
	}
	if pb.Config.ConfigFile == "" {
		pb.Config.ConfigFile = defaultNodeClone.Config.ConfigFile
	}
	if pb.Config.ConfigPath == "" {
		pb.Config.ConfigPath = defaultNodeClone.Config.ConfigPath
	}
	if pb.Model == "" {
		pb.Model = defaultNodeClone.Model
	}
	if pb.Os == "" {
		pb.Os = defaultNodeClone.Os
	}
	if pb.Services == nil {
		pb.Services = defaultNodeClone.Services
	}
	if pb.Labels == nil {
		pb.Labels = map[string]string{}
	}
	if pb.Labels["vendor"] == "" {
		pb.Labels["vendor"] = defaultNodeClone.Labels["vendor"]
	}
	if pb.Labels["model"] == "" {
		pb.Labels["model"] = pb.Model
	}
	if pb.Labels["os"] == "" {
		pb.Labels["os"] = pb.Os
	}
	if pb.Labels[node.OndatraRoleLabel] == "" {
		pb.Labels[node.OndatraRoleLabel] = defaultNodeClone.Labels[node.OndatraRoleLabel]
	}
	if pb.Config.Env == nil {
		pb.Config.Env = map[string]string{}
	}
	pb.Config.Env["CLAB_LABEL_CLAB_NODE_NAME"] = pb.Name
	pb.Config.Env["CLAB_LABEL_CLAB_NODE_TYPE"] = pb.Model

	if pb.Model == ModelWR && pb.Config.Image == "" {
		pb.Config.Image = "artifactory.ciena.com/psa/rw-containerlab:latest"
	} else if pb.Config.Image == "" {
		pb.Config.Image = defaultNodeClone.Config.Image
	}
	return pb, nil
}

func init() {
	node.Vendor(tpb.Vendor_CIENA, New)
}
