# IN_CLUSTER_PROXY Node Type

The `IN_CLUSTER_PROXY` node type is a specialized KNE node designed to act as a back-to-back proxy inside the cluster. It binds to a specific target node (e.g., a Device Under Test / DUT) over a single dedicated link and forwards configured ports to it using `socat`.

## Features

-   **Automatic Command Generation**: Automatically computes static IP address sizing for sidecar point-to-point subnets and generates `socat` listener scriptlets directly without writing bash boilerplate.
-   **Cross-Stack Support**: Transparently supports both point-to-point **IPv4 (`/31`)** and **IPv6 (`/127`)** addressing setups.
-   **Static Topology Integrity Verification**: Proactively verifies that `eth1` connects directly to the declared target node backplane before attempting to load or deploy.

## Node Constraints

To pass static validation, the node **must** meet the following conditions:

| Parameter | Constraint |
| :--- | :--- |
| **Interfaces** | Exactly one interface named `eth1`. |
| **Links** | `eth1` **must** link directly to the node specified in the `proxy-pool-for` label. |
| **Services** | Exactly one Service mapping must be provided inside `Services` map. |
| **Labels** | The node must have the `proxy-pool-for` label correctly populated. |

## Configuration Labels

| Label Key | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `proxy-pool-for` | String | **Yes** | Name of the target node this proxy is mediating. |
| `peer-ip` | IP | No (Opt-in) | IP address of the peer (DUT) connected over `eth1` (e.g. `192.168.100.1` or `2001:db8::1`). |
| `peer-prefix` | String | No (Opt-in) | Prefix length of the peer IP (e.g. `31` or `127`). |
| `target-port` | Integer | No (Opt-in) | Port on the peer node to forward proxy streams to. |

> **Note on Opt-in Automatic Setup**: 
> If **all three of** `peer-ip`, `peer-prefix`, and `target-port` are provided, the controller will automatically calculate the inverse IP (your side of the `/31` or `/127` link) and generate full commands addressing `socat`. If omitted, users must configure `command` and `args` in `.Config` structures manually.

---

## Example (Protobuf text format)

Below is an example of an `IN_CLUSTER_PROXY` node mediating a BGP lookup connection to node `cx1`.

```protobuf
nodes: {
  name: "bgp-proxy-1"
  vendor: IN_CLUSTER_PROXY
  labels: {
    key: "proxy-pool-for"
    value: "cx1"
  }
  labels: {
    key: "peer-ip"
    value: "192.168.100.1"
  }
  labels: {
    key: "peer-prefix"
    value: "31"
  }
  labels: {
    key: "target-port"
    value: "179"
  }
  services: {
    key: 1790
    value: { 
      name: "bgp-proxy" 
      inside: 1790 
    }
  }
}

links: {
  a_node: "bgp-proxy-1"
  a_int: "eth1"
  z_node: "cx1"
  z_int: "eth1"
}
```

In this example, the proxy container will automatically spin up to:
1.  Assign `192.168.100.0/31` to its `eth1` interface.
2.  Run `socat` forwarding any stream sent to port `1790` out to `192.168.100.1:179` across the wire.
