package mutations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
)

func parsePod(t *testing.T, name string) *corev1.Pod {
	t.Helper()
	buf, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}

	p := &corev1.Pod{}
	if err := json.Unmarshal(buf, p); err != nil {
		t.Fatal(err)
	}

	return p
}

func TestContainer(t *testing.T) {
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
			inPod:     parsePod(t, "in.json"),
			wantPod:   parsePod(t, "out.json"),
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPod, err := addContainer(tt.inPod)
			if tt.wantError != (err != nil) {
				t.Fatalf("addContainer() returned unexpected error. want: %t, got %t, error: %v", tt.wantError, err != nil, err)
			}

			if diff := cmp.Diff(gotPod, tt.wantPod); diff != "" {
				t.Errorf("addContainer() returned diff (-got, +want):\n%s", diff)
			}
		})
	}

}
