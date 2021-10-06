package node

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"path/filepath"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/utils/pointer"

	topopb "github.com/google/kne/proto/topo"
)

type Interface interface {
	KubeClient() kubernetes.Interface
	RESTConfig() *rest.Config
	Interfaces() map[string]*Link
	Namespace() string
}

type Implementation interface {
	Proto() *topopb.Node
	// CreateNodeResource provides a custom implementation of pod creation
	// for a node type. Requires context, Kubernetes client interface and namespace.
	CreateNodeResource(context.Context, Interface) error
	// GetNodeResource provides a custom implementation of accessing vendor node status.
	// Requires context, Kubernetes client interface and namespace.
	GetNodeResourceStatus(context.Context, Interface) (NodeStatus, error)
	// CreateNodeResource provides a custom implementation of pod creation
	// for a node type. Requires context, Kubernetes client interface and namespace.
	DeleteNodeResource(context.Context, Interface) error
}

type NewNodeFn func(*topopb.Node) (Implementation, error)

// Resetter provides Reset interface to nodes.
type Resetter interface {
	ResetCfg(ctx context.Context, ni Interface) error
}

func (n *Node) ResetCfg(ctx context.Context) error {
	r, ok := n.impl.(Resetter)
	if !ok {
		return fmt.Errorf("%T is not a Resetter", n.impl)
	}
	return r.ResetCfg(ctx, n)
}

var (
	mu        sync.Mutex
	nodeTypes = map[topopb.Node_Type]NewNodeFn{}
)

func Register(t topopb.Node_Type, fn NewNodeFn) {
	mu.Lock()
	if _, ok := nodeTypes[t]; ok {
		panic(fmt.Sprintf("duplicate registration for %T", t))
	}
	nodeTypes[t] = fn
	mu.Unlock()
}

type NodeStatus struct {
	Status string
	Reason string
}

// Node is a topology node in the cluster.
type Node struct {
	impl       Implementation
	namespace  string
	kClient    kubernetes.Interface
	rCfg       *rest.Config
	interfaces map[string]*Link
}

// Interfaces returns the node's map of interfaces.
func (n *Node) Interfaces() map[string]*Link {
	return n.interfaces
}

// KubeClient returns the node's kubeclient.
func (n *Node) KubeClient() kubernetes.Interface {
	return n.kClient
}

// RESTConfig returns the node's REST configuration.
func (n *Node) RESTConfig() *rest.Config {
	return n.rCfg
}

// Namespace returns the node's namespace.
func (n *Node) Namespace() string {
	return n.namespace
}

// Impl returns the node implementation.
func (n *Node) Impl() Implementation {
	return n.impl
}

// New creates a new node for use in the k8s cluster.  Configure will push the node to
// the cluster.
func New(namespace string, pb *topopb.Node, kClient kubernetes.Interface, rCfg *rest.Config) (*Node, error) {
	impl, err := getImpl(pb)
	if err != nil {
		return nil, err
	}
	return &Node{
		namespace:  namespace,
		impl:       impl,
		rCfg:       rCfg,
		kClient:    kClient,
		interfaces: map[string]*Link{},
	}, nil
}

// Configure creates the node on the k8s cluster.
func (n *Node) Configure(ctx context.Context, bp string) error {
	pb := n.impl.Proto()
	var data []byte
	switch v := pb.Config.GetConfigData().(type) {
	case *topopb.Config_File:
		var err error
		data, err = ioutil.ReadFile(filepath.Join(bp, v.File))
		if err != nil {
			return err
		}
	case *topopb.Config_Data:
		data = v.Data
	}
	if data != nil {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("%s-config", pb.Name),
			},
			Data: map[string]string{
				pb.Config.ConfigFile: string(data),
			},
		}
		sCM, err := n.kClient.CoreV1().ConfigMaps(n.namespace).Create(ctx, cm, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		log.Infof("Server Config Map:\n%v\n", sCM)
	}
	return nil
}

// Delete removes the Node from the cluster.
func (n *Node) Delete(ctx context.Context) error {
	return n.kClient.CoreV1().ConfigMaps(n.namespace).Delete(ctx, fmt.Sprintf("%s-config", n.Name()), metav1.DeleteOptions{})
}

const (
	InitContainerName = "networkop/init-wait:latest"
)

func ToEnvVar(kv map[string]string) []corev1.EnvVar {
	var envVar []corev1.EnvVar
	for k, v := range kv {
		envVar = append(envVar, corev1.EnvVar{
			Name:  k,
			Value: v,
		})
	}
	return envVar
}

func ToResourceRequirements(kv map[string]string) corev1.ResourceRequirements {
	r := corev1.ResourceRequirements{
		Requests: map[corev1.ResourceName]resource.Quantity{},
	}
	if v, ok := kv["cpu"]; ok {
		r.Requests["cpu"] = resource.MustParse(v)
	}
	if v, ok := kv["memory"]; ok {
		r.Requests["memory"] = resource.MustParse(v)
	}
	return r
}

// CreateResource creates the node specific resources.
func (n *Node) CreateResource(ctx context.Context) error {
	pb := n.impl.Proto()
	log.Infof("Creating Resource for Pod:\n %+v", pb)
	err := n.impl.CreateNodeResource(ctx, n)
	switch status.Code(err) {
	case codes.OK:
		return nil
	case codes.Unimplemented:
	default:
		return err
	}
	return n.CreatePod(ctx)
}

// CreatePod creates the pod for the node.
func (n *Node) CreatePod(ctx context.Context) error {
	pb := n.impl.Proto()
	log.Infof("Creating Pod:\n %+v", pb)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: pb.Name,
			Labels: map[string]string{
				"app":  pb.Name,
				"topo": n.namespace,
			},
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{
				Name:  fmt.Sprintf("init-%s", pb.Name),
				Image: InitContainerName,
				Args: []string{
					fmt.Sprintf("%d", len(n.Interfaces())+1),
					fmt.Sprintf("%d", pb.Config.Sleep),
				},
				ImagePullPolicy: "IfNotPresent",
			}},
			Containers: []corev1.Container{{
				Name:            pb.Name,
				Image:           pb.Config.Image,
				Command:         pb.Config.Command,
				Args:            pb.Config.Args,
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
	sPod, err := n.kClient.CoreV1().Pods(n.namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create pod for %q: %w", pb.Name, err)
	}
	log.Debugf("Pod created:\n%+v\n", sPod)
	return nil
}

// CreateService add the service definition for the Node.
func (n *Node) CreateService(ctx context.Context) error {
	pb := n.impl.Proto()
	var servicePorts []corev1.ServicePort
	if len(pb.Services) == 0 {
		return nil
	}
	for k, v := range pb.Services {
		name := v.Name
		if name == "" {
			name = fmt.Sprintf("port-%d", k)
		}
		sp := corev1.ServicePort{
			Name:       name,
			Protocol:   "TCP",
			Port:       int32(v.Inside),
			NodePort:   int32(v.NodePort),
			TargetPort: intstr.FromInt(int(v.Inside)),
		}
		if v.Outside != 0 {
			sp.TargetPort = intstr.FromInt(int(v.Outside))
		}
		servicePorts = append(servicePorts, sp)
	}
	s := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("service-%s", pb.Name),
			Labels: map[string]string{
				"pod": pb.Name,
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: servicePorts,
			Selector: map[string]string{
				"app": pb.Name,
			},
			Type: "LoadBalancer",
		},
	}
	sS, err := n.kClient.CoreV1().Services(n.namespace).Create(ctx, s, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	log.Infof("Created Service:\n%v\n", sS)
	return nil
}

// DeleteService removes the service definition for the Node.
func (n *Node) DeleteService(ctx context.Context) error {
	i := int64(0)
	return n.kClient.CoreV1().Services(n.namespace).Delete(ctx, fmt.Sprintf("service-%s", n.Name()), metav1.DeleteOptions{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
		},
		GracePeriodSeconds: &i,
	})
}

// DeleteResource removes the resource definition for the Node.
func (n *Node) DeleteResource(ctx context.Context) error {
	pb := n.impl.Proto()
	log.Infof("Deleting Resource for Pod:\n %+v", pb)
	err := n.impl.DeleteNodeResource(ctx, n)
	switch status.Code(err) {
	case codes.OK:
		return nil
	case codes.Unimplemented:
	default:
		return err
	}
	return n.kClient.CoreV1().Pods(n.namespace).Delete(ctx, n.Name(), metav1.DeleteOptions{})
}

// Exec will make a connection via spdy transport to the Pod and execute the provided command.
// It will wire up stdin, stdout, stderr to provided io channels.
func (n *Node) Exec(ctx context.Context, cmd []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	req := n.kClient.CoreV1().RESTClient().Post().Resource("pods").Name(n.Name()).Namespace(n.namespace).SubResource("exec")
	opts := &corev1.PodExecOptions{
		Command:   cmd,
		Container: n.Name(),
		Stdin:     true,
		Stdout:    true,
		Stderr:    true,
		TTY:       true,
	}
	if stdin == nil {
		opts.Stdin = false
	}
	req.VersionedParams(
		opts,
		scheme.ParameterCodec,
	)

	exec, err := remotecommand.NewSPDYExecutor(n.rCfg, "POST", req.URL())
	if err != nil {
		return err
	}
	log.Infof("Execing %s on %s", cmd, n.Name())
	return exec.Stream(remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
	})
}

// Status returns the current pod state for Node.
func (n *Node) Status(ctx context.Context) (corev1.PodPhase, error) {
	p, err := n.Pod(ctx)
	if err != nil {
		return corev1.PodUnknown, err
	}
	return p.Status.Phase, nil
}

// Name returns the name of the node.
func (n *Node) Name() string {
	return n.impl.Proto().Name
}

// Pod returns the pod definition for the node.
func (n *Node) Pod(ctx context.Context) (*corev1.Pod, error) {
	return n.kClient.CoreV1().Pods(n.namespace).Get(ctx, n.Name(), metav1.GetOptions{})
}

var (
	remountSys = []string{
		"bin/sh",
		"-c",
		"mount -o ro,remount /sys; mount -o rw,remount /sys",
	}
	getBridge = []string{
		"bin/sh",
		"-c",
		"ls /sys/class/net/ | grep br-",
	}
	enableIPForwarding = []string{
		"bin/sh",
		"-c",
		"sysctl -w net.ipv4.ip_forward=1",
	}
)

func enableLLDP(b string) []string {
	return []string{
		"bin/sh",
		"-c",
		fmt.Sprintf("echo 16396 > /sys/class/net/%s/bridge/group_fwd_mask", b),
	}
}

// EnableLLDP enables LLDP on the pod.
func (n *Node) EnableLLDP(ctx context.Context) error {
	log.Infof("Enabling LLDP on node: %s", n.Name())
	stdout := bytes.NewBuffer([]byte{})
	stderr := bytes.NewBuffer([]byte{})
	if err := n.Exec(ctx, remountSys, nil, stdout, stderr); err != nil {
		return err
	}
	log.Infof("stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	stdout.Reset()
	stderr.Reset()
	if err := n.Exec(ctx, getBridge, nil, stdout, stderr); err != nil {
		return err
	}
	bridges := strings.Split(stdout.String(), "\n")
	for _, b := range bridges {
		stdout.Reset()
		stderr.Reset()
		cmd := enableLLDP(b)
		if err := n.Exec(ctx, cmd, nil, stdout, stderr); err != nil {
			return err
		}
		log.Infof("stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	return nil
}

// EnableIPForwarding enables IP forwarding on the pod.
func (n *Node) EnableIPForwarding(ctx context.Context) error {
	log.Infof("Enabling IP forwarding for node: %s", n.Name())
	stdout := bytes.NewBuffer([]byte{})
	stderr := bytes.NewBuffer([]byte{})
	if err := n.Exec(ctx, enableIPForwarding, nil, stdout, stderr); err != nil {
		return err
	}
	log.Infof("stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	return nil
}

type ConfigPusher interface {
	ConfigPush(context.Context, string, io.Reader) error
}

func (n *Node) ConfigPush(ctx context.Context, r io.Reader) error {
	cp, ok := n.impl.(ConfigPusher)
	if !ok {
		return fmt.Errorf("%T is not a ConfigPusher", n.impl)
	}
	return cp.ConfigPush(ctx, n.namespace, r)
}

func getImpl(pb *topopb.Node) (Implementation, error) {
	mu.Lock()
	defer mu.Unlock()
	fn, ok := nodeTypes[pb.Type]
	if !ok {
		return nil, fmt.Errorf("impl not found: %v", pb.Type)
	}
	return fn(pb)
}

type Link struct {
	UID   int
	Proto *topopb.Link
}

var (
	muPort   sync.Mutex
	nextPort uint32 = 30001
)

func GetNextPort() uint32 {
	muPort.Lock()
	defer muPort.Unlock()
	p := nextPort
	nextPort++
	return p
}

func FixServices(pb *topopb.Node) {
	for k := range pb.Services {
		if pb.Services[k].NodePort == 0 {
			pb.Services[k].NodePort = GetNextPort()
		}
	}
}
