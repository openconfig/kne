# Performance and System Tuning in KNE

This document details host kernel tunables, interface queue configurations, and gRPC overlay settings recommended for running high-density emulated topologies and bursty control-plane traffic (such as large BGP table exchanges and high-throughput IS-IS meshes) in KNE.

---

## 1. Host Kernel Sysctl Tunables

When running large-scale or multi-vendor network topologies in KNE, default Linux networking buffers and device backlog limits can lead to silent packet dropouts under sudden traffic bursts.

### Recommended Sysctl Configuration

Create `/etc/sysctl.d/99-kne.conf` on K8s worker nodes:

```ini
# Increase maximum network device input queue backlog for bursty packet creation
net.core.netdev_max_backlog = 10000

# Increase maximum OS socket receive and send buffer sizes to 16 MB
net.core.rmem_max = 16777216
net.core.wmem_max = 16777216

# Increase default OS socket receive and send buffer sizes to 16 MB
net.core.rmem_default = 16777216
net.core.wmem_default = 16777216
```

Apply the configuration immediately:

```bash
sudo sysctl --system
```

### Why These Tunables Matter

* **`net.core.netdev_max_backlog = 10000`**: The default Linux backlog queue (`1000`) can fill up rapidly when hundreds of virtual interfaces simultaneously receive bursty control-plane frames (e.g., initial link-state advertisements or topology convergence events). Increasing the backlog queue prevents kernel-level packet drops before frames reach container sockets.
* **`net.core.rmem_max` / `wmem_max` / `rmem_default` / `wmem_default = 16777216` (16 MB)**: Default Linux socket buffers (typically ~212 KB) are insufficient for large BGP updates, routing snapshots, or high-volume telemetry streams across emulated nodes. Providing a 16 MB ceiling and default allows socket buffer allocations (such as `SO_RCVBUF` / `SO_SNDBUF` of 4 MB used by high-performance network bridges and routers) to allocate sufficient buffer memory without kernel truncation or dropouts.

> **Note**: These sysctl settings are pre-baked into KNE GCE VM images built via CloudBuild/Packer (`cloudbuild/internal.pkr.hcl` and `cloudbuild/external.pkr.hcl`). For custom Kubernetes clusters or bare-metal setups, apply `/etc/sysctl.d/99-kne.conf` manually on each node.

---

## 2. Link Interface Queue Length (`txqueuelen`)

Virtual Ethernet (`vEth`), TAP, and vxLAN links created by Meshnet carry full line-rate inter-container traffic. The default Linux `txqueuelen` for virtual interfaces is often small (`1000` or `0`), which can drop packets when container workloads burst faster than context switching can drain the virtual device.

* Meshnet configures `txqueuelen = 10000` on created TAP, vEth, and vxLAN interfaces.
* You can override the default queue length via the `LINK_TXQUEUELEN` environment variable on the `meshnet` daemon pod if desired:

```yaml
env:
  - name: LINK_TXQUEUELEN
    value: "10000"
```

---

## 3. gRPC Overlay Stream & Connection Flow Control

When using gRPC wire overlay (`INTER_NODE_LINK_TYPE: "GRPC"`) for cross-node mesh tunneling, Meshnet communicates via multiplexed bidirectional gRPC streams.

To avoid HTTP/2 stream-level flow control stalls and handle bursty control-plane traffic:
* **Stream Initial Window Size (`InitialWindowSize`)**: `4 MB` (overrides standard HTTP/2 64 KB window to allow large packet bursts per stream).
* **Connection Initial Window Size (`InitialConnWindowSize`)**: `16 MB` (overrides default connection window to provide multiplexed stream headroom).
* **Max Message Size (`MaxRecvMsgSize` / `MaxSendMsgSize`)**: `64 MB` (ensures frames, Jumbo packets, and batch RPCs are never rejected).

---

## 4. MTU and Jumbo Frames

KNE and Meshnet support standard MTU (`1500`) as well as Jumbo frames (`9216` bytes) for IS-IS LSPs, BGP Jumbo frames, and encapsulated overlay traffic:
* Interface MTU can be specified per-link in the KNE topology definition (under `interfaces.mtu`).
* Meshnet internal packet buffers allocate up to `65535` bytes, fully preventing frame truncation for Jumbo frames.
