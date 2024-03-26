package mutations

import (
	"testing"

	"google3/third_party/golang/cmp/cmp"
	"google3/third_party/golang/k8s_io/apimachinery/v/v0_23/pkg/apis/meta/v1/v1"

	corev1 "google3/third_party/golang/k8s_io/api/v/v0_23/core/v1/v1"
)

func fakeMutation(p *corev1.Pod) (*corev1.Pod, error) {
	if p.Labels == nil {
		p.Labels = make(map[string]string)
	}
	p.Labels["fake"] = "fake"

	return p, nil
}

func TestMutatePod(t *testing.T) {
	tests := []struct {
		name      string
		inPod     *corev1.Pod
		wantPatch []byte
	}{
		{
			name: "basic-pod",
			inPod: &corev1.Pod{
				ObjectMeta: v1.ObjectMeta{
					Name: "basic-pod",
				},
			},
			wantPatch: []byte(`[{"value":{"fake":"fake"},"op":"add","path":"/metadata/labels"},{"value":false,"op":"add","path":"/spec/automountServiceAccountToken"}]`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Mutor{
				mutationFuncs: []mutationFunc{fakeMutation},
			}

			patch, err := m.MutatePod(tt.inPod)
			if err != nil {
				t.Fatalf("MutatePod returned error: %v", err)
			}

			if diff := cmp.Diff(patch, tt.wantPatch); diff != "" {
				t.Errorf("MutatePod returned diff (-got, +want):\n%s", diff)
			}
		})
	}
}
