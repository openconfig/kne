// Juniper cPTX for KNE
// Copyright (c) Juniper Networks, Inc., 2021. All rights reserved.

package cptx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"strings"
	"time"

	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	scraplinetwork "github.com/scrapli/scrapligo/driver/network"
	scrapliopts "github.com/scrapli/scrapligo/driver/options"
	scrapliutil "github.com/scrapli/scrapligo/util"
	scraplicfg "github.com/scrapli/scrapligocfg"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

// ErrIncompatibleCliConn raised when an invalid scrapligo cli transport type is found.
var ErrIncompatibleCliConn = errors.New("incompatible cli connection in use")

var (
	// For committing a very large config
	scrapliOperationTimeout = 300 * time.Second
)

const (
	defaultInitContainerImage = "networkop/init-wait:latest"

	scrapliPlatformName = "juniper_junos"
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
	_ node.ConfigPusher = (*Node)(nil)
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

	opts = n.PatchCLIConnOpen("kubectl", []string{"Cli", "-c"}, opts)

	var err error
	n.cliConn, err = n.GetCLIConn(scrapliPlatformName, opts)

	return err
}

func (n *Node) ConfigPush(ctx context.Context, r io.Reader) error {
	log.Infof("%s - pushing config", n.Name())

	cfg, err := ioutil.ReadAll(r)
	cfgs := string(cfg)

	log.Debug(cfgs)

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
		true, //load replace
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

	log.Infof("%s - finshed config push", n.Name())

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
		"load factory-default",
		"delete system commit factory-settings",
		// Plaintext password is 'Google123'
		// Setting a plaintext password would require an interactive prompt to enter the password.
		"set system root-authentication encrypted-password $6$7uA5z8vs$cmHIvL0aLU4ioWAHPR0PLeU/mJj.JO/5pQVQoqRlInK3fJNTLYLhwiDi.Q6gHhltSB3S1P/.raEsuDSH7akcJ/",
		// Without SSH enabled the cli binary returns with the message 'ssh is disabled'
		"set system services ssh root-login allow",
		"commit",
	}
	resp, err := n.cliConn.SendConfigs(cfgs)
	if err != nil {
		return err
	}

	if resp.Failed == nil {
		log.Infof("%s - finshed resetting config", n.Name())
	}

	return resp.Failed
}

func (n *Node) Create(ctx context.Context) error {
	log.Infof("Creating cPTX node resource %s", n.Name())

	if err := n.CreateConfig(ctx); err != nil {
		return fmt.Errorf("node %s failed to create config-map %w", n.Name(), err)
	}
	log.Infof("Created cPTX node %s configmap", n.Name())

	pb := n.Proto
	initContainerImage := pb.Config.InitImage
	if initContainerImage == "" {
		initContainerImage = defaultInitContainerImage
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
						Add: []corev1.Capability{"SYS_ADMIN"},
					},
				},
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
	log.Infof("Created cPTX node resource %s pod", n.Name())
	if err := n.CreateService(ctx); err != nil {
		return err
	}
	log.Infof("Created cPTX node resource %s services", n.Name())
	return nil
}

func defaults(pb *tpb.Node) *tpb.Node {
	if pb == nil {
		pb = &tpb.Node{
			Name: "default_cptx_node",
		}
	}
	if pb.Constraints == nil {
		pb.Constraints = map[string]string{
			"cpu":    "8",
			"memory": "8Gi",
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
			50051: {
				Name:   "gnmi",
				Inside: 50051,
			},
		}
	}
	if pb.Labels == nil {
		pb.Labels = map[string]string{
			"type":   tpb.Node_JUNIPER_CEVO.String(),
			"vendor": tpb.Vendor_JUNIPER.String(),
		}
	} else {
		if pb.Labels["type"] == "" {
			pb.Labels["type"] = tpb.Node_JUNIPER_CEVO.String()
		}
		if pb.Labels["vendor"] == "" {
			pb.Labels["vendor"] = tpb.Vendor_JUNIPER.String()
		}
	}
	if pb.Config == nil {
		pb.Config = &tpb.Config{}
	}
	if len(pb.Config.GetCommand()) == 0 {
		pb.Config.Command = []string{
			"/entrypoint.sh",
		}
	}
	if pb.Config.Image == "" {
		pb.Config.Image = "cptx:latest"
	}
	if pb.Config.Env == nil {
		pb.Config.Env = map[string]string{
			"CPTX": "1",
		}
	}
	if pb.Config.EntryCommand == "" {
		pb.Config.EntryCommand = fmt.Sprintf("kubectl exec -it %s -- cli -c", pb.Name)
	}
	if pb.Config.ConfigPath == "" {
		pb.Config.ConfigPath = "/home/evo/configdisk"
	}
	if pb.Config.ConfigFile == "" {
		pb.Config.ConfigFile = "juniper.conf"
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
	node.Register(tpb.Node_JUNIPER_CEVO, New)
	node.Vendor(tpb.Vendor_JUNIPER, New)
}
