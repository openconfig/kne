// Juniper cPTX for KNE
// Copyright (c) Juniper Networks, Inc., 2021. All rights reserved.

package juniper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	scraplinetwork "github.com/scrapli/scrapligo/driver/network"
	scrapliopts "github.com/scrapli/scrapligo/driver/options"
	scrapliutil "github.com/scrapli/scrapligo/util"
	scraplicfg "github.com/scrapli/scrapligocfg"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	log "k8s.io/klog/v2"
	"k8s.io/utils/pointer"
)

// ErrIncompatibleCliConn raised when an invalid scrapligo cli transport type is found.
var ErrIncompatibleCliConn = errors.New("incompatible cli connection in use")

var (
	// For committing a very large config
	scrapliOperationTimeout = 300 * time.Second
	// Wait for PKI cert infra
	certGenTimeout = 15 * time.Minute
	// Time between polls
	certGenRetrySleep = 30 * time.Second
	// Wait for config mode
	configModeTimeout = 15 * time.Minute
	// Time between polls - config mode
	configModeRetrySleep = 30 * time.Second
	// Default gRPC port
	defaultGrpcPort = uint32(9339)

	defaultNCPTXConstraints = node.Constraints{
		CPU:    "4000m", // 4000 milliCPUs
		Memory: "4Gi",   // 4 GB RAM
	}

	defaultCPTXConstraints = node.Constraints{
		CPU:    "8000m", // 8000 milliCPUs
		Memory: "8Gi",   // 8 GB RAM
	}
)

const (
	scrapliPlatformName = "juniper_junos"
	ModelNCPTX          = "ncptx"
	ModelCPTX           = "cptx"
)

var (
	defaultNode = tpb.Node{
		Name: "default_ncptx_node",
		Services: map[uint32]*tpb.Service{
			443: {
				Names:  []string{"ssl"},
				Inside: 443,
			},
			22: {
				Names:  []string{"ssh"},
				Inside: 22,
			},
			9339: {
				Names:  []string{"gnmi", "gnoi", "gnsi"},
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
		},
		Constraints: map[string]string{
			"cpu":    defaultNCPTXConstraints.CPU,
			"memory": defaultNCPTXConstraints.Memory,
		},
		Os:    "evo",
		Model: ModelNCPTX,
		Labels: map[string]string{
			"vendor":              tpb.Vendor_JUNIPER.String(),
			"model":               ModelNCPTX,
			"os":                  "evo",
			node.OndatraRoleLabel: node.OndatraRoleDUT,
		},
		Config: &tpb.Config{
			EntryCommand: fmt.Sprintf("kubectl exec -it %s -- cli", "default_ncptx_node"),
			ConfigPath:   "/home/evo/configdisk",
			ConfigFile:   "juniper.conf",
			Image:        "ncptx:latest",
			Cert: &tpb.CertificateCfg{
				Config: &tpb.CertificateCfg_SelfSigned{
					SelfSigned: &tpb.SelfSignedCertCfg{
						CertName: "grpc-server-cert",
						KeyName:  "my_key",
						KeySize:  4096,
					},
				},
			},
			Command: []string{"/sbin/cevoCntrEntryPoint"},
			Env: map[string]string{
				"JUNOS_EVOLVED_CONTAINER": "1",
			},
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
	cliConn *scraplinetwork.Driver

	// scrapli options used in testing
	testOpts []scrapliutil.Option
}

// Add validations for interfaces the node provides
var (
	_ node.Certer       = (*Node)(nil)
	_ node.ConfigPusher = (*Node)(nil)
	_ node.Resetter     = (*Node)(nil)
)

// SpawnCLIConn spawns a CLI connection towards a Network OS using `kubectl exec` terminal and ensures CLI is ready
// to accept inputs.
// scrapligo options can be provided to this function for a caller to modify scrapligo platform.
// For example, mock transport can be set via options
func (n *Node) SpawnCLIConn() error {
	opts := []scrapliutil.Option{
		scrapliopts.WithAuthBypass(),
		scrapliopts.WithTimeoutOps(scrapliOperationTimeout),
	}

	// add options defined in test package
	opts = append(opts, n.testOpts...)

	opts = n.PatchCLIConnOpen("kubectl", []string{"cli"}, opts)

	var err error
	n.cliConn, err = n.GetCLIConn(scrapliPlatformName, opts)

	return err
}

// DefaultNodeConstraints returns default node constraints for Juniper.
// If the model for cptx is specificied correctly it returns defaults for cptx.
// Otherwise, it returns defaults for ncptx by default.
func (n *Node) DefaultNodeConstraints() node.Constraints {
	if n.Impl == nil || n.Impl.Proto == nil {
		return defaultNCPTXConstraints
	}
	switch n.GetProto().Model {
	case ModelCPTX:
		return defaultCPTXConstraints
	default:
	}

	return defaultNCPTXConstraints
}

// Returns config required to configure gRPC service
func (n *Node) GRPCConfig() []string {
	port := defaultGrpcPort
	for _, service := range n.GetProto().GetServices() {
		if service.GetName() == "gnmi" {
			port = service.GetInside()
		}
	}
	log.Infof("gNMI Port %d", port)
	return []string{
		"set system services http servers server grpc-server-9339",
		fmt.Sprintf("set system services http servers server grpc-server-9339 port %d", port),
		"set system services http servers server grpc-server-9339 grpc gnmi",
		"set system services http servers server grpc-server-9339 grpc gnoi",
		"set system services http servers server grpc-server-9339 grpc gnsi",
		"set system services http servers server grpc-server-9339 tls local-certificate grpc-server-cert",
		"set system services http servers server grpc-server-9339 listen-address 0.0.0.0",
		"set system services http servers server grpc-server-9339 grpc all-grpc max-connections 300",
		"set system services http servers server grpc-server-9340",
		"set system services http servers server grpc-server-9340 port 9340",
		"set system services http servers server grpc-server-9340 grpc gribi",
		"set system services http servers server grpc-server-9340 tls local-certificate grpc-server-cert",
		"set system services http servers server grpc-server-9340 listen-address 0.0.0.0",
		"set system services http servers server grpc-server-9340 grpc all-grpc max-connections 300",
		"set system services http servers server grpc-server-9559",
		"set system services http servers server grpc-server-9559 port 9559",
		"set system services http servers server grpc-server-9559 grpc p4",
		"set system services http servers server grpc-server-9559 tls local-certificate grpc-server-cert",
		"set system services http servers server grpc-server-9559 listen-address 0.0.0.0",
		"set system services http servers server grpc-server-9559 grpc all-grpc max-connections 300",
		"commit",
	}
}

// Waits and retries until CLI config mode is up and config is applied
func (n *Node) waitConfigInfraReadyAndPushConfigs(configs []string) error {
	log.Infof("Waiting for config to be pushed (timeout: %v) node %s", configModeTimeout, n.Name())
	start := time.Now()
	for time.Since(start) < configModeTimeout {
		multiresp, err := n.cliConn.SendConfigs(configs)
		if err != nil {
			if strings.Contains(err.Error(), "errPrivilegeError") || strings.Contains(err.Error(), "errTimeoutError") {
				log.Infof("Config mode not ready. Retrying in %v. Node %s, Resp %v", configModeRetrySleep, n.Name(), err)
			} else {
				return fmt.Errorf("failed pushing configs: %v", err)
			}
		} else {
			for _, resp := range multiresp.Responses {
				if resp.Failed != nil {
					return resp.Failed
				}
				if strings.Contains(resp.Result, "commit complete") {
					log.Infof("Config mode ready. Config commit done. Node %s", n.Name())
					return nil
				}
				if strings.Contains(resp.Result, "error:") {
					log.Infof("Config mode not ready. Retrying in %v. Node %s Response %s", certGenRetrySleep, n.Name(), multiresp.JoinedResult())
				}
			}
		}
		time.Sleep(configModeRetrySleep)
	}

	return fmt.Errorf("failed sending configs")
}

// Waits and retries until Cert infra is up and certs are applied
func (n *Node) waitCertInfraReadyAndPushCert() error {
	selfSigned := n.Proto.GetConfig().GetCert().GetSelfSigned()
	commands := []string{
		fmt.Sprintf("request security pki generate-key-pair certificate-id %s", selfSigned.GetCertName()),
		fmt.Sprintf("request security pki local-certificate generate-self-signed certificate-id %s "+
			"subject CN=abc domain-name google.com ip-address 1.2.3.4 email example@google.com",
			selfSigned.GetCertName()),
	}

	log.Infof("Waiting for certificates to be pushed (timeout: %v) node %s", certGenTimeout, n.Name())
	start := time.Now()
	for time.Since(start) < certGenTimeout {
		multiresp, err := n.cliConn.SendCommands(commands)
		if err != nil {
			return fmt.Errorf("failed sending generate-self-signed commands: %v", err)
		}
		for _, resp := range multiresp.Responses {
			if resp.Failed != nil {
				return resp.Failed
			}
			if strings.Contains(resp.Result, "successfully") {
				log.Infof("Cert Infra ready. Configured Certs. Node %s, Response %s", n.Name(), multiresp.JoinedResult())
				return nil
			}
			if strings.Contains(resp.Result, "error:") {
				log.Infof("Cert infra isn't ready. Retrying in %v. Node %s Response %s", certGenRetrySleep, n.Name(), multiresp.JoinedResult())
			}
		}
		time.Sleep(certGenRetrySleep)
	}

	return fmt.Errorf("failed sending generate-self-signed commands")
}

// GenerateSelfSigned generates a self-signed TLS certificate using Junos PKI
func (n *Node) GenerateSelfSigned(ctx context.Context) error {
	selfSigned := n.Proto.GetConfig().GetCert().GetSelfSigned()
	if selfSigned == nil {
		log.Infof("%s - no cert config", n.Name())
		return nil
	}
	log.Infof("%s - generating self signed certs", n.Name())
	log.Infof("%s - waiting for pod to be running", n.Name())
	w, err := n.KubeClient.CoreV1().Pods(n.Namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: fields.SelectorFromSet(
			fields.Set{metav1.ObjectNameField: n.Name()},
		).String(),
	})
	if err != nil {
		return err
	}
	for e := range w.ResultChan() {
		p, ok := e.Object.(*corev1.Pod)
		if !ok {
			continue
		}
		if p.Status.Phase == corev1.PodRunning {
			break
		}
	}
	log.Infof("%s - pod running.", n.Name())

	if err := n.SpawnCLIConn(); err != nil {
		return err
	}

	if !n.cliConn.Transport.IsAlive() {
		return errors.New("scrapligo device driver not open")
	}

	// wait for cert infra to be ready and push certs
	if err := n.waitCertInfraReadyAndPushCert(); err != nil {
		return err
	}

	// Send gRPC config
	if err := n.waitConfigInfraReadyAndPushConfigs(n.GRPCConfig()); err != nil {
		return fmt.Errorf("failed sending grpc config commands - self-signed-cert: %v", err)
	}

	log.Infof("%s - finished cert generation", n.Name())

	return n.cliConn.Close()
}

func (n *Node) ConfigPush(ctx context.Context, r io.Reader) error {
	log.Infof("%s - pushing config", n.Name())

	cfg, err := io.ReadAll(r)
	cfgs := string(cfg)

	if len(cfgs) == 0 {
		log.Infof("%s - empty config! not pushing", n.Name())
		return nil
	}

	log.V(1).Info(cfgs)

	if err != nil {
		return err
	}

	err = n.SpawnCLIConn()
	if err != nil {
		return err
	}

	defer n.cliConn.Close()

	// use a static candidate file name for test transport
	var candidateConfigFile string
	if len(n.testOpts) != 0 {
		candidateConfigFile = "scrapli_cfg_testing"
	}

	c, err := scraplicfg.NewCfg(
		n.cliConn,
		"juniper_junos",
		scraplicfg.WithCandidateName(candidateConfigFile),
	)
	if err != nil {
		return err
	}

	err = c.Prepare()
	if err != nil {
		return err
	}

	resp, err := c.LoadConfig(
		cfgs,
		false, // load merge
	)
	if err != nil {
		return err
	}
	if resp.Failed != nil {
		return resp.Failed
	}

	resp, err = c.CommitConfig()
	if err != nil {
		return err
	}
	if resp.Failed != nil {
		return resp.Failed
	}

	log.Infof("%s - finished config push", n.Name())

	return nil
}

func (n *Node) ResetCfg(ctx context.Context) error {
	log.Infof("%s - resetting config", n.Name())

	err := n.SpawnCLIConn()
	if err != nil {
		return err
	}

	defer n.cliConn.Close()

	cfgs := []string{
		// override the current one with the factory config passed via KNE
		"load override /var/vmguest/config/juniper.conf",
		"commit",
	}
	multiresp, err := n.cliConn.SendConfigs(cfgs)
	if err != nil {
		return err
	}
	for _, resp := range multiresp.Responses {
		if resp.Failed != nil {
			return resp.Failed
		}
		if strings.Contains(resp.Result, "error:") {
			return fmt.Errorf("failed sending config-reset commands: %s", multiresp.JoinedResult())
		}
	}

	// Reset applies factory config which doesn't contain gRPC config
	// send gRPC config
	multiresp, err = n.cliConn.SendConfigs(n.GRPCConfig())
	if err != nil {
		return err
	}
	for _, resp := range multiresp.Responses {
		if resp.Failed != nil {
			return resp.Failed
		}
		if strings.Contains(resp.Result, "error:") {
			return fmt.Errorf("failed sending gRPC commands: %s", multiresp.JoinedResult())
		}
	}

	log.Infof("%s - finished resetting config", n.Name())
	return nil
}

func (n *Node) Create(ctx context.Context) error {
	log.Infof("Creating cPTX node resource %s model %s", n.Name(), n.Proto.Model)

	hpd := corev1.HostPathDirectory
	pb := n.Proto
	initContainerImage := pb.Config.InitImage
	if initContainerImage == "" {
		initContainerImage = node.DefaultInitContainerImage
	}

	// downward api - pass some useful values to container
	if n.isChannelized() {
		pb.Config.Env["CPTX_CHANNELIZED"] = "1"
	}
	pb.Config.Env["CPTX_CPU_LIMIT"] = pb.Constraints["cpu"]
	pb.Config.Env["CPTX_MEMORY_LIMIT"] = pb.Constraints["memory"]
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
				SecurityContext: &corev1.SecurityContext{
					Privileged: pointer.Bool(true),
					RunAsUser:  pointer.Int64(0),
					Capabilities: &corev1.Capabilities{
						Add: []corev1.Capability{"SYS_ADMIN", "NET_ADMIN"},
					},
					SeccompProfile: &corev1.SeccompProfile{
						Type: "Unconfined",
					},
				},
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      fmt.Sprintf("%s-run-mount", pb.Name),
						ReadOnly:  false,
						MountPath: "/run",
					},
					{
						Name:      fmt.Sprintf("%s-tmp-mount", pb.Name),
						ReadOnly:  false,
						MountPath: "/tmp",
					},
					{
						Name:      fmt.Sprintf("%s-dev-shm-mount", pb.Name),
						ReadOnly:  false,
						MountPath: "/dev/shm",
					},
					{
						Name:      fmt.Sprintf("%s-cgroup-mount", pb.Name),
						ReadOnly:  false,
						MountPath: "/sys/fs/cgroup",
					},
				},
			}},
			Volumes: []corev1.Volume{
				{
					Name: fmt.Sprintf("%s-run-mount", pb.Name),
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{
							Medium: "Memory",
						},
					},
				},
				{
					Name: fmt.Sprintf("%s-tmp-mount", pb.Name),
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{
							Medium: "Memory",
						},
					},
				},
				{
					Name: fmt.Sprintf("%s-dev-shm-mount", pb.Name),
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{
							Medium:    "Memory",
							SizeLimit: resource.NewQuantity(2*1024*1024*1024, resource.BinarySI),
						},
					},
				},
				{
					Name: fmt.Sprintf("%s-cgroup-mount", pb.Name),
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/sys/fs/cgroup",
							Type: &hpd,
						},
					},
				},
			},
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
	log.Infof("Created cPTX node resource %s pod model %s", n.Name(), n.Proto.Model)
	if err := n.CreateService(ctx); err != nil {
		return err
	}
	log.Infof("Created cPTX node resource %s services", n.Name())
	return nil
}

func defaults(pb *tpb.Node) *tpb.Node {
	defaultNodeClone := proto.Clone(&defaultNode).(*tpb.Node)
	if pb == nil {
		pb = &tpb.Node{
			Name: defaultNodeClone.Name,
		}
	}
	if pb.Model == "" {
		pb.Model = defaultNodeClone.Model
	}
	if pb.Os == "" {
		pb.Os = defaultNodeClone.Os
	}
	if pb.Config == nil {
		pb.Config = &tpb.Config{}
	}
	switch pb.Model {
	case ModelNCPTX:
		if pb.Constraints == nil {
			pb.Constraints = defaultNodeClone.Constraints
		}
		if pb.Constraints["cpu"] == "" {
			pb.Constraints["cpu"] = defaultNCPTXConstraints.CPU
		}
		if pb.Constraints["memory"] == "" {
			pb.Constraints["memory"] = defaultNCPTXConstraints.Memory
		}
		if len(pb.Config.GetCommand()) == 0 {
			pb.Config.Command = defaultNodeClone.Config.Command
		}
		if pb.Config.Image == "" {
			pb.Config.Image = defaultNodeClone.Config.Image
		}
	default:
		if pb.Constraints == nil {
			pb.Constraints = map[string]string{}
		}
		if pb.Constraints["cpu"] == "" {
			pb.Constraints["cpu"] = defaultCPTXConstraints.CPU
		}
		if pb.Constraints["memory"] == "" {
			pb.Constraints["memory"] = defaultCPTXConstraints.Memory
		}
		if len(pb.Config.GetCommand()) == 0 {
			pb.Config.Command = []string{
				"/entrypoint.sh",
			}
		}
		if pb.Config.Image == "" {
			pb.Config.Image = "cptx:latest"
		}
	}
	if pb.Services == nil {
		pb.Services = defaultNodeClone.Services
	}
	if pb.Config.Cert == nil {
		pb.Config.Cert = defaultNodeClone.Config.Cert
	}
	if pb.Config.Env == nil {
		pb.Config.Env = defaultNodeClone.Config.Env
	}
	if pb.Config.EntryCommand == "" {
		pb.Config.EntryCommand = fmt.Sprintf("kubectl exec -it %s -- cli", pb.Name)
	}
	if pb.Config.ConfigPath == "" {
		pb.Config.ConfigPath = "/home/evo/configdisk"
	}
	if pb.Config.ConfigFile == "" {
		pb.Config.ConfigFile = "juniper.conf"
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
	return pb
}

// isChannelized is a helper function that returns 1 if cptx is channelized
func (n *Node) isChannelized() bool {
	interfaces := n.GetProto().GetInterfaces()
	for key, value := range interfaces {
		if strings.Contains(key, "eth") && strings.Contains(value.Name, ":") {
			return true
		}
	}
	return false
}

func init() {
	node.Vendor(tpb.Vendor_JUNIPER, New)
}
