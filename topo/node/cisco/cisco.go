// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package cisco

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/google/kne/topo/node"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	tpb "github.com/google/kne/proto/topo"
)

func New(nodeImpl *node.Impl) (node.Node, error) {
	if nodeImpl == nil {
		return nil, fmt.Errorf("nodeImpl cannot be nil")
	}
	cfg := defaults(nodeImpl.Proto)
	proto.Merge(cfg, nodeImpl.Proto)
	node.FixServices(cfg)
	n := &Node{
		Impl: nodeImpl,
	}
	proto.Merge(n.Impl.Proto, cfg)
	return n, nil
}

type Node struct {
	*node.Impl
}

func (n *Node) Create(ctx context.Context) error {
	log.Infof("Creating Cisco %s node resource %s", n.Proto.Model, n.Name())

	if err := n.CreateConfig(ctx); err != nil {
		return fmt.Errorf("node %s failed to create config-map %w", n.Name(), err)
	}
	log.Infof("Created Cisco %s node %s configmap", n.Proto.Model, n.Name())
	pb := n.Proto
	secContext := &corev1.SecurityContext{
		Privileged: pointer.Bool(true),
	}
	if pb.Model == "xrd" {
		secContext = &corev1.SecurityContext{
			Privileged: pointer.Bool(true),
			RunAsUser:  pointer.Int64(0),
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{"SYS_ADMIN"},
			},
		}
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: n.Name(),
			Labels: map[string]string{
				"app":  n.Name(),
				"topo": n.Namespace,
			},
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{
				Name:  fmt.Sprintf("init-%s", n.Name()),
				Image: pb.Config.InitImage,
				Args: []string{
					fmt.Sprintf("%d", len(pb.GetInterfaces())+1),
					fmt.Sprintf("%d", pb.GetConfig().Sleep),
				},
				ImagePullPolicy: "IfNotPresent",
			}},
			Containers: []corev1.Container{{
				Name:            n.Name(),
				Image:           pb.Config.Image,
				Command:         pb.Config.Command,
				Args:            pb.Config.Args,
				Env:             node.ToEnvVar(pb.Config.Env),
				Resources:       node.ToResourceRequirements(pb.Constraints),
				ImagePullPolicy: "IfNotPresent",
				SecurityContext: secContext,
				VolumeMounts: []corev1.VolumeMount{{
					Name:      fmt.Sprintf("%s-run-mount", pb.Name),
					ReadOnly:  false,
					MountPath: "/run",
				}},
			}},
			Volumes: []corev1.Volume{{
				Name: fmt.Sprintf("%s-run-mount", pb.Name),
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{
						Medium: "Memory",
					},
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
	if pb.Config.ConfigData != nil {
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name: "startup-config-volume",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: fmt.Sprintf("%s-config", pb.Name),
					},
				},
			},
		})
		for i, c := range pod.Spec.Containers {
			pod.Spec.Containers[i].VolumeMounts = append(c.VolumeMounts, corev1.VolumeMount{
				Name:      "startup-config-volume",
				MountPath: pb.Config.ConfigPath + "/" + pb.Config.ConfigFile,
				SubPath:   pb.Config.ConfigFile,
				ReadOnly:  true,
			})
		}
	}
	sPod, err := n.KubeClient.CoreV1().Pods(n.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create pod for %q: %w", pb.Name, err)
	}
	log.Debugf("Pod created:\n%+v\n", sPod)
	log.Infof("Created Cisco %s node resource %s pod", n.Proto.Model, n.Name())
	if err := n.CreateService(ctx); err != nil {
		return err
	}
	log.Infof("Created Cisco %s node resource %s services", n.Proto.Model, n.Name())
	return nil
}

func constraints(pb *tpb.Node) map[string]string {
	constraints := map[string]string{
		"cpu":    "1",
		"memory": "2Gi",
	}
	switch pb.Model {
	case "8201", "8201-32FH", "8202", "8101-32H", "8102-64H":
		constraints = map[string]string{
			"cpu":    "4",
			"memory": "12Gi",
		}
	case "xrd":
		constraints = map[string]string{
			"cpu":    "1",
			"memory": "2Gi",
		}
	}
	return constraints
}

func generateInterfacesEnvs(pb *tpb.Node) (interfaceList, interfaceMap string) {
	interfaceList = "MgmtEther0/RP0/CPU0/0"
	interfaceMap = "MgmtEther0/RP0/CPU0/0:eth0"
	eths := make([]string, 0)
	for k, _ := range pb.Interfaces {
		eths = append(eths, k)
	}
	sort.Strings(eths)
	for _, eth := range eths {
		if !strings.HasPrefix(eth, "eth") {
			continue
		}
		if pb.Interfaces[eth].Name == "" {
			ethID, err := strconv.Atoi(strings.TrimPrefix(eth, "eth"))
			if err != nil {
				// This should never happen since  eth ids are validated before calling this function.
				continue
			}
			ciscoInterfacePrefix := ""
			switch pb.Model {
			case "xrd":
				ciscoInterfacePrefix = "GigabitEthernet0/0/0/"
			case "8201":
				if ethID-1 <= 23 {
					ciscoInterfacePrefix = "FourHundredGigE0/0/0/"

				} else if ethID-1 <= 35 {
					ciscoInterfacePrefix = "HundredGigE0/0/0/"
				}
			case "8202":
				if ethID-1 <= 47 {
					ciscoInterfacePrefix = "HundredGigE0/0/0/"

				} else if ethID-1 <= 59 {
					ciscoInterfacePrefix = "FourHundredGigE0/0/0/"
				} else if ethID-1 <= 71 {
					ciscoInterfacePrefix = "HundredGigE0/0/0/"
				}
			case "8201-32FH":
				if ethID-1 <= 31 {
					ciscoInterfacePrefix = "FourHundredGigE0/0/0/"
				}
			case "8101-32H":
				if ethID-1 <= 31 {
					ciscoInterfacePrefix = "HundredGigE0/0/0/"
				}
			case "8102-64H":
				if ethID-1 <= 63 {
					ciscoInterfacePrefix = "HundredGigE0/0/0/"
				}
			}
			if ciscoInterfacePrefix != "" {
				interfaceList = fmt.Sprintf("%s,%s%d", interfaceList, ciscoInterfacePrefix, ethID-1)
				interfaceMap = fmt.Sprintf("%s,%s%d:%s", interfaceMap, ciscoInterfacePrefix, ethID-1, eth)
			}
		} else {
			interfaceList = fmt.Sprintf("%s,%s", interfaceList, pb.Interfaces[eth].Name)
			interfaceMap = fmt.Sprintf("%s,%s:%s", interfaceMap, pb.Interfaces[eth].Name, eth)
		}
	}
	return interfaceList, interfaceMap
}

/*for k, v := range pb.Interfaces {
	if !strings.HasPrefix(k, "eth") {
		continue
	}
	if v.Name == "" {
		ethID, err := strconv.Atoi(strings.TrimPrefix(k, "eth"))
		if err != nil {
			// This should never happen since  eth ids are validated before calling this function.
			continue
		}
		ciscoInterfacePrefix := ""
		switch pb.Model {
		case "xrd":
			ciscoInterfacePrefix = "GigabitEthernet0/0/0/"
		case "8201":
			if ethID-1 <= 23 {
				ciscoInterfacePrefix = "FourHundredGigE0/0/0/"

			} else if ethID-1 <= 35 {
				ciscoInterfacePrefix = "HundredGigE0/0/0/"
			}
		case "8202":
			if ethID-1 <= 47 {
				ciscoInterfacePrefix = "HundredGigE0/0/0/"

			} else if ethID-1 <= 59 {
				ciscoInterfacePrefix = "FourHundredGigE0/0/0/"
			} else if ethID-1 <= 71 {
				ciscoInterfacePrefix = "HundredGigE0/0/0/"
			}
		case "8201-32FH":
			if ethID-1 <= 31 {
				ciscoInterfacePrefix = "FourHundredGigE0/0/0/"
			}
		case "8101-32H":
			if ethID-1 <= 31 {
				ciscoInterfacePrefix = "HundredGigE0/0/0/"
			}
		case "8102-64H":
			if ethID-1 <= 63 {
				ciscoInterfacePrefix = "HundredGigE0/0/0/"
			}
		}
		if ciscoInterfacePrefix != "" {
			interfaceList = fmt.Sprintf("%s,%s%d", interfaceList, ciscoInterfacePrefix, ethID-1)
			interfaceMap = fmt.Sprintf("%s,%s%d:%s", interfaceMap, ciscoInterfacePrefix, ethID-1, k)
		}
	} else {
		interfaceList = fmt.Sprintf("%s,%s", interfaceList, v.Name)
		interfaceMap = fmt.Sprintf("%s,%s:%s", interfaceMap, v.Name, k)
	}
}*/
//}
func defaults(pb *tpb.Node) *tpb.Node {
	if pb == nil {
		pb = &tpb.Node{
			Name: "default_cisco_node",
		}
	}
	if pb.Config == nil {
		pb.Config = &tpb.Config{}
	}
	if pb.Config.ConfigFile == "" {
		pb.Config.ConfigFile = "startup.cfg"
	}
	if pb.Config.ConfigPath == "" {
		pb.Config.ConfigPath = "/"
	}
	if pb.Model == "" {
		pb.Model = "xrd"
	}
	interfaceList, interfaceMap := generateInterfacesEnvs(pb)
	return &tpb.Node{
		Name:        pb.Name,
		Model:       pb.Model,
		Constraints: constraints(pb),
		Services: map[uint32]*tpb.Service{
			443: {
				Name:     "ssl",
				Inside:   443,
				NodePort: node.GetNextPort(),
			},
			22: {
				Name:     "ssh",
				Inside:   22,
				NodePort: node.GetNextPort(),
			},
			57400: {
				Name:     "gnmi",
				Inside:   57400,
				NodePort: node.GetNextPort(),
			},
		},
		Labels: map[string]string{
			"vendor": tpb.Vendor_CISCO.String(),
		},
		Config: &tpb.Config{
			Image: "ios-xr:latest",
			Env: map[string]string{
				"XR_INTERFACES":                  interfaceMap,
				"XR_CHECKSUM_OFFLOAD_COUNTERACT": interfaceList,
				"XR_EVERY_BOOT_CONFIG":           filepath.Join(pb.Config.ConfigPath, pb.Config.ConfigFile),
			},
			EntryCommand: fmt.Sprintf("kubectl exec -it %s -- bash", pb.Name),
			ConfigPath:   pb.Config.ConfigPath,
			ConfigFile:   pb.Config.ConfigFile,
		},
	}
}

func init() {
	node.Vendor(tpb.Vendor_CISCO, New)
}
