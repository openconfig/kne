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
	cfg, err := defaults(nodeImpl.Proto)
	if err != nil {
		return nil, err
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

func constraints(pb *tpb.Node) *tpb.Node {
	if pb.Constraints == nil {
		pb.Constraints = map[string]string{}
	}
	switch pb.Model {
	case "8201", "8201-32FH", "8202", "8101-32H", "8102-64H":
		if pb.Constraints["cpu"] == "" {
			pb.Constraints["cpu"] = "4"
		}
		if pb.Constraints["memory"] == "" {
			pb.Constraints["memory"] = "12Gi"
		}
	default:
		if pb.Constraints["cpu"] == "" {
			pb.Constraints["cpu"] = "1"
		}
		if pb.Constraints["memory"] == "" {
			pb.Constraints["memory"] = "2Gi"
		}
	}
	return pb
}

func fmtInt100(eid int) string {
	return fmt.Sprintf("HundredGigE0/0/0/%d", eid)
}

func fmtInt400(eid int) string {
	return fmt.Sprintf("FourHundredGigE0/0/0/%d", eid)
}

func getCiscoInterfaceId(pb *tpb.Node, eth string) (string, error) {
	ethWithIdRegx := regexp.MustCompile(`e(t(h(e(r(n(e(t)*)*)*)*)*)*)\d+`) // check for e|et|eth|....
	ethRegx := regexp.MustCompile(`e(t(h(e(r(n(e(t)*)*)*)*)*)*)`)          // check for e|et|eth|....
	if !ethWithIdRegx.MatchString(eth) {
		return "", fmt.Errorf("interface '%s' is invalid", eth)
	}
	if pb.Interfaces[eth].Name != "" {
		return pb.Interfaces[eth].Name, nil
	}
	// ethWithIdRegx.MatchString(eth) was successful, so no need to do extra check here
	ethId, _ := strconv.Atoi(ethRegx.Split(eth, -1)[1])
	eid := ethId - 1
	switch pb.Model {
	case "8201":
		switch {
		case eid <= 23:
			return fmtInt400(eid), nil
		case eid <= 35:
			return fmtInt100(eid), nil
		}
		return "", fmt.Errorf("interface id %d can not be mapped to a cisco interface, eth1..eth36 is supported on %s ", ethId, pb.Model)
	case "8202":
		switch {
		case eid <= 47:
			return fmtInt100(eid), nil
		case eid <= 59:
			return fmtInt400(eid), nil
		case eid <= 71:
			return fmtInt100(eid), nil
		}
		return "", fmt.Errorf("interface id %d can not be mapped to a cisco interface, eth1..eth72 is supported on %s ", ethId, pb.Model)
	case "8201-32FH":
		if eid <= 31 {
			return fmtInt400(eid), nil
		}
		return "", fmt.Errorf("interface id %d can not be mapped to a cisco interface, eth1..eth32 is supported on %s ", ethId, pb.Model)
	case "8101-32H":
		if eid <= 31 {
			return fmtInt100(eid), nil
		}
		return "", fmt.Errorf("interface id %d can not be mapped to a cisco interface, eth1..eth32 is supported on %s ", ethId, pb.Model)
	case "8102-64H":
		if eid <= 63 {
			return fmtInt100(eid), nil
		}
		return "", fmt.Errorf("interface id %d can not be mapped to a cisco interface, eth1..eth64 is supported on %s ", ethId, pb.Model)
	default:
		return fmt.Sprintf("GigabitEthernet0/0/0/%d", eid), nil
	}
}

func generateInterfacesEnvs(pb *tpb.Node) (interfaceList, interfaceMap string, err error) {
	interfaceList = "MgmtEther0/RP0/CPU0/0"
	interfaceMap = "MgmtEther0/RP0/CPU0/0:eth0"
	eths := make([]string, 0)
	for k := range pb.Interfaces {
		eths = append(eths, k)
	}
	sort.Strings(eths)
	for _, eth := range eths {
		ciscoInterfaceId, err := getCiscoInterfaceId(pb, eth)
		if err == nil {
			interfaceList = fmt.Sprintf("%s,%s", interfaceList, ciscoInterfaceId)
			interfaceMap = fmt.Sprintf("%s,%s:%s", interfaceMap, ciscoInterfaceId, eth)
		} else {
			return interfaceList, interfaceMap, err
		}
	}
	return interfaceList, interfaceMap, nil
}

func defaults(pb *tpb.Node) (*tpb.Node, error) {
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
	pb = constraints(pb)
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
	interfaceList, interfaceMap, err := generateInterfacesEnvs(pb)
	if err != nil {
		return nil, err
	}
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
	return pb, nil
}

func init() {
	node.Vendor(tpb.Vendor_CISCO, New)
}
