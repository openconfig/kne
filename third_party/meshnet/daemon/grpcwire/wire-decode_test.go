package grpcwire

import (
	"bytes"
	"net"
	"strings"
	"testing"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

var (
	testSrcMAC = net.HardwareAddr{0x02, 0, 0, 0, 0, 1}
	testDstMAC = net.HardwareAddr{0x02, 0, 0, 0, 0, 2}
)

// ipv4UDPFrame returns an Ethernet/IPv4/UDP frame that is size bytes long.
func ipv4UDPFrame(t *testing.T, size int) []byte {
	t.Helper()
	const hdrs = 14 + 20 + 8
	if size < hdrs {
		t.Fatalf("ipv4UDPFrame: size %d is smaller than the headers (%d)", size, hdrs)
	}
	eth := &layers.Ethernet{SrcMAC: testSrcMAC, DstMAC: testDstMAC, EthernetType: layers.EthernetTypeIPv4}
	ip := &layers.IPv4{
		Version:  4,
		TTL:      64,
		Protocol: layers.IPProtocolUDP,
		SrcIP:    net.IPv4(192, 0, 2, 1),
		DstIP:    net.IPv4(192, 0, 2, 2),
	}
	udp := &layers.UDP{SrcPort: 1000, DstPort: 2000}
	if err := udp.SetNetworkLayerForChecksum(ip); err != nil {
		t.Fatalf("SetNetworkLayerForChecksum: %v", err)
	}
	return serialize(t, size, eth, ip, udp, gopacket.Payload(make([]byte, size-hdrs)))
}

// ipv6UDPFrame returns an Ethernet/IPv6/UDP frame that is size bytes long.
func ipv6UDPFrame(t *testing.T, size int) []byte {
	t.Helper()
	const hdrs = 14 + 40 + 8
	if size < hdrs {
		t.Fatalf("ipv6UDPFrame: size %d is smaller than the headers (%d)", size, hdrs)
	}
	eth := &layers.Ethernet{SrcMAC: testSrcMAC, DstMAC: testDstMAC, EthernetType: layers.EthernetTypeIPv6}
	ip := &layers.IPv6{
		Version:    6,
		HopLimit:   64,
		NextHeader: layers.IPProtocolUDP,
		SrcIP:      net.ParseIP("2001:db8::1"),
		DstIP:      net.ParseIP("2001:db8::2"),
	}
	udp := &layers.UDP{SrcPort: 1000, DstPort: 2000}
	if err := udp.SetNetworkLayerForChecksum(ip); err != nil {
		t.Fatalf("SetNetworkLayerForChecksum: %v", err)
	}
	return serialize(t, size, eth, ip, udp, gopacket.Payload(make([]byte, size-hdrs)))
}

func serialize(t *testing.T, size int, ls ...gopacket.SerializableLayer) []byte {
	t.Helper()
	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}
	if err := gopacket.SerializeLayers(buf, opts, ls...); err != nil {
		t.Fatalf("SerializeLayers: %v", err)
	}
	b := buf.Bytes()
	if len(b) != size {
		t.Fatalf("serialized frame is %d bytes, want %d", len(b), size)
	}
	return b
}

func TestDecodeFrame(t *testing.T) {
	tests := []struct {
		desc      string
		frame     []byte
		wantPkts  int
		wantMulti bool
	}{{
		desc:     "single IPv4 frame",
		frame:    ipv4UDPFrame(t, 100),
		wantPkts: 1,
	}, {
		desc:     "single jumbo IPv4 frame",
		frame:    ipv4UDPFrame(t, 9228),
		wantPkts: 1,
	}, {
		// The IPv6 Length field excludes the 40-byte header. Treating it
		// as the whole packet made a jumbo frame look like several.
		desc:     "single jumbo IPv6 frame",
		frame:    ipv6UDPFrame(t, 9228),
		wantPkts: 1,
	}, {
		desc:      "two IPv4 frames",
		frame:     bytes.Join([][]byte{ipv4UDPFrame(t, 1000), ipv4UDPFrame(t, 1000)}, nil),
		wantPkts:  2,
		wantMulti: true,
	}, {
		// b/586672521: re-slicing by the cumulative offset ran past the
		// end of the buffer from the third packet on.
		desc: "four IPv4 frames over 1518 bytes",
		frame: bytes.Join([][]byte{
			ipv4UDPFrame(t, 500), ipv4UDPFrame(t, 500), ipv4UDPFrame(t, 500), ipv4UDPFrame(t, 500),
		}, nil),
		wantPkts:  4,
		wantMulti: true,
	}, {
		desc: "mixed IPv4 and IPv6 frames of different sizes",
		frame: bytes.Join([][]byte{
			ipv4UDPFrame(t, 300), ipv6UDPFrame(t, 1200), ipv4UDPFrame(t, 64), ipv6UDPFrame(t, 900), ipv4UDPFrame(t, 1514),
		}, nil),
		wantPkts:  5,
		wantMulti: true,
	}, {
		desc:     "trailing bytes shorter than an Ethernet header",
		frame:    append(ipv4UDPFrame(t, 1600), make([]byte, 10)...),
		wantPkts: 1,
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := DecodeFrame(tt.frame)
			if strings.Contains(got, "decode error") {
				t.Errorf("DecodeFrame() = %q, want no decode error", got)
			}
			if n := strings.Count(got, "Pkt no "); n != tt.wantPkts {
				t.Errorf("DecodeFrame() decoded %d packets, want %d; got %q", n, tt.wantPkts, got)
			}
			if gotMulti := strings.HasPrefix(got, "Multi Pkts: "); gotMulti != tt.wantMulti {
				t.Errorf("DecodeFrame() multi-packet prefix = %v, want %v; got %q", gotMulti, tt.wantMulti, got)
			}
		})
	}
}

// TestDecodeFrameMalformed checks that DecodeFrame does not panic on
// truncated, oversized, or garbage input.
func TestDecodeFrameMalformed(t *testing.T) {
	good := ipv4UDPFrame(t, 1000)

	// An IPv4 header that claims a total length past the end of the buffer.
	overLen := append([]byte(nil), good...)
	overLen[14+2], overLen[14+3] = 0xff, 0xff

	garbage := make([]byte, 9228)
	for i := range garbage {
		garbage[i] = byte(i * 7)
	}

	tests := []struct {
		desc  string
		frame []byte
	}{
		{desc: "nil", frame: nil},
		{desc: "empty", frame: []byte{}},
		{desc: "shorter than an Ethernet header", frame: good[:10]},
		{desc: "truncated IPv4 packet", frame: good[:40]},
		{desc: "IPv4 length past end of buffer", frame: bytes.Join([][]byte{overLen, good}, nil)},
		{desc: "garbage", frame: garbage},
		{desc: "VLAN tag with no payload", frame: []byte{
			2, 0, 0, 0, 0, 2, 2, 0, 0, 0, 0, 1, 0x81, 0x00, 0x00, 0x01, 0x00, 0x00,
		}},
		{desc: "LLC header with no payload", frame: []byte{
			2, 0, 0, 0, 0, 2, 2, 0, 0, 0, 0, 1, 0x00, 0x03, 0xfe, 0xfe, 0x03,
		}},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			// DecodeFrame recovers internally, so a panic here is a failure.
			_ = DecodeFrame(tt.frame)
		})
	}
}
