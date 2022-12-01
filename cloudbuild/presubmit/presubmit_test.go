package presubmit_test

import (
	"context"
	"io"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
	gribipb "github.com/openconfig/gribi/v1/proto/service"
	"github.com/openconfig/ondatra"
	kinit "github.com/openconfig/ondatra/knebind/init"
)

func TestMain(m *testing.M) {
	ondatra.RunTests(m, kinit.Init)
}

// testGNMI checks the status of port1 of the dut.
func testGNMI(t *testing.T, dut *ondatra.DUTDevice) {
	t.Helper()
	c := dut.RawAPIs().GNMI().New(t)
	resp, err := c.Capabilities(context.Background(), &gnmipb.CapabilityRequest{})
	if err != nil {
		t.Fatalf("gNMI failure: Capabilities request failed: %v", err)
	}
	t.Logf("Got Capabilities response: %v", resp)
}

// testGRIBI checks for AFT entries for a DUT.
func testGRIBI(t *testing.T, dut *ondatra.DUTDevice) {
	t.Helper()
	c := dut.RawAPIs().GRIBI().New(t)
	req := &gribipb.GetRequest{
		NetworkInstance: &gribipb.GetRequest_All{},
		Aft:             gribipb.AFTType_ALL,
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

// testGNOI checks the system time using gNOI for a DUT.
func testGNOI(t *testing.T, dut *ondatra.DUTDevice) {
	t.Helper()
	systemTime := dut.Operations().Time(t)
	t.Logf("Got system time: %v", systemTime)
}

func TestCEOS(t *testing.T) {
	dut := ondatra.DUT(t, "ceos")
	testGNMI(t, dut)
}

func TestCTPX(t *testing.T) {
	dut := ondatra.DUT(t, "cptx")
	testGNMI(t, dut)
	testGRIBI(t, dut)
	testGNOI(t, dut)
}

func TestSRL(t *testing.T) {
	dut := ondatra.DUT(t, "srl")
	testGNMI(t, dut)
	testGRIBI(t, dut)
	testGNOI(t, dut)
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
