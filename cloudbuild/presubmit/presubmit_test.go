package presubmit_test

import (
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/openconfig/ondatra"
	kinit "github.com/openconfig/ondatra/knebind/init"
)

func TestMain(m *testing.M) {
	ondatra.RunTests(m, kinit.Init)
}

func TestGNMICEOS(t *testing.T) {
	dut := ondatra.DUT(t, "ceos")
	sys := dut.Telemetry().System().Lookup(t)
	if !sys.IsPresent() {
		t.Fatalf("No System telemetry for %v", dut)
	}
}

func TestGNMICTPX(t *testing.T) {
	t.Skip("Not yet implemented")
}

func TestGNMISRL(t *testing.T) {
	t.Skip("Not yet implemented")
}

func TestGNMIXRD(t *testing.T) {
	t.Skip("Not yet implemented")
}

func TestGRIBICEOS(t *testing.T) {
	t.Skip("Not yet implemented")
}

func TestGRIBICTPX(t *testing.T) {
	t.Skip("Not yet implemented")
}

func TestGRIBISRL(t *testing.T) {
	t.Skip("Not yet implemented")
}

func TestGRIBIXRD(t *testing.T) {
	t.Skip("Not yet implemented")
}

func TestP4RTCEOS(t *testing.T) {
	t.Skip("Not yet implemented")
}

func TestP4RTCPTX(t *testing.T) {
	t.Skip("Not yet implemented")
}

func TestP4RTSRL(t *testing.T) {
	t.Skip("Not yet implemented")
}

func TestP4RTXRD(t *testing.T) {
	t.Skip("Not yet implemented")
}

func TestOTG(t *testing.T) {
	ate := ondatra.ATE(t, "otg")
	cfg := ate.OTG().NewConfig(t)
	cfg.Ports().Add().SetName("port1")
	cfg.Ports().Add().SetName("port2")
	cfg.Ports().Add().SetName("port3")
	cfg.Ports().Add().SetName("port4")
	ate.OTG().PushConfig(t, cfg)

	portNames := ate.OTG().Telemetry().PortAny().Name().Get(t)
	sort.Strings(portNames)
	if want := []string{"port1", "port2", "port3", "port4"}; !cmp.Equal(portNames, want) {
		t.Errorf("Telemetry got port names %v, want %v", portNames, want)
	}
}
