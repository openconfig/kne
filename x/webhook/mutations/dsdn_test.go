package mutations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"google3/testing/gobase/runfilestest"
	"google3/third_party/golang/cmp/cmp"
	corev1 "google3/third_party/golang/k8s_io/api/v/v0_23/core/v1/v1"
)

const (
	basePath = "google3/net/sdn/decentralized/kne/webhook/mutations/testdata/"
)

func parsePod(t *testing.T, name string) *corev1.Pod {
	pod := runfilestest.Path(t, filepath.Join(basePath, name))
	buf, err := os.ReadFile(pod)
	if err != nil {
		t.Fatal(err)
	}

	p := &corev1.Pod{}
	if err := json.Unmarshal(buf, p); err != nil {
		t.Fatal(err)
	}

	return p
}

func TestDSDNContainer(t *testing.T) {
	tests := []struct {
		name      string
		inPod     *corev1.Pod
		wantPod   *corev1.Pod
		wantError bool
	}{
		{
			name:      "empty pod",
			inPod:     &corev1.Pod{},
			wantPod:   nil,
			wantError: true,
		},
		{
			name:      "valid pod",
			inPod:     parsePod(t, "ceos.json"),
			wantPod:   parsePod(t, "dsdn-out.json"),
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPod, err := addDSDNContainers(tt.inPod)
			if tt.wantError != (err != nil) {
				t.Fatalf("addDSDNContainer(origPod) returned unexpected error. want: %t, got %t, error: %v", tt.wantError, err != nil, err)
			}

			if diff := cmp.Diff(gotPod, tt.wantPod); diff != "" {
				t.Errorf("addDSDNContainer(origPod) returned diff (-got, +want):\n%s", diff)
			}
		})
	}

}
