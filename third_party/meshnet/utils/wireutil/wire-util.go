package wireutil

import (
	"fmt"
	"hash/fnv"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/safchain/ethtool"
)

// NamespaceVNIOffset returns a VNI offset derived from hashing the namespace name.
// This distributes VNIs for different namespaces into separate blocks of 10,000 to avoid collisions.
func NamespaceVNIOffset(namespace string) int64 {
	if namespace == "" {
		namespace = "default"
	}
	h := fnv.New32a()
	h.Write([]byte(namespace))
	hashVal := h.Sum32()

	// Max VNI is 16,777,215.
	// We divide it into blocks of 10,000 VNIs.
	// Max blocks = 16,777,215 / 10,000 = 1677.
	// We use modulo 1000 to keep it safe.
	blockOffset := int64(hashVal % 1000)

	// Start blocks at 10,000 (block 1) to avoid low VNIs.
	return 10000 + blockOffset*10000
}

const (
	GRPCDefaultPort       = 51111
	INTER_NODE_LINK_VXLAN = "VXLAN"
	INTER_NODE_LINK_GRPC  = "GRPC"
)

func SetTxChecksumOff(intfName, nsName string) error {
	var vethNs ns.NetNS
	var err error

	if vethNs, err = ns.GetNS(nsName); err != nil {
		return fmt.Errorf("could not get required ns %s: %v", nsName, err)
	}
	defer vethNs.Close()

	err = vethNs.Do(func(_ ns.NetNS) error {
		etlHndl, err := ethtool.NewEthtool()
		if err != nil {
			return fmt.Errorf("could not open ethtool handle: %v", err)
		}
		defer etlHndl.Close()

		etlConf := map[string]bool{
			"tx-checksum-ipv4":             false,
			"tx-checksum-ipv6":             false,
			"tx-checksum-ip-generic":       false,
			"tx-tcp-segmentation":          false,
			"tx-tcp6-segmentation":         false,
			"tx-checksum-fcoe-crc":         false,
			"tx-checksum-sctp":             false,
			"tx-tcp-ecn-segmentation":      false,
			"tx-tcp-mangleid-segmentation": false,
		}

		err = etlHndl.Change(intfName, etlConf)
		if err != nil {
			return fmt.Errorf("could not set tx checksum on interface %s, ns %s: %v", intfName, nsName, err)
		}
		return nil
	})
	return err
}
