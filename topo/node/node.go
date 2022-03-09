package node

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"path/filepath"
	"sync"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/utils/pointer"

	topologyv1 "github.com/google/kne/api/types/v1beta1"
	tpb "github.com/google/kne/proto/topo"
)

type Interface interface {
	Name() string
	GetNamespace() string
	GetProto() *tpb.Node
}

type Implementation interface {
	// TopologySpecs provides a custom implementation for providing
	// one or more meshnet resource spec for a node type
	TopologySpecs(context.Context) ([]*topologyv1.Topology, error)
	// Create provides a custom implementation of pod creation
	// for a node type. Requires context, Kubernetes client interface and namespace.
	Create(context.Context) error
	// Status provides a custom implementation of accessing vendor node status.
	// Requires context, Kubernetes client interface and namespace.
	Status(context.Context) (NodeStatus, error)
	// Delete provides a custom implementation of pod creation
	// for a node type. Requires context, Kubernetes client interface and namespace.
	Delete(context.Context) error
	Pods(context.Context) ([]*corev1.Pod, error)
	Services(context.Context) ([]*corev1.Service, error)
}

// Certer provides an interface for working with certs on nodes.
type Certer interface {
	GenerateSelfSigned(context.Context) error
}

// ConfigPusher provides an interface for performing config pushes to the node.
type ConfigPusher interface {
	ConfigPush(context.Context, io.Reader) error
}

// Resetter provides Reset interface to nodes.
type Resetter interface {
	ResetCfg(ctx context.Context) error
}

// Node is the base interface for all node implementations in KNE.
type Node interface {
	Interface
	Implementation
}

type NodeStatus string

const (
	NODE_PENDING NodeStatus = "PENDING"
	NODE_RUNNING NodeStatus = "RUNNING"
	NODE_FAILED  NodeStatus = "FAILED"
	NODE_UNKNOWN NodeStatus = "UNKNOWN"
)

type NewNodeFn func(n *Impl) (Node, error)

var (
	mu          sync.Mutex
	nodeTypes   = map[tpb.Node_Type]NewNodeFn{}
	vendorTypes = map[tpb.Vendor]NewNodeFn{}
)

// Register registers the node type with the topology manager.
func Register(t tpb.Node_Type, fn NewNodeFn) {
	mu.Lock()
	if _, ok := nodeTypes[t]; ok {
		panic(fmt.Sprintf("duplicate registration for %T", t))
	}
	nodeTypes[t] = fn
	mu.Unlock()
}

// Vendor registers the vendor type with the topology manager.
func Vendor(v tpb.Vendor, fn NewNodeFn) {
	mu.Lock()
	if _, ok := vendorTypes[v]; ok {
		panic(fmt.Sprintf("duplicate registration for %T", v))
	}
	vendorTypes[v] = fn
	mu.Unlock()
}

// Impl is a topology node in the cluster.
type Impl struct {
	Namespace  string
	KubeClient kubernetes.Interface
	RestConfig *rest.Config
	Proto      *tpb.Node
	BasePath   string
	Kubecfg    string
}

// New creates a new node for use in the k8s cluster.  Configure will push the node to
// the cluster.
func New(namespace string, pb *tpb.Node, kClient kubernetes.Interface, rCfg *rest.Config, bp, kubecfg string) (Node, error) {
	return getImpl(&Impl{
		Namespace:  namespace,
		Proto:      pb,
		KubeClient: kClient,
		RestConfig: rCfg,
		BasePath:   bp,
		Kubecfg:    kubecfg,
	})
}

func (n *Impl) GetProto() *tpb.Node {
	return n.Proto
}

func (n *Impl) GetNamespace() string {
	return n.Namespace
}

func (n *Impl) TopologySpecs(context.Context) ([]*topologyv1.Topology, error) {
	proto := n.GetProto()

	t := topologyv1.Topology{
		ObjectMeta: metav1.ObjectMeta{
			Name: proto.Name,
		},
		Spec: topologyv1.TopologySpec{
			Links: make([]topologyv1.Link, len(proto.Interfaces)),
		},
	}

	i := 0
	for ifcName, ifc := range proto.Interfaces {
		link := &t.Spec.Links[i]
		link.UID = int(ifc.Uid)
		link.LocalIntf = ifcName
		link.PeerIntf = ifc.PeerIntName
		link.PeerPod = ifc.PeerName
		link.LocalIP = ""
		link.PeerIP = ""
		i++
	}

	// by default each node will result in exactly one topology resource
	// with multiple links
	return []*topologyv1.Topology{&t}, nil
}

const (
	defaultInitContainerImage = "networkop/init-wait:latest"
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

// Create will create the node in the k8s cluster with all services and config
// maps.
func (n *Impl) Create(ctx context.Context) error {
	if err := n.CreateConfig(ctx); err != nil {
		return fmt.Errorf("node %s failed to create config-map %w", n.Name(), err)
	}
	if err := n.CreatePod(ctx); err != nil {
		return fmt.Errorf("node %s failed to create pod %w", n.Name(), err)
	}
	if err := n.CreateService(ctx); err != nil {
		return fmt.Errorf("node %s failed to create service %w", n.Name(), err)
	}
	return nil
}

// CreateConfig creates a boot config for the node based on the underlying proto.
func (n *Impl) CreateConfig(ctx context.Context) error {
	pb := n.Proto
	var data []byte
	switch v := pb.Config.GetConfigData().(type) {
	case *tpb.Config_File:
		var err error
		data, err = ioutil.ReadFile(filepath.Join(n.BasePath, v.File))
		if err != nil {
			return err
		}
	case *tpb.Config_Data:
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
		sCM, err := n.KubeClient.CoreV1().ConfigMaps(n.Namespace).Create(ctx, cm, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		log.Infof("Server Config Map:\n%v\n", sCM)
	}
	return nil
}

// CreatePod creates a Pod for the Node based on the underlying proto.
func (n *Impl) CreatePod(ctx context.Context) error {
	pb := n.Proto
	log.Infof("Creating Pod:\n %+v", pb)
	initContainerImage := pb.Config.InitImage
	if initContainerImage == "" {
		initContainerImage = defaultInitContainerImage
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
	sPod, err := n.KubeClient.CoreV1().Pods(n.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	log.Debugf("Pod created:\n%+v\n", sPod)
	return nil
}

// CreateService creates services for the node based on the underlying proto.
func (n *Impl) CreateService(ctx context.Context) error {
	var servicePorts []corev1.ServicePort
	if len(n.Proto.Services) == 0 {
		log.Info("no services found")
		return nil
	}
	for k, v := range n.Proto.Services {
		name := v.Name
		if name == "" {
			name = fmt.Sprintf("port-%d", k)
		}
		sp := corev1.ServicePort{
			Name:       name,
			Protocol:   "TCP",
			Port:       int32(v.Inside),
			TargetPort: intstr.FromInt(int(v.Inside)),
		}
		if v.NodePort != 0 {
			sp.NodePort = int32(v.NodePort)
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
			Name: fmt.Sprintf("service-%s", n.Name()),
			Labels: map[string]string{
				"pod": n.Name(),
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: servicePorts,
			Selector: map[string]string{
				"app": n.Name(),
			},
			Type: "LoadBalancer",
		},
	}
	sS, err := n.KubeClient.CoreV1().Services(n.Namespace).Create(ctx, s, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	log.Infof("Created Service:\n%v\n", sS)
	return nil
}

// Delete remove the node from the cluster.
func (n *Impl) Delete(ctx context.Context) error {
	// Delete config maps for node
	if err := n.DeleteConfig(ctx); err != nil {
		log.Warnf("Error deleting config-map %q: %v", n.Name(), err)
	}
	if err := n.DeleteService(ctx); err != nil {
		log.Warnf("Error deleting service %q: %v", n.Name(), err)
	}
	// Delete Resource for node
	if err := n.DeleteResource(ctx); err != nil {
		log.Warnf("Error deleting resource %q: %v", n.Name(), err)
	}
	return nil
}

// DeleteConfig removes the node configmap from the cluster.
func (n *Impl) DeleteConfig(ctx context.Context) error {
	return n.KubeClient.CoreV1().ConfigMaps(n.Namespace).Delete(ctx, fmt.Sprintf("%s-config", n.Name()), metav1.DeleteOptions{})
}

// DeleteService removes the service definition for the Node.
func (n *Impl) DeleteService(ctx context.Context) error {
	return n.KubeClient.CoreV1().Services(n.Namespace).Delete(ctx, fmt.Sprintf("service-%s", n.Name()), metav1.DeleteOptions{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
		},
		GracePeriodSeconds: pointer.Int64(0),
	})
}

// DeleteResource removes the resource definition for the Node.
func (n *Impl) DeleteResource(ctx context.Context) error {
	log.Infof("Deleting Resource for Pod:%s", n.Name())
	return n.KubeClient.CoreV1().Pods(n.Namespace).Delete(ctx, n.Name(), metav1.DeleteOptions{})
}

// Exec will make a connection via spdy transport to the Pod and execute the provided command.
// It will wire up stdin, stdout, stderr to provided io channels.
func (n *Impl) Exec(ctx context.Context, cmd []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	req := n.KubeClient.CoreV1().RESTClient().Post().Resource("pods").Name(n.Name()).Namespace(n.Namespace).SubResource("exec")
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

	exec, err := remotecommand.NewSPDYExecutor(n.RestConfig, "POST", req.URL())
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

// Status returns the current node state.
func (n *Impl) Status(ctx context.Context) (NodeStatus, error) {
	p, err := n.Pods(ctx)
	if err != nil {
		return NODE_UNKNOWN, err
	}
	if len(p) != 1 {
		return NODE_UNKNOWN, fmt.Errorf("expected exactly one pod for node %s", n.Name())
	}
	switch p[0].Status.Phase {
	case corev1.PodFailed:
		return NODE_FAILED, nil
	case corev1.PodRunning:
		return NODE_RUNNING, nil
	case corev1.PodPending:
		fallthrough
	default:
		return NODE_PENDING, nil
	}
}

// Name returns the name of the node.
func (n *Impl) Name() string {
	return n.Proto.Name
}

// Pod returns the pod definition for the node.
func (n *Impl) Pods(ctx context.Context) ([]*corev1.Pod, error) {
	p, err := n.KubeClient.CoreV1().Pods(n.Namespace).Get(ctx, n.Name(), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return []*corev1.Pod{p}, nil
}

// Service returns the service definition for the node.
func (n *Impl) Services(ctx context.Context) ([]*corev1.Service, error) {
	s, err := n.KubeClient.CoreV1().Services(n.Namespace).Get(ctx, fmt.Sprintf("service-%s", n.Name()), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return []*corev1.Service{s}, nil
}

func getImpl(impl *Impl) (Node, error) {
	mu.Lock()
	defer mu.Unlock()
	if impl == nil {
		return nil, fmt.Errorf("impl cannot be nil")
	}
	if impl.Proto == nil {
		return nil, fmt.Errorf("impl.Proto cannot be nil")
	}
	fn, ok := vendorTypes[impl.Proto.Vendor]
	if ok {
		return fn(impl)
	}
	// TODO(hines): Remove once type is deprecated.
	fn, ok = nodeTypes[impl.Proto.Type]
	if !ok {
		return nil, fmt.Errorf("impl not found: %v", impl.Proto.Type)
	}
	return fn(impl)
}
