package presubmit_test

import (
	// "context"
	// "io"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	// gpb "github.com/openconfig/gribi/v1/proto/service"
	"github.com/openconfig/ondatra"
	"github.com/openconfig/ondatra/gnmi"
	kinit "github.com/openconfig/ondatra/knebind/init"
)

func TestMain(m *testing.M) {
	ondatra.RunTests(m, kinit.Init)
}

// lookupTelemetry checks for system telemetry for a DUT.
func lookupTelemetry(t *testing.T, dut *ondatra.DUTDevice) {
	t.Helper()
	sys := gnmi.Lookup(t, dut, gnmi.OC().System().State())
	if !sys.IsPresent() {
		t.Fatalf("No System telemetry for %v", dut)
	}
}

func TestGNMICEOS(t *testing.T) {
	dut := ondatra.DUT(t, "ceos")
	lookupTelemetry(t, dut)
}

func TestGNMICTPX(t *testing.T) {
	t.Skip()
}

func TestGNMISRL(t *testing.T) {
	t.Skip()
}

func TestGNMIXRD(t *testing.T) {
	t.Skip()
}

func TestGNOICEOS(t *testing.T) {
	t.Skip()
}

func TestGNOICTPX(t *testing.T) {
	t.Skip()
}

func TestGNOISRL(t *testing.T) {
	t.Skip()
}

func TestGNOIXRD(t *testing.T) {
	t.Skip()
}

// fetchAFTEntries checks for AFT entries using gRIBI for a DUT.
// func fetchAFTEntries(t *testing.T, dut *ondatra.DUTDevice) {
//	t.Helper()
//	c := dut.RawAPIs().GRIBI().New(t)
//	req := &gpb.GetRequest{
//		NetworkInstance: &gpb.GetRequest_All{},
//		Aft:             gpb.AFTType_ALL,
//	}
//	stream, err := c.Get(context.Background(), req)
//	if err != nil {
//		t.Fatalf("gRIBI Get request failed: %v", err)
//	}
//	for {
//		resp, err := stream.Recv()
//		if err == io.EOF {
//			break
//		}
//		if err != nil {
//			t.Fatalf("failed to recv from stream: %v", err)
//		}
//		t.Logf("Got AFT entries: %v", resp.GetEntry())
//	}
// }

func TestGRIBICEOS(t *testing.T) {
	t.Skip()
}

func TestGRIBICTPX(t *testing.T) {
	t.Skip()
}

func TestGRIBISRL(t *testing.T) {
	t.Skip()
}

func TestGRIBIXRD(t *testing.T) {
	t.Skip()
}

func TestP4RTCEOS(t *testing.T) {
	t.Skip()
}

func TestP4RTCPTX(t *testing.T) {
	t.Skip()
}

func TestP4RTSRL(t *testing.T) {
	t.Skip()
}

func TestP4RTXRD(t *testing.T) {
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

	portNames := gnmi.GetAll(t, ate.OTG(), gnmi.OTG().PortAny().Name().State())
	sort.Strings(portNames)
	if want := []string{"port1", "port2", "port3", "port4"}; !cmp.Equal(portNames, want) {
		t.Errorf("Telemetry got port names %v, want %v", portNames, want)
	}
}
