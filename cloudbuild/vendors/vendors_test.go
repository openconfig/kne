package vendors_test

import (
	"context"
	"io"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	gribipb "github.com/openconfig/gribi/v1/proto/service"
	"github.com/openconfig/ondatra"
	"github.com/openconfig/ondatra/gnmi"
	kinit "github.com/openconfig/ondatra/knebind/init"
	p4pb "github.com/p4lang/p4runtime/go/p4/v1"
)

func TestMain(m *testing.M) {
	ondatra.RunTests(m, kinit.Init)
}

func testGNMI(t *testing.T, dut *ondatra.DUTDevice) {
	t.Helper()
	v := gnmi.Lookup(t, dut, gnmi.OC().System().State())
	if v.IsPresent() {
		t.Logf("Got gNMI system state: %v", v.String())
	}
}

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
		t.Logf("Got gRIBI AFT entries: %v", resp.GetEntry())
	}
}

func testGNOI(t *testing.T, dut *ondatra.DUTDevice) {
	t.Helper()
	systemTime := dut.Operations().Time(t)
	t.Logf("Got gNOI system time: %v", systemTime)
}

func testP4RT(t *testing.T, dut *ondatra.DUTDevice) {
	t.Helper()
	c := dut.RawAPIs().P4RT().New(t)
	resp, err := c.Capabilities(context.Background(), &p4pb.CapabilitiesRequest{})
	if err != nil {
		t.Fatalf("P4RT failure: Capabilities request failed: %v", err)
	}
	t.Logf("Got P4RT Capabilities response: %v", resp)
}

func TestCEOS(t *testing.T) {
	dut := ondatra.DUT(t, "ceos")
	testGNMI(t, dut)
	testGRIBI(t, dut)
	// testGNOI(t, dut)
	// testP4RT(t, dut)
}

func TestCTPX(t *testing.T) {
	dut := ondatra.DUT(t, "cptx")
	testGNMI(t, dut)
	testGRIBI(t, dut)
	testGNOI(t, dut)
	// testP4RT(t, dut)
}

func TestSRL(t *testing.T) {
	dut := ondatra.DUT(t, "srl")
	testGNMI(t, dut)
	testGRIBI(t, dut)
	testGNOI(t, dut)
	testP4RT(t, dut)
}

func TestXRD(t *testing.T) {
	dut := ondatra.DUT(t, "xrd")
	testGNMI(t, dut)
	testGRIBI(t, dut)
	testGNOI(t, dut)
	// testP4RT(t, dut)
}

func Test8000e(t *testing.T) {
	dut := ondatra.DUT(t, "e8000")
	testGNMI(t, dut)
	testGRIBI(t, dut)
	testGNOI(t, dut)
	// testP4RT(t, dut)
}

func TestLemming(t *testing.T) {
	dut := ondatra.DUT(t, "lemming")
	testGNMI(t, dut)
	testGRIBI(t, dut)
	testGNOI(t, dut)
	// testP4RT(t, dut)
}

func TestOTG(t *testing.T) {
	ate := ondatra.ATE(t, "otg")
	cfg := ate.OTG().NewConfig(t)
	portNames := []string{"port1", "port2", "port3", "port4"}
	for _, name := range portNames {
		cfg.Ports().Add().SetName(name)
	}
	ate.OTG().PushConfig(t, cfg)

	gotPortNames := gnmi.GetAll(t, ate.OTG(), gnmi.OTG().PortAny().Name().State())
	sort.Strings(gotPortNames)
	if !cmp.Equal(gotPortNames, portNames) {
		t.Errorf("Telemetry got port names %v, want %v", gotPortNames, portNames)
	}
}
