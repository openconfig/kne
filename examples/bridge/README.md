# KNE Packet Bridge Example

This example demonstrates bridging Layer 2 Ethernet frames across independent topology segments using the KNE Packet Bridge daemon.

## Architecture

```text
[ host1 (192.168.1.1/24) ]
           |
       (eth1 link)
           v
   [ bridge-server ]  (Listens on gRPC port 50058)
           :
           :  <-- gRPC Wire Stream over cluster network (service-bridge-server:50058)
           :
   [ bridge-client ]  (Connected to bridge-server via gRPC)
           ^
       (eth1 link)
           |
[ host2 (192.168.1.2/24) ]
```

There is no direct Meshnet link between `host1` and `host2`. All packets (ARP requests, ICMP echo/reply, TCP, UDP) are dynamically forwarded over the gRPC `Wire` stream between `bridge-server` and `bridge-client`.

## Running the Example

1. **Deploy the Topology:**

   ```bash
   kne create examples/bridge/paired-bridge.pb.txt
   ```

2. **Verify Connectivity via Native Ping:**

   Execute standard Linux `ping` from `host1` to `host2`:

   ```bash
   kubectl exec -it host1 -- ping -c 4 192.168.1.2
   ```

   Execute ping from `host2` to `host1`:

   ```bash
   kubectl exec -it host2 -- ping -c 4 192.168.1.1
   ```

3. **Inspect Captured Traffic (Optional):**

   Run `tcpdump` inside `host2` to observe the Ethernet frames arriving across the bridge:

   ```bash
   kubectl exec -it host2 -- tcpdump -i eth1 -n
   ```

4. **Teardown:**

   ```bash
   kne delete examples/bridge/paired-bridge.pb.txt
   ```

---

## Host-Side `veth` Bridging (Bare Host Use Case)

You can also run `bridge client` directly on a development workstation to bridge local host traffic into a KNE cluster topology:

1. **Create a local veth pair on your workstation:**

   ```bash
   sudo ip link add veth-kne type veth peer name veth-host
   sudo ip link set veth-kne up
   sudo ip link set veth-host up
   sudo ip addr add 192.168.1.100/24 dev veth-host
   ```

2. **Run the KNE bridge client on the local interface:**

   Find the external IP/NodePort for `service-bridge-server` via `kne topology service`, then run:

   ```bash
   sudo kne bridge client --peer=<BRIDGE_SERVICE_IP>:50058 --interface=veth-kne
   ```

3. **Ping directly from the host:**

   ```bash
   ping -I veth-host 192.168.1.2
   ```
