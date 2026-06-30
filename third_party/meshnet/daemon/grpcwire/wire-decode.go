package grpcwire

import (
	"fmt"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

func DecodeFrame(frame []byte) string {
	pktTypeStr := ""
	numPkts := 1
	totalLen := len(frame)
	etherHdrLen := 14
	totalDecodedLen := 0

	for {
		packet := gopacket.NewPacket(frame, layers.LayerTypeEthernet, gopacket.Default)
		ethernetLayer := packet.Layer(layers.LayerTypeEthernet)
		if ethernetLayer != nil {
			ethernetPacket, _ := ethernetLayer.(*layers.Ethernet)
			pktTypeStr += fmt.Sprintf("Pkt no %d: ", numPkts) + "Ethernet"
			decodedLen, typeStr := DecodePkt(packet, ethernetPacket.NextLayerType(), ethernetPacket.Length)
			pktTypeStr += typeStr
			totalDecodedLen += etherHdrLen + decodedLen
			remainingLen := totalLen - totalDecodedLen
			if remainingLen >= 14 {
				numPkts++
				frame = frame[totalDecodedLen:]
				pktTypeStr += "\n            "
			} else {
				break
			}
		} else {
			break
		}
	}
	if numPkts > 1 {
		pktTypeStr = "Multi Pkts: " + pktTypeStr
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
				if llcPacket.Payload[0] == 0x83 {
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
				if vlanPacket.Payload[0] == 0xFE && vlanPacket.Payload[1] == 0xFE && vlanPacket.Payload[2] == 0x03 {
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
		decodedLen = int(ipPacket.Length)

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
