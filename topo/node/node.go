package node

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	topologyv1 "github.com/networkop/meshnet-cni/api/types/v1beta1"
	"github.com/openconfig/gnmi/errlist"
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
	String() string
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
	// BackToBackLoop returns a bool if the node supports links directly between
	// two ports on the same node.
	BackToBackLoop() bool
	// ValidateConstraints validates the host with the node's constraints.
	ValidateConstraints() error
	// DefaultNodeConstraints exports the node's default constraints.
	DefaultNodeConstraints() Constraints
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

// Constraints struct holds the values for node constraints like CPU, Memory.
// The constraints are represented as strings grok format.
// For example, CPU: "1000m" or "500m" , etc. Memory: "1Gi" or "2Gi" etc.
type Constraints struct {
	CPU    string
	Memory string
}

var (
	// Stubs for testing
	kernelConstraintValue = kernelConstraintValueImpl
)

func kernelConstraintValueImpl(constraint string) (int, error) {
	constraintData, err := os.ReadFile(convertSysctlNameToProcSysPath(constraint))
	if err != nil {
		return 0, err
	}
	kcValue, err := strconv.Atoi(strings.TrimSpace(string(constraintData)))
	if err != nil {
		return 0, fmt.Errorf("failed to convert kernel constraint data: %s error: %w", string(constraintData), err)
	}
	return kcValue, nil
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

func (n *Impl) String() string {
	return fmt.Sprintf("%q (vendor: %q, model: %q)", n.Proto.Name, n.Proto.Vendor, n.Proto.Model)
}

func (n *Impl) TopologySpecs(context.Context) ([]*topologyv1.Topology, error) {
	links, err := GetNodeLinks(n.Proto)
	if err != nil {
		return nil, err
	}

	// by default each node will result in exactly one topology resource
	// with multiple links
	return []*topologyv1.Topology{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: n.Proto.Name,
			},
			Spec: topologyv1.TopologySpec{
				Links: links,
			},
		},
	}, nil
}

const (
	DefaultInitContainerImage = "us-west1-docker.pkg.dev/kne-external/kne/init-wait:ga"
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

// ValidateConstraints will check the host constraints and returns a list of errors for cases which do not meet
// the constraints
func (n *Impl) ValidateConstraints() error {
	hostConstraints := n.GetProto().GetHostConstraints()
	if hostConstraints == nil {
		return nil
	}
	var errorList errlist.List
	for _, hc := range hostConstraints {
		switch v := hc.GetConstraint().(type) {
		case *tpb.HostConstraint_KernelConstraint:
			log.Infof("Validating %s constraint for node %s", v.KernelConstraint.GetName(), n.String())
			kcValue, err := kernelConstraintValue(v.KernelConstraint.Name)
			if err != nil {
				errorList.Add(err)
				continue
			}
			switch k := v.KernelConstraint.GetConstraintType().(type) {
			case *tpb.KernelParam_BoundedInteger:
				if err := validateBoundedInteger(k.BoundedInteger, kcValue); err != nil {
					errorList.Add(fmt.Errorf("failed to validate kernel constraint error: %w", err))
				}
			}
		default:
			return nil
		}
	}

	return errorList.Err()
}

// DefaultNodeConstraints - Returns default constraints of the node. It returns an empty struct by default.
func (n *Impl) DefaultNodeConstraints() Constraints {
	return Constraints{}
}

// validateBoundedInteger - Evaluates a constraint if is within a bound of max - min integer. It defaults any unspecified upper bound to infinity,
// the lower bound should already default to zero if not specified, validates that lower <= upper in the constraint otherwise error
func validateBoundedInteger(nodeConstraint *tpb.BoundedInteger, hostCons int) error {
	if nodeConstraint.MaxValue == 0 {
		nodeConstraint.MaxValue = math.MaxInt64
	}
	if nodeConstraint.MinValue > nodeConstraint.MaxValue {
		return fmt.Errorf("invalid bounds. Max value %d is less than min value %d", nodeConstraint.MaxValue, nodeConstraint.MinValue)
	}
	if !(nodeConstraint.MinValue <= int64(hostCons) && int64(hostCons) <= nodeConstraint.MaxValue) {
		return fmt.Errorf("invalid bounded integer constraint. min: %d max %d constraint data %d",
			nodeConstraint.MinValue, nodeConstraint.MaxValue, hostCons)
	}
	return nil
}

// convertSysctlNameToProcSysPath - Constraints need to be read from a specific path in the system which is
// prepended with "proc/sys". The full path needs to be calculated
func convertSysctlNameToProcSysPath(sysctlName string) string {
	sysctlNameParts := strings.Split(sysctlName, ".")
	procSysPath := "/proc/sys/"
	for i, part := range sysctlNameParts {
		procSysPath += part
		if i < len(sysctlNameParts)-1 {
			procSysPath += "/"
		}
	}
	return procSysPath
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
	case size < 1048576: // size less than 1MB, use configMap
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
	default: // size greater than 1MB, use hostPath
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
	links, err := GetNodeLinks(pb)
	if err != nil {
		return err
	}
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
					fmt.Sprintf("%d", len(links)+1),
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
		if v.Outside != 0 {
			log.Warningf("Outside should not be set by user. The key is used as the target external port")
		}
		nodePort := v.NodePort
		if nodePort > math.MaxUint16 {
			return fmt.Errorf("node port %d out of range (max: %d)", k, math.MaxUint16)
		}
		if k > math.MaxUint16 {
			return fmt.Errorf("service port %d out of range (max: %d)", k, math.MaxUint16)
		}
		sp := corev1.ServicePort{
			Protocol:   "TCP",
			Port:       int32(k),
			NodePort:   int32(nodePort),
			TargetPort: intstr.FromInt(int(v.Inside)),
			Name:       v.Name,
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
			// Do not allocate a NodePort for this LoadBalancer. MetalLB
			// or the equivalent load balancer should handle exposing this service.
			// Large topologies may try to allocate more NodePorts than are
			// supported in default clusters.
			// https://kubernetes.io/docs/concepts/services-networking/service/#load-balancer-nodeport-allocation
			AllocateLoadBalancerNodePorts: pointer.Bool(false),
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
	if len(n.Proto.Services) == 0 {
		return nil, nil
	}
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

// BackToBackLoop returns a bool indicating if the node supports a single link
// connecting two ports on the same node. By default this is false.
func (n *Impl) BackToBackLoop() bool {
	return false
}

func GetNodeLinks(n *tpb.Node) ([]topologyv1.Link, error) {
	var links []topologyv1.Link
	for ifcName, ifc := range n.Interfaces {
		if ifcName == "eth0" {
			log.Infof("Found mgmt interface ignoring for Meshnet: %q", ifcName)
			continue
		}
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
	return links, nil
}
