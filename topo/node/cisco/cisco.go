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
package cisco

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	scraplinetwork "github.com/scrapli/scrapligo/driver/network"
	scrapliopts "github.com/scrapli/scrapligo/driver/options"
	scrapliutil "github.com/scrapli/scrapligo/util"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	log "k8s.io/klog/v2"
	"k8s.io/utils/pointer"
)

const (
	ModelXRD = "xrd"

	scrapliPlatformName     = "cisco_iosxr"
	reset8000eCMD           = "copy disk0:/startup-config running-config replace"
	scrapliOperationTimeout = 300 * time.Second
)

var podIsUpRegex = regexp.MustCompile(`Router up`)

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

	cliConn *scraplinetwork.Driver

	// scrapli options used in testing
	testOpts []scrapliutil.Option
}

// Add validations for interfaces the node provides
var (
	_ node.Resetter = (*Node)(nil)
)

// For enabling option to skip validation in unit tests
var skipValidation = false

func (n *Node) Create(ctx context.Context) error {
	log.Infof("Creating Cisco %s node resource %s", n.Proto.Model, n.Name())

	pb := n.Proto
	if err := validateHostConstraints(n, skipValidation); err != nil {
		return err
	}
	initContainerImage := pb.Config.InitImage
	if initContainerImage == "" {
		initContainerImage = node.DefaultInitContainerImage
	}
	secContext := &corev1.SecurityContext{
		Privileged: pointer.Bool(true),
	}
	if pb.Model == ModelXRD {
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
				Image: initContainerImage,
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
		return fmt.Errorf("failed to create pod for %q: %w", pb.Name, err)
	}
	log.V(1).Infof("Pod created:\n%+v\n", sPod)
	log.Infof("Created Cisco %s node resource %s pod", n.Proto.Model, n.Name())
	if err := n.CreateService(ctx); err != nil {
		return err
	}
	log.Infof("Created Cisco %s node resource %s services", n.Proto.Model, n.Name())
	return nil
}

// validateHostConstraints - Validates host contraints through the default node's implementation. It skips the validation optionally
// based on skipValidation flag which is useful for unit tests
func validateHostConstraints(n *Node, skipValidation bool) error {
	if skipValidation {
		return nil
	}
	if err := n.ValidateConstraints(); err != nil {
		return fmt.Errorf("host constraints validation failed for node %s with error %s", n.Name(), err)
	}
	return nil
}

func constraints(pb *tpb.Node) *tpb.Node {
	if pb.Constraints == nil {
		pb.Constraints = map[string]string{}
	}
	switch pb.Model {
	//nolint:goconst
	case "8201", "8201-32FH", "8202", "8101-32H", "8102-64H":
		if pb.Constraints["cpu"] == "" {
			pb.Constraints["cpu"] = "4"
		}
		if pb.Constraints["memory"] == "" {
			pb.Constraints["memory"] = "20Gi"
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

func setE8000Env(pb *tpb.Node) error {
	interfaceList := "MgmtEther0/RP0/CPU0/0"
	interfaceMap := "MgmtEther0/RP0/CPU0/0:eth0"
	var eths []string
	for k := range pb.Interfaces {
		eths = append(eths, k)
	}
	sort.Strings(eths)
	for _, eth := range eths {
		ciscoInterfaceID, err := getCiscoInterfaceID(pb, eth)
		if err != nil {
			return err
		}
		interfaceList = fmt.Sprintf("%s,%s", interfaceList, ciscoInterfaceID)
		interfaceMap = fmt.Sprintf("%s,%s:%s", interfaceMap, ciscoInterfaceID, eth)
	}
	if pb.Config.Env == nil {
		pb.Config.Env = map[string]string{}
	}
	if pb.Config.Env["XR_INTERFACES"] == "" {
		pb.Config.Env["XR_INTERFACES"] = interfaceMap
	}
	if pb.Config.Env["XR_CHECKSUM_OFFLOAD_COUNTERACT"] == "" {
		pb.Config.Env["XR_CHECKSUM_OFFLOAD_COUNTERACT"] = interfaceList
	}
	if pb.Config.Env["XR_EVERY_BOOT_CONFIG"] == "" {
		pb.Config.Env["XR_EVERY_BOOT_CONFIG"] = filepath.Join(pb.Config.ConfigPath, pb.Config.ConfigFile)
	}

	return nil
}

func setXRDEnv(pb *tpb.Node) error {
	interfaceMap := ""
	var eths []string
	for k := range pb.Interfaces {
		eths = append(eths, k)
	}
	sort.Strings(eths)
	for _, eth := range eths {
		ciscoInterfaceID, err := getCiscoInterfaceID(pb, eth)
		if err != nil {
			return err
		}
		if interfaceMap == "" {
			interfaceMap = fmt.Sprintf("linux:%s,xr_name=%s", eth, ciscoInterfaceID)
		} else {
			interfaceMap = fmt.Sprintf("%s;linux:%s,xr_name=%s", interfaceMap, eth, ciscoInterfaceID)
		}
	}
	if pb.Config.Env == nil {
		pb.Config.Env = map[string]string{}
	}
	if pb.Config.Env["XR_INTERFACES"] == "" {
		pb.Config.Env["XR_INTERFACES"] = interfaceMap
	}
	if pb.Config.Env["XR_EVERY_BOOT_CONFIG"] == "" {
		pb.Config.Env["XR_EVERY_BOOT_CONFIG"] = filepath.Join(pb.Config.ConfigPath, pb.Config.ConfigFile)
	}
	pb.Config.Env["XR_MGMT_INTERFACES"] = "linux:eth0,xr_name=MgmtEth0/RP0/CPU0/0,chksum,snoop_v4,snoop_v6"
	return nil
}

func getCiscoInterfaceID(pb *tpb.Node, eth string) (string, error) {
	ethWithIDRegx := regexp.MustCompile(`e(t(h(e(r(n(e(t)*)*)*)*)*)*)\d+`) // check for e|et|eth|....
	ethRegx := regexp.MustCompile(`e(t(h(e(r(n(e(t)*)*)*)*)*)*)`)
	if !ethWithIDRegx.MatchString(eth) {
		return "", fmt.Errorf("interface '%s' is invalid", eth)
	}
	if pb.Interfaces[eth].Name != "" {
		return pb.Interfaces[eth].Name, nil
	}
	// ethWithIDRegx.MatchString(eth) was successful, so no need to do extra check here
	ethID, _ := strconv.Atoi(ethRegx.Split(eth, -1)[1])
	eid := ethID - 1
	switch pb.Model {
	case "8201":
		switch {
		case eid <= 23:
			return fmtInt400(eid), nil
		case eid <= 35:
			return fmtInt100(eid), nil
		}
		return "", fmt.Errorf("interface id %d can not be mapped to a cisco interface, eth1..eth36 is supported on %s ", ethID, pb.Model)
	case "8202":
		switch {
		case eid <= 47:
			return fmtInt100(eid), nil
		case eid <= 59:
			return fmtInt400(eid), nil
		case eid <= 71:
			return fmtInt100(eid), nil
		}
		return "", fmt.Errorf("interface id %d can not be mapped to a cisco interface, eth1..eth72 is supported on %s ", ethID, pb.Model)
	case "8201-32FH": //nolint:goconst
		if eid <= 31 {
			return fmtInt400(eid), nil
		}
		return "", fmt.Errorf("interface id %d can not be mapped to a cisco interface, eth1..eth32 is supported on %s ", ethID, pb.Model)
	case "8101-32H":
		if eid <= 31 {
			return fmtInt100(eid), nil
		}
		return "", fmt.Errorf("interface id %d can not be mapped to a cisco interface, eth1..eth32 is supported on %s ", ethID, pb.Model)
	case "8102-64H":
		if eid <= 63 {
			return fmtInt100(eid), nil
		}
		return "", fmt.Errorf("interface id %d can not be mapped to a cisco interface, eth1..eth64 is supported on %s ", ethID, pb.Model)
	default:
		return fmt.Sprintf("GigabitEthernet0/0/0/%d", eid), nil
	}
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
	if pb.Config.Cert == nil {
		pb.Config.Cert = &tpb.CertificateCfg{
			Config: &tpb.CertificateCfg_SelfSigned{
				SelfSigned: &tpb.SelfSignedCertCfg{
					CertName: "ems.pem",
					KeyName:  "ems.key",
					KeySize:  2048,
				},
			},
		}
	}
	if pb.Model == "" {
		pb.Model = ModelXRD
	}
	if pb.Os == "" {
		pb.Os = "ios-xr"
	}
	pb = constraints(pb)
	if pb.Services == nil {
		pb.Services = map[uint32]*tpb.Service{
			22: {
				Names:  []string{"ssh"},
				Inside: 22,
			},
			9339: {
				Names:  []string{"gnmi", "gnoi", "gnsi"},
				Inside: 57400,
			},
			9340: {
				Names:  []string{"gribi"},
				Inside: 57400,
			},
			9559: {
				Names:  []string{"p4rt"},
				Inside: 57400,
			},
		}
	}
	if pb.Labels == nil {
		pb.Labels = map[string]string{}
	}
	if pb.Labels["vendor"] == "" {
		pb.Labels["vendor"] = tpb.Vendor_CISCO.String()
	}
	if pb.Labels["model"] == "" {
		pb.Labels["model"] = pb.Model
	}
	if pb.Labels["os"] == "" {
		pb.Labels["os"] = pb.Os
	}
	if pb.Labels[node.OndatraRoleLabel] == "" {
		pb.Labels[node.OndatraRoleLabel] = node.OndatraRoleDUT
	}
	if pb.Config.EntryCommand == "" {
		pb.Config.EntryCommand = fmt.Sprintf("kubectl exec -it %s -- bash", pb.Name)
	}
	switch pb.Model {
	default:
		return nil, fmt.Errorf("unexpected model %q", pb.Model)
	case ModelXRD:
		if err := setXRDEnv(pb); err != nil {
			return nil, err
		}
		if pb.Config.Image == "" {
			pb.Config.Image = "xrd:latest"
		}
		if pb.HostConstraints == nil {
			pb.HostConstraints = append(pb.HostConstraints,
				&tpb.HostConstraint{Constraint: &tpb.HostConstraint_KernelConstraint{
					KernelConstraint: &tpb.KernelParam{Name: "fs.inotify.max_user_instances",
						ConstraintType: &tpb.KernelParam_BoundedInteger{BoundedInteger: &tpb.BoundedInteger{
							MinValue: 64000}}}}})
		}
	//nolint:goconst
	case "8201", "8202", "8201-32FH", "8102-64H", "8101-32H":
		if err := setE8000Env(pb); err != nil {
			return nil, err
		}
		if pb.Config.Image == "" {
			pb.Config.Image = "8000e:latest"
		}
		if pb.HostConstraints == nil {
			pb.HostConstraints = append(pb.HostConstraints,
				&tpb.HostConstraint{Constraint: &tpb.HostConstraint_KernelConstraint{
					KernelConstraint: &tpb.KernelParam{Name: "kernel.pid_max",
						ConstraintType: &tpb.KernelParam_BoundedInteger{BoundedInteger: &tpb.BoundedInteger{
							MaxValue: 1048575}}}}})
		}
	}
	return pb, nil
}

// Status returns the current node state.
// For 8000e nodes it checks the logs and return running if log contains "Router up"
func (n *Node) Status(ctx context.Context) (node.Status, error) {
	p, err := n.Pods(ctx)
	if err != nil {
		return node.StatusUnknown, err
	}
	if len(p) != 1 {
		return node.StatusUnknown, fmt.Errorf("expected exactly one pod for node %s", n.Name())
	}
	switch p[0].Status.Phase {
	case corev1.PodPending:
		return node.StatusPending, nil
	case corev1.PodUnknown:
		return node.StatusUnknown, nil
	case corev1.PodFailed:
		return node.StatusFailed, nil
	case corev1.PodSucceeded, corev1.PodRunning:
		pb := n.Proto
		if pb.GetModel() != ModelXRD {
			req := n.KubeClient.CoreV1().Pods(p[0].Namespace).GetLogs(p[0].Name, &corev1.PodLogOptions{})
			if !isNode8000eUp(ctx, req) {
				log.V(2).Infof("Cisco %s node %s status is %v", n.Proto.Model, n.Name(), node.StatusPending)
				return node.StatusPending, nil
			}
		}
		for _, cond := range p[0].Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status != corev1.ConditionTrue {
				log.V(2).Infof("Cisco %s node %s status is %v", n.Proto.Model, n.Name(), node.StatusPending)
				return node.StatusPending, nil
			}
		}
		log.Infof("Cisco %s node %s status is %v ", n.Proto.Model, n.Name(), node.StatusRunning)
		return node.StatusRunning, nil
	default:
		return node.StatusUnknown, nil
	}
}

func isNode8000eUp(ctx context.Context, req *rest.Request) bool {
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return false
	}
	defer podLogs.Close()
	buf := new(bytes.Buffer)
	len, err := io.Copy(buf, podLogs)
	if err != nil || len == 0 {
		return false
	}
	return podIsUpRegex.Match(buf.Bytes())
}

// SpawnCLIConn spawns a CLI connection towards a IOSXR using `kubectl exec` terminal and ensures CLI is ready
// to accept inputs.
// scrapligo options can be provided to this function for a caller to modify scrapligo platform.
// For example, mock transport can be set via options
func (n *Node) SpawnCLIConn() error {
	// opts := []scrapliutil.Option{
	// 	scrapliopts.WithAuthBypass(),
	// 	scrapliopts.WithTimeoutOps(scrapliOperationTimeout),
	// }
	// // add options defined in test package
	// opts = append(opts, n.testOpts...)
	// opts = n.PatchCLIConnOpen("kubectl", []string{"xr"}, opts)
	// if n.Proto.Model != ModelXRD {
	// 	opts = n.PatchCLIConnOpen("kubectl", []string{"telnet", "0", "60000"}, opts)
	// }
	// var err error
	opts := []scrapliutil.Option{
		scrapliopts.WithAuthBypass(),
		scrapliopts.WithTimeoutOps(scrapliOperationTimeout),
	}
	// add options defined in test package
	opts = append(opts, n.testOpts...)
	opts = n.PatchCLIConnOpen("kubectl", []string{"xr"}, opts)
	if n.Proto.Model != ModelXRD {
		opts = n.PatchCLIConnOpen("kubectl", []string{"telnet", "0", "60000"}, opts)
	}
	var err error
	n.cliConn, err = n.GetCLIConn(scrapliPlatformName, opts)
	// TODO: add the following pattern in the scrapli/scrapligo/blob/main/assets/platforms/cisco_iosxr.yaml
	n.cliConn.FailedWhenContains = append(n.cliConn.FailedWhenContains, "ERROR")
	n.cliConn.FailedWhenContains = append(n.cliConn.FailedWhenContains, "% Failed")

	if n.Proto.Model != ModelXRD {
		n.cliConn.OnClose = endTelnet
	}

	return err
}

func endTelnet(d *scraplinetwork.Driver) error {
	// sending ctrl + ] (^]) to end telnet session gracefully. Otherwise, the next connection can be blocked.
	endTelnet := string(byte(29)) + " quit\n"
	log.Infof("Closing the connection by sending ctrl+] quit \n")
	d.SendCommand(endTelnet)
	return nil
}

func (n *Node) ResetCfg(ctx context.Context) error {
	if n.Proto.Model == ModelXRD {
		return status.Errorf(codes.Unimplemented, "reset config is not implemented for cisco xrd node")
	}

	log.Infof("%s resetting config", n.Name())
	err := n.SpawnCLIConn()
	if err != nil {
		return err
	}
	defer n.cliConn.Close()

	resp1, err1 := n.cliConn.SendCommand("show version")
	log.Infof("show version output: %s", resp1.Result)

	resp, err := n.cliConn.SendCommand(reset8000eCMD)
	if err != nil {
		return err
	}
	if resp.Failed == nil {
		log.Infof("%s - finished resetting config", n.Name())
	}
	return resp.Failed
}

// processConfig removes end command from config
// since running it can lead to interactive prompt which is not handled.
// Also it add commits to the end of config if it is missing
func processConfig(cfg string) string {
	processedCfg := ""
	lines := strings.Split(cfg, "\n")
	lastLine := ""
	for _, line := range lines {
		if strings.ToLower(strings.Trim(line, " ")) == "end" {
			continue
		}
		lastLine = line
		processedCfg += line + "\n"
	}
	if strings.ToLower(strings.Trim(lastLine, " ")) != "commit" {
		processedCfg += "commit\n"
	}
	return processedCfg
}

func (n *Node) ConfigPush(ctx context.Context, r io.Reader) error {

	log.Infof("%s - pushing config foo", n.Name())

	cfg, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	cfgs := string(cfg)
	cfgs = processConfig(cfgs)
	log.V(1).Info(cfgs)

	err = n.SpawnCLIConn()
	if err != nil {
		return err
	}
	defer n.cliConn.Close()


	resp1, err1 := n.cliConn.SendCommand("show version")
	log.Infof("show version output: %s", resp1.Result)


	resp, err := n.cliConn.SendConfig(cfgs)
	if err != nil {
		return err
	}
	if resp.Failed == nil {
		log.Infof("%s - finished config push", n.Impl.Proto.Name)
	}

	return resp.Failed
}

func (n *Node) GenerateSelfSigned(context.Context) error {
	// IOS XR automatically generates a self-signed certificate when gRPC is first enabled.
	// If the startup configuration contains a gRPC configuration, or if the user configures
	// gRPC after bootup, the self-signed cert will automatically be created and used.
	return status.Errorf(codes.Unimplemented, "certificate generation is not supported")
}

func init() {
	node.Vendor(tpb.Vendor_CISCO, New)
}
