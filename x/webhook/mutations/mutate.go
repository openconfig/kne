// Package mutations contains individual mutation to apply to a pod config.
package mutations

import (
	"encoding/json"

	"google3/third_party/golang/github_com/wI2L/jsondiff/v/v0/jsondiff"
	corev1 "google3/third_party/golang/k8s_io/api/v/v0_23/core/v1/v1"
	"google3/third_party/golang/klog_v2/klog"
	"google3/third_party/golang/protobuf/v2/proto/proto"
)

type mutationFunc func(*corev1.Pod) (*corev1.Pod, error)

// Mutor contains a set of mutation functions.
type Mutor struct {
	mutationFuncs []mutationFunc
}

// New build a new Mutor
func New() *Mutor {
	return &Mutor{
		mutationFuncs: []mutationFunc{addDSDNContainers},
	}
}

// MutatePod applies all mutations to a copy of the provided pod. It returns a json patch
// (rfc6902).
func (m *Mutor) MutatePod(pod *corev1.Pod) ([]byte, error) {
	klog.Infof("Mutating pod %s", pod.Name)

	// list of all mutations to be applied to the pod

	mpod := pod.DeepCopy()

	// apply all mutations
	for _, mutation := range m.mutationFuncs {
		var err error
		mpod, err = mutation(mpod)
		if err != nil {
			return nil, err
		}
	}

	mpod.Spec.AutomountServiceAccountToken = proto.Bool(false)

	// generate json patch
	patch, err := jsondiff.Compare(pod, mpod)
	if err != nil {
		return nil, err
	}

	patchb, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}

	return patchb, nil
}
