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
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"github.com/google/kne/topo/node"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	tpb "github.com/google/kne/proto/topo"
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

func getCiscoInterfaceId(model string, ethID int) (string, error) {
	ciscoInterfacePrefix := ""
	switch model {
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
	default:
		ciscoInterfacePrefix = "GigabitEthernet0/0/0/"
	}
	if ciscoInterfacePrefix != "" {
		return fmt.Sprintf("%s%d", ciscoInterfacePrefix, ethID-1), nil
	} else {
		return "", errors.New("invalid interface id")
	}
}

func generateInterfacesEnvs(pb *tpb.Node) (interfaceList, interfaceMap string) {
	interfaceList = "MgmtEther0/RP0/CPU0/0"
	interfaceMap = "MgmtEther0/RP0/CPU0/0:eth0"
	eths := make([]string, 0)
	for k := range pb.Interfaces {
		eths = append(eths, k)
	}
	sort.Strings(eths)
	ethWithIdRegx := regexp.MustCompile(`e(t(h(e(r(n(e(t)*)*)*)*)*)*)\d+`) // check for e|et|eth|....
	ethRegx := regexp.MustCompile(`e(t(h(e(r(n(e(t)*)*)*)*)*)*)`)          // check for e|et|eth|....
	for _, eth := range eths {
		if !ethWithIdRegx.MatchString(eth) {
			log.Warn(fmt.Sprintf("Interface id %s is invalid", eth))
			continue
		}
		if pb.Interfaces[eth].Name == "" {
			// ethWithIdRegx.MatchString(eth) was suceffull, so no need to do extra check here
			ethId, _ := strconv.Atoi(ethRegx.Split(eth, -1)[1])
			ciscoInterfaceId, err := getCiscoInterfaceId(pb.Model, ethId)
			if err == nil {
				interfaceList = fmt.Sprintf("%s,%s", interfaceList, ciscoInterfaceId)
				interfaceMap = fmt.Sprintf("%s,%s:%s", interfaceMap, ciscoInterfaceId, eth)
			} else {
				log.Warn(fmt.Errorf("can not get cisco interface id for %s : %w ", eth, err))
				continue
			}
		} else {
			interfaceList = fmt.Sprintf("%s,%s", interfaceList, pb.Interfaces[eth].Name)
			interfaceMap = fmt.Sprintf("%s,%s:%s", interfaceMap, pb.Interfaces[eth].Name, eth)
		}
	}
	return interfaceList, interfaceMap
}

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
	constraints := constraints(pb)
	if pb.Constraints == nil {
		pb.Constraints = constraints
	} else {
		if pb.Constraints["cpu"] == "" {
			pb.Constraints["cpu"] = constraints["cpu"]
		}
		if pb.Constraints["memory"] == "" {
			pb.Constraints["memory"] = constraints["memory"]
		}
	}
	if pb.Services == nil {
		pb.Services = map[uint32]*tpb.Service{
			443: {
				Name:   "ssl",
				Inside: 443,
			},
			22: {
				Name:   "ssh",
				Inside: 22,
			},
			6030: {
				Name:   "gnmi",
				Inside: 57400,
			},
		}
	}
	if pb.Labels == nil {
		pb.Labels = map[string]string{
			"vendor": tpb.Vendor_CISCO.String(),
		}
	}
	if pb.Config.Image == "" {
		pb.Config.Image = "ios-xr:latest"
	}
	if pb.Config.EntryCommand == "" {
		pb.Config.EntryCommand = fmt.Sprintf("kubectl exec -it %s -- bash", pb.Name)
	}
	interfaceList, interfaceMap := generateInterfacesEnvs(pb)
	if pb.Config.Env == nil {
		pb.Config.Env = map[string]string{
			"XR_INTERFACES":                  interfaceMap,
			"XR_CHECKSUM_OFFLOAD_COUNTERACT": interfaceList,
			"XR_EVERY_BOOT_CONFIG":           filepath.Join(pb.Config.ConfigPath, pb.Config.ConfigFile),
		}
	} else {
		if pb.Config.Env["XR_INTERFACES"] == "" {
			pb.Config.Env["XR_INTERFACES"] = interfaceMap
		}
		if pb.Config.Env["XR_CHECKSUM_OFFLOAD_COUNTERACT"] == "" {
			pb.Config.Env["XR_CHECKSUM_OFFLOAD_COUNTERACT"] = interfaceList
		}
		if pb.Config.Env["XR_EVERY_BOOT_CONFIG"] == "" {
			pb.Config.Env["XR_EVERY_BOOT_CONFIG"] = filepath.Join(pb.Config.ConfigPath, pb.Config.ConfigFile)
		}
	}
	if pb.Model == "xrd" {
		// XR_SNOOP_IP_INTERFACES should always set to MgmtEther0/RP0/CPU0/0
		// This enables autmatic bringup of the managment interface for xrd
		pb.Config.Env["XR_SNOOP_IP_INTERFACES"] = "MgmtEther0/RP0/CPU0/0"
	}
	return pb
}

func init() {
	node.Vendor(tpb.Vendor_CISCO, New)
}
