package mutations

import (
	"fmt"
	"strings"

	"google3/third_party/golang/k8s_io/apimachinery/v/v0_23/pkg/api/resource/resource"
	"google3/third_party/golang/protobuf/v2/proto/proto"

	corev1 "google3/third_party/golang/k8s_io/api/v/v0_23/core/v1/v1"
)

// Default values for cpu and memory allocations for added containers.
const (
	// TODO(alshabib) make these values configurable.
	mem = "1Gi"
	cpu = "500m"
)

var (
	pmteImage   = "us-west1-docker.pkg.dev/ggn-dsdn/integration/pmte:candidate"
	pmteCommand = []string{"./pmte_server"}
	dsdnImage   = "us-west1-docker.pkg.dev/ggn-dsdn/integration/dsdn:candidate"
	dsdnCommand = []string{"./dsdn", "launch"}
	dsdnEnvPre  = "DSDN_"
)

// addDSDNContainers adds the containers relevant to dSDN (dsdn, pmte). It also migrates any
// dSDN related environment variables to the dsdn container.
func addDSDNContainers(pod *corev1.Pod) (*corev1.Pod, error) {
	mpod := pod.DeepCopy()
	if err := addContainers(mpod); err != nil {
		return nil, err
	}
	return mpod, nil
}

func addContainers(pod *corev1.Pod) error {
	// kne only supports supplying one container
	if len(pod.Spec.Containers) != 1 {
		return fmt.Errorf("pod %s has %d containers", pod.Name, len(pod.Spec.Containers))
	}

	cpuQuantity, err := resource.ParseQuantity(cpu)
	if err != nil {
		return err
	}

	memoryQuantity, err := resource.ParseQuantity(mem)
	if err != nil {
		return err
	}

	cnt := pod.Spec.Containers[0]
	dsdnCnt := corev1.Container{
		Name:            "dsdn",
		Image:           dsdnImage,
		Command:         dsdnCommand,
		ImagePullPolicy: corev1.PullIfNotPresent,
		// TODO(b/310147781): add cmd in dsdn to check health and
		SecurityContext: &corev1.SecurityContext{
			Privileged: proto.Bool(true),
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    cpuQuantity,
				corev1.ResourceMemory: memoryQuantity,
			},
		},
	}

	pmteCnt := corev1.Container{
		Name:            "pmte",
		Image:           pmteImage,
		Command:         pmteCommand,
		ImagePullPolicy: corev1.PullIfNotPresent,
		// TODO(alshabib): add cmd in dsdn to check health and use it in StartupProbe
		SecurityContext: &corev1.SecurityContext{
			Privileged: proto.Bool(true),
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    cpuQuantity,
				corev1.ResourceMemory: memoryQuantity,
			},
		},
	}

	var dsdnEnv []corev1.EnvVar
	for _, env := range cnt.Env {
		if strings.HasPrefix(env.Name, dsdnEnvPre) {
			dsdnEnv = append(dsdnEnv, env)
		}
	}

	dsdnCnt.Env = dsdnEnv

	pod.Spec.Containers = []corev1.Container{cnt, dsdnCnt, pmteCnt}

	return nil
}
