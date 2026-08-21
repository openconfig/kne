package wireutil

import (
	"os"
	"strconv"

       log "github.com/sirupsen/logrus"
       "golang.org/x/sys/unix"
)

// GetEnvInt reads an integer environment variable with a default fallback value if unset or invalid.
func GetEnvInt(key string, defaultVal int) int {
	if valStr := os.Getenv(key); valStr != "" {
		if val, err := strconv.Atoi(valStr); err == nil && val > 0 {
			return val
		}
	}
	return defaultVal
}

func getEnvString(key string, defaultVal string) string {
	if valStr := os.Getenv(key); valStr != "" {
		return valStr
	}
	return defaultVal
}

// GetLinkTxQLen returns the configured link txqueuelen (default 10000, configurable via LINK_TXQUEUELEN).
func GetLinkTxQLen() int {
	return GetEnvInt("LINK_TXQUEUELEN", 10000)
}

// TuneSystem configures global OS sysctl tunables (backlog, buffers, ARP/neighbor GC thresholds,
// multicast group limits, rp_filter, and IPv6 startup behavior) and RLIMIT_NOFILE for high-density,
// high-throughput network topologies. Values can be customized via environment variables.
func TuneSystem() {
	// 1. Increase max open file descriptors for daemon (rlimit)
	noFileLimit := GetEnvInt("RLIMIT_NOFILE", 1048576)
	var rlim unix.Rlimit
	rlim.Max = uint64(noFileLimit)
	rlim.Cur = uint64(noFileLimit)
	if err := unix.Setrlimit(unix.RLIMIT_NOFILE, &rlim); err != nil {
		log.Warnf("TuneSystem: failed to set RLIMIT_NOFILE to %d: %v", noFileLimit, err)
	} else {
		log.Infof("TuneSystem: successfully set RLIMIT_NOFILE to %d", noFileLimit)
	}

	// 2. Sysctl kernel tunables for network device backlog, buffer limits, ARP/neighbor GC thresholds,
	// multicast memberships, reverse path filtering, and IPv6 DAD/RS startup tuning.
	sysctls := map[string]string{
		"/proc/sys/net/core/netdev_max_backlog":                getEnvString("NETDEV_MAX_BACKLOG", "10000"),
		"/proc/sys/net/core/rmem_max":                           getEnvString("RMEM_MAX", "16777216"),
		"/proc/sys/net/core/wmem_max":                           getEnvString("WMEM_MAX", "16777216"),
		"/proc/sys/net/core/rmem_default":                       getEnvString("RMEM_DEFAULT", "16777216"),
		"/proc/sys/net/core/wmem_default":                       getEnvString("WMEM_DEFAULT", "16777216"),
		"/proc/sys/net/ipv4/neigh/default/gc_thresh1":           getEnvString("ARP_GC_THRESH1", "1024"),
		"/proc/sys/net/ipv4/neigh/default/gc_thresh2":           getEnvString("ARP_GC_THRESH2", "4096"),
		"/proc/sys/net/ipv4/neigh/default/gc_thresh3":           getEnvString("ARP_GC_THRESH3", "8192"),
		"/proc/sys/net/ipv6/neigh/default/gc_thresh1":           getEnvString("ARP_GC_THRESH1", "1024"),
		"/proc/sys/net/ipv6/neigh/default/gc_thresh2":           getEnvString("ARP_GC_THRESH2", "4096"),
		"/proc/sys/net/ipv6/neigh/default/gc_thresh3":           getEnvString("ARP_GC_THRESH3", "8192"),
		"/proc/sys/net/ipv4/igmp_max_memberships":               getEnvString("IGMP_MAX_MEMBERSHIPS", "10000"),
		"/proc/sys/net/ipv6/mld_max_msf":                        getEnvString("MLD_MAX_MSF", "4096"),
		"/proc/sys/net/ipv4/conf/all/rp_filter":                 getEnvString("RP_FILTER", "2"),
		"/proc/sys/net/ipv4/conf/default/rp_filter":             getEnvString("RP_FILTER", "2"),
		"/proc/sys/net/ipv6/conf/default/accept_dad":            getEnvString("IPV6_ACCEPT_DAD", "0"),
		"/proc/sys/net/ipv6/conf/default/router_solicitations": getEnvString("IPV6_ROUTER_SOLICITATIONS", "0"),
		"/proc/sys/net/ipv6/route/max_size":                    getEnvString("IPV6_ROUTE_MAX_SIZE", "1048576"),
	}

	for path, val := range sysctls {
		if err := os.WriteFile(path, []byte(val), 0644); err != nil {
			log.Warnf("TuneSystem: failed to write %s to %s: %v", val, path, err)
		} else {
			log.Infof("TuneSystem: set %s = %s", path, val)
		}
	}
}
