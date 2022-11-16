package presubmit_test

import (
	"context"
	"io"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	gpb "github.com/openconfig/gribi/v1/proto/service"
	"github.com/openconfig/ondatra"
	kinit "github.com/openconfig/ondatra/knebind/init"
)

func TestMain(m *testing.M) {
	ondatra.RunTests(m, kinit.Init)
}

// lookupTelemetry checks for system telemetry for a DUT.
func lookupTelemetry(t *testing.T, dut *ondatra.DUTDevice) {
	t.Helper()
	sys := dut.Telemetry().System().Lookup(t)
	if !sys.IsPresent() {
		t.Fatalf("gNMI failure: no System telemetry for %v", dut)
	}
}

// fetchAFTEntries checks for AFT entries using gRIBI for a DUT.
func fetchAFTEntries(t *testing.T, dut *ondatra.DUTDevice) {
	t.Helper()
	c := dut.RawAPIs().GRIBI().New(t)
	req := &gpb.GetRequest{
		NetworkInstance: &gpb.GetRequest_All{},
		Aft:             gpb.AFTType_ALL,
	}
	stream, err := c.Get(context.Background(), req)
	if err != nil {
		t.Fatalf("gRIBI failure: Get request failed: %v", err)
	}
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("gRIBI failure: failed to recv from stream: %v", err)
		}
		t.Logf("Got AFT entries: %v", resp.GetEntry())
	}
}

func TestCEOS(t *testing.T) {
	dut := ondatra.DUT(t, "ceos")
	lookupTelemetry(t, dut)
}

func TestCTPX(t *testing.T) {
	dut := ondatra.DUT(t, "cptx")
	lookupTelemetry(t, dut)
	fetchAFTEntries(t, dut)
}

func TestSRL(t *testing.T) {
	t.Skip()
}

func TestXRD(t *testing.T) {
	t.Skip()
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
