package wireutil

import (
	"fmt"
	"os"
	"unsafe"

	"github.com/containernetworking/plugins/pkg/ns"
	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const tunDevice = "/dev/net/tun"

type ifreq struct {
	Name  [unix.IFNAMSIZ]byte
	Flags uint16
	_     [22]byte
}

// CreateOrAttachTAP opens an existing persistent TAP device or creates a new persistent TAP device
// with the given ifName inside the specified network namespace at podNsPath.
// Returns the open *os.File handle to the TAP device.
func CreateOrAttachTAP(podNsPath string, ifName string, ipCIDR string) (*os.File, error) {
	podNs, err := ns.GetNS(podNsPath)
	if err != nil {
		return nil, fmt.Errorf("could not open netns %s: %w", podNsPath, err)
	}
	defer podNs.Close()

	var tapFile *os.File

	err = podNs.Do(func(_ ns.NetNS) error {
		fd, err := unix.Open(tunDevice, unix.O_RDWR, 0)
		if err != nil {
			return fmt.Errorf("failed to open %s in netns %s: %w", tunDevice, podNsPath, err)
		}

		var ifr ifreq
		copy(ifr.Name[:], []byte(ifName))
		ifr.Flags = unix.IFF_TAP | unix.IFF_NO_PI

		_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TUNSETIFF), uintptr(unsafe.Pointer(&ifr)))
		if errno != 0 {
			unix.Close(fd)
			return fmt.Errorf("TUNSETIFF failed for %s in netns %s: %v", ifName, podNsPath, errno)
		}

		// Make device persistent so it survives process crashes/restarts
		_, _, _ = unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TUNSETPERSIST), 1)

		link, err := netlink.LinkByName(ifName)
		if err != nil {
			unix.Close(fd)
			return fmt.Errorf("failed to find link %s inside netns %s: %w", ifName, podNsPath, err)
		}

		if err := netlink.LinkSetUp(link); err != nil {
			log.Warnf("CreateOrAttachTAP: failed to set %s UP in netns %s: %v", ifName, podNsPath, err)
		}

		if ipCIDR != "" {
			addr, err := netlink.ParseAddr(ipCIDR)
			if err == nil {
				_ = netlink.AddrAdd(link, addr)
			}
		}

		tapFile = os.NewFile(uintptr(fd), ifName)
		return nil
	})

	if err != nil {
		return nil, err
	}

	// Disable tx offload inside the netns (ignore error if non-fatal)
	_ = SetTxChecksumOff(ifName, podNsPath)

	return tapFile, nil
}
