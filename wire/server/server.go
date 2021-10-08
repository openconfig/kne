package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
)

func main() {
	var (
		d = flag.String("d", "", "-d [device]: device(network interface)")
	)

	flag.Parse()

	if *d == "" {
		fmt.Println("Invalid device")
		os.Exit(1)
	}
	h, err := pcap.OpenLive(*d, 10000, true, pcap.BlockForever)
	if err != nil {
		fmt.Printf("Error: %v", err)
		os.Exit(1)
	}
	for {
		b, ci, err := h.ReadPacketData()
		if err != nil {
			fmt.Printf("Error: %v", err)
			os.Exit(1)
		}
		p := gopacket.NewPacket(b, layers.LayerTypeEthernet, gopacket.Default)
		fmt.Println(ci.Timestamp, p.String())
	}
}
