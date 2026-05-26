package inclusterproxy

import (
	"testing"

	"github.com/openconfig/gnmi/errdiff"
	topopb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	"google.golang.org/protobuf/proto"
)

func TestNew(t *testing.T) {
	tests := []struct {
		desc    string
		ni      *node.Impl
		wantPB  *topopb.Node
		wantErr string
	}{{
		desc:    "nil node impl",
		wantErr: "nodeImpl cannot be nil",
	}, {
		desc:    "nil pb",
		wantErr: "nodeImpl.Proto cannot be nil",
		ni:      &node.Impl{},
	}, {
		desc: "missing proxy-pool-for label",
		ni: &node.Impl{
			Proto: &topopb.Node{
				Name: "test_node",
			},
		},
		wantErr: "label 'proxy-pool-for' is required",
	}, {
		desc: "missing interfaces",
		ni: &node.Impl{
			Proto: &topopb.Node{
				Name: "test_node",
				Labels: map[string]string{
					"proxy-pool-for": "dut1",
				},
				Services: map[uint32]*topopb.Service{1: {Inside: 1}},
			},
		},
		wantErr: "exactly one interface is required",
	}, {
		desc: "too many interfaces",
		ni: &node.Impl{
			Proto: &topopb.Node{
				Name: "test_node",
				Labels: map[string]string{
					"proxy-pool-for": "dut1",
				},
				Interfaces: map[string]*topopb.Interface{
					"eth1": {},
					"eth2": {},
				},
				Services: map[uint32]*topopb.Service{1: {Inside: 1}},
			},
		},
		wantErr: "exactly one interface is required",
	}, {
		desc: "wrong interface name",
		ni: &node.Impl{
			Proto: &topopb.Node{
				Name: "test_node",
				Labels: map[string]string{
					"proxy-pool-for": "dut1",
				},
				Interfaces: map[string]*topopb.Interface{
					"eth2": {},
				},
				Services: map[uint32]*topopb.Service{1: {Inside: 1}},
			},
		},
		wantErr: "interface must be 'eth1'",
	}, {
		desc: "multiple services not allowed",
		ni: &node.Impl{
			Proto: &topopb.Node{
				Name: "test_node",
				Labels: map[string]string{
					"proxy-pool-for": "dut1",
				},
				Interfaces: map[string]*topopb.Interface{
					"eth1": {PeerName: "dut1"},
				},
				Services: map[uint32]*topopb.Service{
					1: {Inside: 1},
					2: {Inside: 2},
				},
			},
		},
		wantErr: "exactly one service must be configured",
	}, {
		desc: "valid pb manual command",
		ni: &node.Impl{
			Proto: &topopb.Node{
				Name: "test_node",
				Labels: map[string]string{
					"proxy-pool-for": "dut1",
				},
				Interfaces: map[string]*topopb.Interface{
					"eth1": {PeerName: "dut1"},
				},
				Services: map[uint32]*topopb.Service{1790: {Inside: 1790}},
				Config: &topopb.Config{
					Command: []string{"socat"},
				},
			},
		},
		wantPB: &topopb.Node{
			Name: "test_node",
			Labels: map[string]string{
				"proxy-pool-for": "dut1",
			},
			Config: &topopb.Config{
				Image:   "nicolaka/netshoot:latest",
				Command: []string{"socat"},
			},
			Interfaces: map[string]*topopb.Interface{
				"eth1": {PeerName: "dut1"},
			},
			Services: map[uint32]*topopb.Service{1790: {Inside: 1790}},
		},
	}, {
		desc: "valid pb with automatic generation ipv4",
		ni: &node.Impl{
			Proto: &topopb.Node{
				Name: "test_node",
				Labels: map[string]string{
					"proxy-pool-for": "dut1",
					"peer-ip":        "192.168.100.1",
					"peer-prefix":    "31",
					"target-port":    "179",
				},
				Interfaces: map[string]*topopb.Interface{
					"eth1": {PeerName: "dut1"},
				},
				Services: map[uint32]*topopb.Service{
					1790: {Inside: 1790},
				},
			},
		},
		wantPB: &topopb.Node{
			Name: "test_node",
			Labels: map[string]string{
				"proxy-pool-for": "dut1",
				"peer-ip":        "192.168.100.1",
				"peer-prefix":    "31",
				"target-port":    "179",
			},
			Config: &topopb.Config{
				Image:   "nicolaka/netshoot:latest",
				Command: []string{"/bin/sh", "-c"},
				Args:    []string{"ip addr add 192.168.100.0/31 dev eth1 && ip link set eth1 up && sleep 2 && /usr/bin/socat -d -d TCP-LISTEN:1790,reuseaddr,fork TCP:192.168.100.1:179"},
			},
			Interfaces: map[string]*topopb.Interface{
				"eth1": {PeerName: "dut1"},
			},
			Services: map[uint32]*topopb.Service{
				1790: {Inside: 1790},
			},
		},
	}, {
		desc: "valid pb with automatic generation ipv6",
		ni: &node.Impl{
			Proto: &topopb.Node{
				Name: "test_node",
				Labels: map[string]string{
					"proxy-pool-for": "dut1",
					"peer-ip":        "2001:db8::1",
					"peer-prefix":    "127",
					"target-port":    "179",
				},
				Interfaces: map[string]*topopb.Interface{
					"eth1": {PeerName: "dut1"},
				},
				Services: map[uint32]*topopb.Service{
					1790: {Inside: 1790},
				},
			},
		},
		wantPB: &topopb.Node{
			Name: "test_node",
			Labels: map[string]string{
				"proxy-pool-for": "dut1",
				"peer-ip":        "2001:db8::1",
				"peer-prefix":    "127",
				"target-port":    "179",
			},
			Config: &topopb.Config{
				Image:   "nicolaka/netshoot:latest",
				Command: []string{"/bin/sh", "-c"},
				Args:    []string{"ip addr add 2001:db8::/127 dev eth1 && ip link set eth1 up && sleep 2 && /usr/bin/socat -d -d TCP6-LISTEN:1790,reuseaddr,fork TCP6:2001:db8::1:179"},
			},
			Interfaces: map[string]*topopb.Interface{
				"eth1": {PeerName: "dut1"},
			},
			Services: map[uint32]*topopb.Service{
				1790: {Inside: 1790},
			},
		},
	}, {
		desc: "invalid peer-ip format",
		ni: &node.Impl{
			Proto: &topopb.Node{
				Name: "test_node",
				Labels: map[string]string{
					"proxy-pool-for": "dut1",
					"peer-ip":        "invalid-ip",
					"peer-prefix":    "31",
					"target-port":    "179",
				},
				Interfaces: map[string]*topopb.Interface{
					"eth1": {PeerName: "dut1"},
				},
				Services: map[uint32]*topopb.Service{1: {Inside: 1}},
			},
		},
		wantErr: "invalid 'peer-ip' or 'peer-prefix' label",
	}, {
		desc: "invalid peer-ip mask v4",
		ni: &node.Impl{
			Proto: &topopb.Node{
				Name: "test_node",
				Labels: map[string]string{
					"proxy-pool-for": "dut1",
					"peer-ip":        "192.168.100.1",
					"peer-prefix":    "24",
					"target-port":    "179",
				},
				Interfaces: map[string]*topopb.Interface{
					"eth1": {PeerName: "dut1"},
				},
				Services: map[uint32]*topopb.Service{1: {Inside: 1}},
			},
		},
		wantErr: "only /31 mask is supported for IPv4",
	}, {
		desc: "invalid peer-ip mask v6",
		ni: &node.Impl{
			Proto: &topopb.Node{
				Name: "test_node",
				Labels: map[string]string{
					"proxy-pool-for": "dut1",
					"peer-ip":        "2001:db8::1",
					"peer-prefix":    "64",
					"target-port":    "179",
				},
				Interfaces: map[string]*topopb.Interface{
					"eth1": {PeerName: "dut1"},
				},
				Services: map[uint32]*topopb.Service{1: {Inside: 1}},
			},
		},
		wantErr: "only /127 mask is supported for IPv6",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			impl, err := New(tt.ni)
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: got: %v, want: %s", err, s)
			}
			if tt.wantErr != "" {
				return
			}
			if !proto.Equal(impl.GetProto(), tt.wantPB) {
				t.Fatalf("New() failed: got\n%v\nwant\n%v", impl.GetProto(), tt.wantPB)
			}
		})
	}
}
