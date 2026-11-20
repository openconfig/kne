package grpcwire

import (
	"fmt"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

const (
	etherHdrLen = 14
	ipv6HdrLen  = 40
)

// DecodeFrame returns a human-readable summary of the Ethernet packets in
// frame, for logging. frame may hold more than one packet back to back (for
// example, when GRO coalesced them). DecodeFrame never panics: decoding stops
// at the first packet whose length is unknown or runs past the end of frame.
func DecodeFrame(frame []byte) (pktTypeStr string) {
	numPkts := 1
	defer func() {
		if r := recover(); r != nil {
			pktTypeStr += fmt.Sprintf(" [decode error: %v]", r)
		}
		if numPkts > 1 {
			pktTypeStr = "Multi Pkts: " + pktTypeStr
		}
	}()

	for len(frame) >= etherHdrLen {
		packet := gopacket.NewPacket(frame, layers.LayerTypeEthernet, gopacket.Default)
		ethernetLayer := packet.Layer(layers.LayerTypeEthernet)
		if ethernetLayer == nil {
			break
		}
		ethernetPacket, _ := ethernetLayer.(*layers.Ethernet)
		pktTypeStr += fmt.Sprintf("Pkt no %d: ", numPkts) + "Ethernet"
		decodedLen, typeStr := DecodePkt(packet, ethernetPacket.NextLayerType(), ethernetPacket.Length)
		pktTypeStr += typeStr
		// Advance by the length of the packet just decoded. Stop if that
		// length is unknown or would run past the end of the buffer.
		pktLen := etherHdrLen + decodedLen
		if decodedLen <= 0 || pktLen > len(frame) {
			break
		}
		frame = frame[pktLen:]
		if len(frame) < etherHdrLen {
			break
		}
		numPkts++
		pktTypeStr += "\n            "
	}
	return pktTypeStr
}

func DecodePkt(packet gopacket.Packet, layerType gopacket.LayerType, length uint16) (int, string) {
	var typeStr, pktTypeStr string
	decodedLen := 0

	if layerType == layers.LayerTypeIPv4 {
		decodedLen, typeStr = decodeIPv4Pkt(packet)
		pktTypeStr += typeStr
	} else if layerType == layers.LayerTypeIPv6 {
		decodedLen, typeStr = decodeIPv6Pkt(packet)
		pktTypeStr += typeStr
	} else if layerType == layers.LayerTypeLLC {
		llcLayer := packet.Layer(layers.LayerTypeLLC)
		if llcLayer != nil {
			pktTypeStr += ":LLC"
			llcPacket, _ := llcLayer.(*layers.LLC)
			if llcPacket.DSAP == 0xFE && llcPacket.SSAP == 0xFE && llcPacket.Control == 0x3 {
				if len(llcPacket.Payload) > 0 && llcPacket.Payload[0] == 0x83 {
					pktTypeStr += ":ISIS"
				}
			}
		}
		decodedLen = int(length)
	} else if layerType == layers.LayerTypeARP {
		pktTypeStr += ":ARP"
		decodedLen = 28
	} else if layerType == layers.LayerTypeDot1Q {
		pktTypeStr += ":VLAN"
		//fmt.Printf("VLAN\n")
		vlanHdrLen := 4
		vlanLayer := packet.Layer(layers.LayerTypeDot1Q)
		if vlanLayer != nil {
			vlanPacket, _ := vlanLayer.(*layers.Dot1Q)
			nextLayer := vlanPacket.NextLayerType()
			if nextLayer == gopacket.LayerTypeZero {
				// this may be LLC layer. try to match with known LLC 0xFEFE03
				if len(vlanPacket.Payload) >= 3 && vlanPacket.Payload[0] == 0xFE && vlanPacket.Payload[1] == 0xFE && vlanPacket.Payload[2] == 0x03 {
					packet := gopacket.NewPacket(vlanPacket.Payload, layers.LayerTypeLLC, gopacket.Default)
					decodedLen, typeStr = DecodePkt(packet, layers.LayerTypeLLC, uint16(vlanPacket.Type))
					pktTypeStr += typeStr
					decodedLen += vlanHdrLen
				}
			} else {
				packet := gopacket.NewPacket(vlanPacket.Payload, nextLayer, gopacket.Default)
				decodedLen, typeStr = DecodePkt(packet, nextLayer, 0)
				pktTypeStr += typeStr
				decodedLen += vlanHdrLen
			}
		} else {
			pktTypeStr += ":No VLAN Hdr"
		}
	}

	return decodedLen, pktTypeStr
}

func decodeIPv4Pkt(packet gopacket.Packet) (int, string) {
	var decodedLen int = 0
	pktTypeStr := ":IPv4"

	ipLayer := packet.Layer(layers.LayerTypeIPv4)
	if ipLayer != nil {
		ipPacket, _ := ipLayer.(*layers.IPv4)
		decodedLen = int(ipPacket.Length)

		pktTypeStr += fmt.Sprintf("[s:%s, d:%s]", ipPacket.SrcIP.String(), ipPacket.DstIP.String())
		if ipPacket.Protocol == layers.IPProtocolICMPv4 {
			pktTypeStr += ":ICMP"
		} else if ipPacket.Protocol == layers.IPProtocolTCP {
			pktTypeStr += ":TCP"
			tcpLayer := packet.Layer(layers.LayerTypeTCP)
			if tcpLayer != nil {
				tcpPkt := tcpLayer.(*layers.TCP)
				if tcpPkt.DstPort == 179 {
					pktTypeStr += ":BGP"
				} else {
					pktTypeStr += fmt.Sprintf(":[Port:%d]", tcpPkt.DstPort)
				}
			}
		} else {
			pktTypeStr += fmt.Sprintf(":IPv4 with protocol : %d", ipPacket.Protocol)
		}
	}
	return decodedLen, pktTypeStr
}

func decodeIPv6Pkt(packet gopacket.Packet) (int, string) {
	var decodedLen int = 0
	pktTypeStr := ":IPv6"

	ipLayer := packet.Layer(layers.LayerTypeIPv6)
	if ipLayer != nil {
		ipPacket, _ := ipLayer.(*layers.IPv6)
		// The IPv6 Length field holds the payload length only, so add the
		// fixed header. A zero Length (jumbogram) is left unknown.
		if ipPacket.Length > 0 {
			decodedLen = ipv6HdrLen + int(ipPacket.Length)
		}

		pktTypeStr += fmt.Sprintf("[s:%s, d:%s]", ipPacket.SrcIP.String(), ipPacket.DstIP.String())
		if ipPacket.NextHeader == layers.IPProtocolICMPv6 {
			pktTypeStr += ":ICMPv6"
		} else if ipPacket.NextHeader == layers.IPProtocolTCP {
			pktTypeStr += ":TCP"
			tcpLayer := packet.Layer(layers.LayerTypeTCP)
			if tcpLayer != nil {
				tcpPkt := tcpLayer.(*layers.TCP)
				if tcpPkt.DstPort == 179 {
					pktTypeStr += ":BGP"
				} else {
					pktTypeStr += fmt.Sprintf("[Port:%d]", tcpPkt.DstPort)
				}
			}
		} else {
			pktTypeStr += fmt.Sprintf(":IPv6 with protocol : %d", ipPacket.NextHeader)
		}
	}
	return decodedLen, pktTypeStr
}
