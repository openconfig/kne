package drivenets

import (
	"context"
	"testing"

	"github.com/openconfig/gnmi/errdiff"
	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name      string
		nodeImpl  *node.Impl
		wantErr   bool
		errString string
	}{
		{
			name:      "nil nodeImpl",
			nodeImpl:  nil,
			wantErr:   true,
			errString: "nodeImpl cannot be nil",
		},
		{
			name: "nil Proto",
			nodeImpl: &node.Impl{
				Proto: nil,
			},
			wantErr:   true,
			errString: "nodeImpl.Proto cannot be nil",
		},
		{
			name: "no model specified",
			nodeImpl: &node.Impl{
				Proto: &tpb.Node{},
			},
			wantErr:   true,
			errString: "unknown model",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.nodeImpl)
			if diff := errdiff.Substring(err, tt.errString); diff != "" {
				t.Errorf("New() %v", diff)
			}
		})
	}
}

func TestCdnosDefaults(t *testing.T) {
	pb := &tpb.Node{
		Name: "testNode",
	}

	pb = cdnosDefaults(pb)

	if pb.Config == nil {
		t.Errorf("Config is nil")
	}

	if pb.Config.Image == "" {
		t.Errorf("Image is empty")
	}

	if pb.Config.InitImage == "" {
		t.Errorf("InitImage is empty")
	}

	if len(pb.GetConfig().GetCommand()) == 0 {
		t.Errorf("Command is empty")
	}

	if pb.Config.EntryCommand == "" {
		t.Errorf("EntryCommand is empty")
	}

	if pb.Config.Cert == nil {
		t.Errorf("Cert is nil")
	}

	if pb.Constraints == nil {
		t.Errorf("Constraints is nil")
	}

	if pb.Constraints["cpu"] == "" {
		t.Errorf("CPU constraint is empty")
	}

	if pb.Constraints["memory"] == "" {
		t.Errorf("Memory constraint is empty")
	}

	if pb.Labels == nil {
		t.Errorf("Labels is nil")
	}

	if pb.Labels["vendor"] == "" {
		t.Errorf("Vendor label is empty")
	}

	if pb.Labels[node.OndatraRoleLabel] != node.OndatraRoleDUT {
		t.Errorf("OndatraRoleLabel is not DUT")
	}

	if pb.Services == nil {
		t.Errorf("Services is nil")
	}
}

// TestCreateConfig_NoDataButPathDefaulted verifies that an empty ConfigMap
// is still created when no startup data is supplied but cdnosDefaults has
// populated ConfigPath/ConfigFile. Without this, the controller-created Pod
// would block forever in PodInitializing on a missing ConfigMap mount.
func TestCreateConfig_NoDataButPathDefaulted(t *testing.T) {
	pb := cdnosDefaults(&tpb.Node{Name: "n1"})
	if pb.Config.GetConfigPath() == "" || pb.Config.GetConfigFile() == "" {
		t.Fatalf("preconditions: cdnosDefaults should populate ConfigPath and ConfigFile")
	}
	if pb.Config.GetConfigData() != nil {
		t.Fatalf("preconditions: cdnosDefaults should not set startup config data")
	}

	kc := fake.NewSimpleClientset()
	n := &Node{Impl: &node.Impl{
		Proto:      pb,
		KubeClient: kc,
		Namespace:  "ns1",
	}}

	vol, err := n.CreateConfig(context.Background())
	if err != nil {
		t.Fatalf("CreateConfig returned unexpected error: %v", err)
	}
	if vol == nil {
		t.Fatalf("CreateConfig returned nil volume; controller mount would fail")
	}
	cm, err := kc.CoreV1().ConfigMaps("ns1").Get(context.Background(), "n1-config", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected ConfigMap n1-config to exist: %v", err)
	}
	if _, ok := cm.Data[pb.Config.GetConfigFile()]; !ok {
		t.Errorf("ConfigMap missing key %q; have keys: %v", pb.Config.GetConfigFile(), keys(cm.Data))
	}
}

// TestCreateConfig_NoDataAndNoPath verifies the explicit "no config mount"
// path: when neither ConfigPath nor ConfigFile is set, no ConfigMap is
// created.
func TestCreateConfig_NoDataAndNoPath(t *testing.T) {
	pb := &tpb.Node{
		Name:   "n2",
		Config: &tpb.Config{}, // empty: no Path, no File, no Data
	}
	kc := fake.NewSimpleClientset()
	n := &Node{Impl: &node.Impl{
		Proto:      pb,
		KubeClient: kc,
		Namespace:  "ns1",
	}}
	vol, err := n.CreateConfig(context.Background())
	if err != nil {
		t.Fatalf("CreateConfig returned unexpected error: %v", err)
	}
	if vol != nil {
		t.Errorf("expected nil volume when no config is requested, got %+v", vol)
	}
	cms, _ := kc.CoreV1().ConfigMaps("ns1").List(context.Background(), metav1.ListOptions{})
	if len(cms.Items) != 0 {
		t.Errorf("expected zero ConfigMaps, found %d", len(cms.Items))
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// dummy var keeps corev1 imported even if the rest of the file doesn't use it.
var _ = corev1.ConfigMap{}

func TestDefaultNodeConstraints(t *testing.T) {
	n := &Node{}
	constraints := n.DefaultNodeConstraints()
	if constraints.CPU != defaultConstraints.CPU {
		t.Errorf("DefaultNodeConstraints() returned unexpected CPU: got %s, want %s", constraints.CPU, defaultConstraints.CPU)
	}

	if constraints.Memory != defaultConstraints.Memory {
		t.Errorf("DefaultNodeConstraints() returned unexpected Memory: got %s, want %s", constraints.Memory, defaultConstraints.Memory)
	}
}
