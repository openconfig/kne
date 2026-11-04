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
           :  <-- gRPC Wire Stream over cluster network (bridge-server:50058)
           :
   [ bridge-client ]  (Connected to bridge-server via gRPC)
           ^
       (eth1 link)
           |
[ host2 (192.168.1.2/24) ]
```

There is no direct Meshnet link between `host1` and `host2`. All packets (ARP requests, ICMP echo/reply, TCP, UDP) are dynamically forwarded over the gRPC `Wire` stream between `bridge-server` and `bridge-client`.

## Declaring a Bridge

A `FORWARD` node describes the wires it terminates, not the daemon flags it needs;
KNE derives the flags. Each wire has two endpoints, `a` and `z`, and the endpoint
written as an `interface` is always the declaring node:

- `a: { interface: ... }` — this node dials the peer named by `z`.
- `z: { interface: ... }` — this node listens, and the peer dials in. If `a` is
  omitted the client is outside the topology entirely, which is how an external
  process such as a Borg job attaches.

A peer inside the cluster is named with `local_node`, and KNE gives every
`FORWARD` node a headless Service under its own name so that name resolves. A
peer outside the cluster is named with `remote_node` and an address; the wire
port is assumed if the address has none.

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

4. **Propagate Link State (Optional):**

   The bridge carries link state as well as frames. Taking `host1`'s `eth1`
   down removes carrier from `host2`'s `eth1`, and the reverse:

   ```bash
   kubectl exec -it host1 -- ip link set eth1 down
   kubectl exec -it host2 -- ip link show eth1   # NO-CARRIER, no LOWER_UP
   kubectl exec -it host1 -- ip link set eth1 up
   kubectl exec -it host2 -- ip link show eth1   # LOWER_UP again
   ```

   See [Link State](#link-state) for how this works.

5. **Teardown:**

   ```bash
   kne delete examples/bridge/paired-bridge.pb.txt
   ```

---

## Link State

A bridge propagates link state using the carrier semantics of a veth pair: an
interface has carrier (`LOWER_UP`) only while the device at the other end of its
link is administratively up.

- When the carrier of a bridge's interface changes while the interface is
  administratively up, the bridge sends `LINK_STATE_UP` or `LINK_STATE_DOWN`
  in `wire.Packet.link_state`. It also sends the current state when a stream
  is established.
- When a bridge receives `LINK_STATE_UP` or `LINK_STATE_DOWN`, it sets its
  interface administratively up or down, and the device at the other end of
  the link sees that as a carrier change.
- A bridge reports nothing while its interface is administratively down. That
  carrier loss was caused by the bridge itself, and reporting it would latch
  both ends down. When the interface comes back up, the bridge reports its
  carrier right away.

A `wire.Packet` may carry a frame (`data`), a link-state update, or both.
External clients of the Wire service, such as a packet bridge outside the
cluster, follow the same rules. Clients that ignore `link_state` keep working
and simply don't propagate link state.

Within a node, meshnet links are veth pairs, so this works on any cluster. A
link between pods on different nodes has these semantics only with meshnet's
gRPC link type (`INTER_NODE_LINK_TYPE=GRPC`, the default in the KNE deployment
configs). It does not work with the VXLAN link type.

> [!WARNING]
> Link-state propagation assumes that at least one side of the Wire is not
> another bridge. In a paired bridge like this example, where both ends are
> bridges in front of veth-like links, the link can latch down. That happens if
> the devices on both sides go administratively down within one propagation
> delay of each other: each bridge takes its own interface down, and a downed
> interface cannot see its far end come back up. Bring the bridge interfaces
> back up by hand (`ip link set <interface> up`) to recover.

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

   Find the worker node IP and NodePort exposing `bridge-server`'s wire port via
   `kne topology service examples/bridge/paired-bridge.pb.txt`, then run:

   ```bash
   sudo kne bridge client --peer=<NODE_IP>:<NODE_PORT> --interface=veth-kne
   ```

3. **Ping directly from the host:**

   ```bash
   ping -I veth-host 192.168.1.2
   ```
