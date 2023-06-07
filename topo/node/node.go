package node

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	topologyv1 "github.com/networkop/meshnet-cni/api/types/v1beta1"
	tpb "github.com/openconfig/kne/proto/topo"
	scraplinetwork "github.com/scrapli/scrapligo/driver/network"
	scrapliopts "github.com/scrapli/scrapligo/driver/options"
	scraplilogging "github.com/scrapli/scrapligo/logging"
	scrapliplatform "github.com/scrapli/scrapligo/platform"
	scrapliutil "github.com/scrapli/scrapligo/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	log "k8s.io/klog/v2"
	"k8s.io/utils/pointer"
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
	Status(context.Context) (Status, error)
	// Delete provides a custom implementation of pod creation
	// for a node type. Requires context, Kubernetes client interface and namespace.
	Delete(context.Context) error
	// Pods provides a custom implementation for querying all pods created for
	// for a node. Requires context, Kubernetes client interface and namespace.
	Pods(context.Context) ([]*corev1.Pod, error)
	// Services provides a custom implementation for querying all services created for
	// for a node. Requires context, Kubernetes client interface and namespace.
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

type Status string

const (
	StatusPending Status = "PENDING"
	StatusRunning Status = "RUNNING"
	StatusFailed  Status = "FAILED"
	StatusUnknown Status = "UNKNOWN"

	ConfigVolumeName = "startup-config-volume"

	OndatraRoleLabel = "ondatra-role"
	OndatraRoleDUT   = "DUT"
	OndatraRoleATE   = "ATE"
)

type NewNodeFn func(n *Impl) (Node, error)

var (
	mu          sync.Mutex
	vendorTypes = map[tpb.Vendor]NewNodeFn{}
	tempCfgDir  = "/tmp/kne"
)

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

	var links []topologyv1.Link
	for ifcName, ifc := range proto.Interfaces {
		if ifc.PeerIntName == "" {
			return nil, fmt.Errorf("interface %q PeerIntName canot be empty", ifcName)
		}
		if ifc.PeerName == "" {
			return nil, fmt.Errorf("interface %q PeerName canot be empty", ifcName)
		}
		links = append(links, topologyv1.Link{
			UID:       int(ifc.Uid),
			LocalIntf: ifcName,
			PeerIntf:  ifc.PeerIntName,
			PeerPod:   ifc.PeerName,
			LocalIP:   "",
			PeerIP:    "",
		})
	}

	// by default each node will result in exactly one topology resource
	// with multiple links
	return []*topologyv1.Topology{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: proto.Name,
			},
			Spec: topologyv1.TopologySpec{
				Links: links,
			},
		},
	}, nil
}

const (
	DefaultInitContainerImage = "us-west1-docker.pkg.dev/kne-external/kne/networkop/init-wait:ga"
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
	if err := n.CreatePod(ctx); err != nil {
		return fmt.Errorf("node %s failed to create pod %w", n.Name(), err)
	}
	if err := n.CreateService(ctx); err != nil {
		return fmt.Errorf("node %s failed to create service %w", n.Name(), err)
	}
	return nil
}

func (n *Impl) readConfig() ([]byte, error) {
	switch v := n.Proto.Config.GetConfigData().(type) {
	case *tpb.Config_File:
		return os.ReadFile(filepath.Join(n.BasePath, v.File))
	case *tpb.Config_Data:
		return v.Data, nil
	case nil:
		return nil, nil
	default:
		return nil, fmt.Errorf("config type not supported: %T", v)
	}
}

// CreateConfig creates a boot config for the node based on the underlying proto.
// A volume containing the boot config is returned. If the config size is <3MB
// then a ConfigMap is created as the volume source. Else a temporary file
// is written with the boot config to serve as a HostPath volume source.
func (n *Impl) CreateConfig(ctx context.Context) (*corev1.Volume, error) {
	data, err := n.readConfig()
	if err != nil {
		return nil, err
	}

	var vs corev1.VolumeSource
	switch size := len(data); {
	case size == 0: // no config data in proto, return a nil volume
		return nil, nil
	case size < 1048576*3: // size less than 3MB, use configMap
		name := fmt.Sprintf("%s-config", n.Proto.Name)
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Data: map[string]string{
				n.Proto.Config.ConfigFile: string(data),
			},
		}
		sCM, err := n.KubeClient.CoreV1().ConfigMaps(n.Namespace).Create(ctx, cm, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
		log.V(1).Infof("Created config ConfigMap:\n%v\n", sCM)
		vs = corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: name,
				},
			},
		}
	default: // size greater than 3MB, use hostPath
		if err := os.MkdirAll(tempCfgDir, 0666); err != nil {
			return nil, err
		}
		f, err := os.CreateTemp(tempCfgDir, fmt.Sprintf("kne-%s-config-*.cfg", n.Proto.Name))
		if err != nil {
			return nil, err
		}
		path := f.Name()
		if _, err := f.Write(data); err != nil {
			os.Remove(path)
			return nil, err
		}
		if err := f.Chmod(0444); err != nil {
			os.Remove(path)
			return nil, err
		}
		if err := f.Close(); err != nil {
			os.Remove(path)
			return nil, err
		}
		log.V(1).Infof("Created config file %s", path)
		vs = corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: path,
			},
		}
	}
	return &corev1.Volume{
		Name:         ConfigVolumeName,
		VolumeSource: vs,
	}, nil
}

// CreatePod creates a Pod for the Node based on the underlying proto.
func (n *Impl) CreatePod(ctx context.Context) error {
	pb := n.Proto
	log.Infof("Creating Pod:\n %+v", pb)
	initContainerImage := pb.Config.InitImage
	if initContainerImage == "" {
		initContainerImage = DefaultInitContainerImage
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
		vol, err := n.CreateConfig(ctx)
		if err != nil {
			return err
		}
		pod.Spec.Volumes = append(pod.Spec.Volumes, *vol)
		vm := corev1.VolumeMount{
			Name:      ConfigVolumeName,
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
		return err
	}
	log.V(1).Infof("Pod created:\n%+v\n", sPod)
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
		if v.Outside != 0 {
			log.Warningf("Outside should not be set by user. The key is used as the target external port")
		}
		sp := corev1.ServicePort{
			Name:       name,
			Protocol:   "TCP",
			Port:       int32(k),
			TargetPort: intstr.FromInt(int(v.Inside)),
		}
		if v.NodePort != 0 {
			sp.NodePort = int32(v.NodePort)
		}
		if v.Outside != 0 {
			sp.Port = int32(v.Outside)
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
	log.V(1).Infof("Created Service:\n%v\n", sS)
	return nil
}

// Delete remove the node from the cluster.
func (n *Impl) Delete(ctx context.Context) error {
	if err := n.DeleteService(ctx); err != nil {
		log.Warningf("Error deleting service %q: %v", n.Name(), err)
	}
	// Delete Resource for node
	if err := n.DeleteResource(ctx); err != nil {
		log.Warningf("Error deleting resource %q: %v", n.Name(), err)
	}
	return nil
}

// DeleteConfig removes the node configmap from the cluster if it exists. If
// a config file hostPath was used for the boot config volume, clean the file up instead.
func (n *Impl) DeleteConfig(ctx context.Context) error {
	pod, err := n.KubeClient.CoreV1().Pods(n.Namespace).Get(ctx, n.Name(), metav1.GetOptions{})
	if err != nil {
		return err
	}
	for _, vol := range pod.Spec.Volumes {
		if vol.Name != ConfigVolumeName {
			continue
		}
		switch vs := vol.VolumeSource; {
		case vs.HostPath != nil:
			path := vs.HostPath.Path
			if err := os.Remove(path); err != nil {
				return err
			}
			log.V(1).Infof("Deleted config file %s", path)
		case vs.ConfigMap != nil:
			name := vs.ConfigMap.LocalObjectReference.Name
			if err := n.KubeClient.CoreV1().ConfigMaps(n.Namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
				return err
			}
			log.V(1).Infof("Deleted config map %s", name)
		}
	}
	return nil
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
	if err := n.DeleteConfig(ctx); err != nil {
		return err
	}
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
func (n *Impl) Status(ctx context.Context) (Status, error) {
	p, err := n.Pods(ctx)
	if err != nil {
		return StatusUnknown, err
	}
	if len(p) != 1 {
		return StatusUnknown, fmt.Errorf("expected exactly one pod for node %s", n.Name())
	}
	switch p[0].Status.Phase {
	case corev1.PodFailed:
		return StatusFailed, nil
	case corev1.PodRunning:
		for _, cond := range p[0].Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
				return StatusRunning, nil
			}
		}
	}
	return StatusPending, nil
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
		return nil, fmt.Errorf("node implementation cannot be nil")
	}
	if impl.Proto == nil {
		return nil, fmt.Errorf("node implementation proto cannot be nil")
	}
	//nolint:staticcheck
	if impl.Proto.Type != tpb.Node_UNKNOWN {
		log.Warningf("node.Type (%v) is a DEPRECATED field, node.Vendor is required", impl.Proto.Type)
	}
	if fn, ok := vendorTypes[impl.Proto.Vendor]; ok {
		return fn(impl)
	}
	return nil, fmt.Errorf("node implementation not found for vendor %v", impl.Proto.Vendor)
}

// PatchCLIConnOpen sets up scrapligo options to work with a tty
// provided by the combination of the bin binary, namespace and the name of the node plus cliCMd command.
// In the context of kne this command is typically `kubectl exec cliCmd`.
func (n *Impl) PatchCLIConnOpen(bin string, cliCmd []string, opts []scrapliutil.Option) []scrapliutil.Option {
	opts = append(opts, scrapliopts.WithSystemTransportOpenBin(bin))

	var args []string
	if n.Kubecfg != "" {
		args = append(args, fmt.Sprintf("--kubeconfig=%s", n.Kubecfg))
	}
	args = append(args, "exec", "-it", "-n", n.GetNamespace(), n.Name(), "--")
	args = append(args, cliCmd...)

	opts = append(opts, scrapliopts.WithSystemTransportOpenArgsOverride(args))

	return opts
}

// GetCLIConn attempts to open the transport channel towards a Network OS and perform scrapligo OnOpen actions
// for a given platform. Retries indefinitely till success and returns a scrapligo network driver instance.
func (n *Impl) GetCLIConn(platform string, opts []scrapliutil.Option) (*scraplinetwork.Driver, error) {
	if log.V(1).Enabled() {
		li, _ := scraplilogging.NewInstance(scraplilogging.WithLevel("debug"),
			scraplilogging.WithLogger(log.Info))
		opts = append(opts, scrapliopts.WithLogger(li))
	}

	for {
		p, err := scrapliplatform.NewPlatform(
			platform,
			n.Name(),
			opts...,
		)
		if err != nil {
			log.Errorf("failed to fetch platform instance for device %s; error: %+v\n", err, n.Name())
			return nil, err
		}

		d, err := p.GetNetworkDriver()
		if err != nil {
			log.Errorf("failed to create driver for device %s; error: %+v\n", err, n.Name())
			return nil, err
		}

		if err = d.Open(); err != nil {
			log.V(1).Infof("%s - Cli not ready (%s) - waiting.", n.Name(), err)
			time.Sleep(time.Second * 2)
			continue
		}

		log.V(1).Infof("%s - Cli ready.", n.Name())

		return d, nil
	}
}
