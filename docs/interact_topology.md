# Interact with a KNE topology

This is part of the How-To guide collection. This guide covers how to interact
with a KNE topology after creation. All of these sections are optional and are
meant as a demonstration for various things that can be done with KNE. The
specific examples included assume the multivendor topology was created following
the [Create Topology How-To](create_topology.md#create-a-topology).

## Push config

The `kne topology push` command can be used to push configuration to a node in a
topology. If a config file was specified in the
[topology textproto](https://github.com/openconfig/kne/blob/main/examples/multivendor/multivendor.pb.txt#L10),
then an initial config will be pushed during topology creation and a manual push
is not required unless a config change is desired. For example:

```bash
$ kne topology push examples/multivendor/multivendor.pb.txt r1 examples/multivendor/r1.ceos.cfg
```

## SSH to pod

### Find the service external IP

Run `kubectl get services`:

```bash
$ kubectl get services -n multivendor
NAME                           TYPE           CLUSTER-IP      EXTERNAL-IP     PORT(S)                                      AGE
service-gnmi-otg-controller    LoadBalancer   10.96.179.48    192.168.11.55   50051:30901/TCP                              4m9s
service-grpc-otg-controller    LoadBalancer   10.96.33.245    192.168.11.56   40051:30449/TCP                              4m9s
service-https-otg-controller   LoadBalancer   10.96.215.225   192.168.11.54   443:32556/TCP                                4m9s
service-otg-port-eth1          LoadBalancer   10.96.82.37     192.168.11.58   5555:30886/TCP,50071:30286/TCP               4m9s
service-otg-port-eth2          LoadBalancer   10.96.204.154   192.168.11.59   5555:31326/TCP,50071:31860/TCP               4m9s
service-otg-port-eth3          LoadBalancer   10.96.136.253   192.168.11.60   5555:30181/TCP,50071:31619/TCP               4m9s
service-otg-port-eth4          LoadBalancer   10.96.205.227   192.168.11.57   5555:32636/TCP,50071:31247/TCP               4m9s
service-r1                     LoadBalancer   10.96.130.198   192.168.11.50   443:32101/TCP,22:32304/TCP,6030:32011/TCP    4m12s
service-r2                     LoadBalancer   10.96.107.2     192.168.11.51   443:31942/TCP,22:30785/TCP,57400:30921/TCP   4m11s
service-r3                     LoadBalancer   10.96.80.18     192.168.11.52   22:32410/TCP                                 4m11s
service-r4                     LoadBalancer   10.96.138.204   192.168.11.53   22:31932/TCP,50051:32666/TCP                 4m10s
```

In this case we will use `r1` as an example which corresponds to
`192.168.11.50`.

### Find the credentials

Ask the vendor of a given node to determine the default username/password. For this multivendor
example, the `r1` Arista node has username/password of `admin`/`admin`.

### SSH

```bash
$ ssh <username>@<service external ip>
```

Here is an example for node `r1`:

```bash
$ ssh admin@192.168.11.50
(admin@192.168.11.50) Password: <admin>
```

<details>
<summary>WARNING: You may need to configure your SSH config to allow SSHing without a proxy.</summary>

1.  Get the IP range used by KNE services:

    ```
    $ kubectl get services -n multivendor
    NAME                           TYPE           CLUSTER-IP      EXTERNAL-IP     PORT(S)                                      AGE
    service-gnmi-otg-controller    LoadBalancer   10.96.179.48    192.168.11.55   50051:30901/TCP                              4m9s
    service-grpc-otg-controller    LoadBalancer   10.96.33.245    192.168.11.56   40051:30449/TCP                              4m9s
    service-https-otg-controller   LoadBalancer   10.96.215.225   192.168.11.54   443:32556/TCP                                4m9s
    service-otg-port-eth1          LoadBalancer   10.96.82.37     192.168.11.58   5555:30886/TCP,50071:30286/TCP               4m9s
    service-otg-port-eth2          LoadBalancer   10.96.204.154   192.168.11.59   5555:31326/TCP,50071:31860/TCP               4m9s
    service-otg-port-eth3          LoadBalancer   10.96.136.253   192.168.11.60   5555:30181/TCP,50071:31619/TCP               4m9s
    service-otg-port-eth4          LoadBalancer   10.96.205.227   192.168.11.57   5555:32636/TCP,50071:31247/TCP               4m9s
    service-r1                     LoadBalancer   10.96.130.198   192.168.11.50   443:32101/TCP,22:32304/TCP,6030:32011/TCP    4m12s
    service-r2                     LoadBalancer   10.96.107.2     192.168.11.51   443:31942/TCP,22:30785/TCP,57400:30921/TCP   4m11s
    service-r3                     LoadBalancer   10.96.80.18     192.168.11.52   22:32410/TCP                                 4m11s
    service-r4                     LoadBalancer   10.96.138.204   192.168.11.53   22:31932/TCP,50051:32666/TCP                 4m10s
    ```

    In this case the IP range would be `192.168.11.*`.

1.  Edit your SSH config found at `~/.ssh/config` to include:

    ```
    Host 192.168.11.*
        UserKnownHostsFile /dev/null
        StrictHostKeyChecking no
        ProxyCommand none
    ```

</details>

## gNMI

### Verifying gNMI

While connected to a device, you can verify that gNMI is also running with `show
management security ssl profile` to confirm the profile is correct, and `show
management api gnmi` to confirm that gNMI is running properly.

```bash
$ kubectl exec -it -n 3node-ceos r1 -- Cli
r1>en
r1#show management security ssl profile
   Profile                      State
---------------------------- -----------
   octa-ssl-profile             valid
   ARISTA_DEFAULT_PROFILE       valid

r1#show management api gnmi
Octa: enabled
Transport: ssl
Enabled: yes
Server: running on port 6030, in default VRF
SSL profile: octa-ssl-profile
QoS DSCP: none
Authorization required: no
Notification timestamp: last change time
```

## OpenConfig services

### Enabling the services

Regardless of the vendor, the topology file should expose the service like the
following gNMI example:

```
nodes: {
    ...
    services: {
        key: 9339
        value: {
            name: "gnmi"
            inside: <VENDOR SPECIFIC gNMI PORT>
        }
    }
}
```

This will configure the node to expose `gnmi` on port `9339` externally
regardless of which port the gNMI server is running on inside the container.

<details>
<summary><h4>Arista</h3></summary>

gNMI is enabled for Arista node `r1` in the multivendor node by default.
Outlined below are the key pieces for configuring gNMI in general.

##### Config

Ensure the following snippet is included in the device config:

```
management api gnmi
   transport grpc default
      ssl profile octa-ssl-profile
   provider eos-native
!
management security
   ssl profile eapi
      tls versions 1.2
      cipher-list EECDH+AESGCM:EDH+AESGCM
      certificate gnmiCert.pem key gnmiCertKey.pem
   !
   ssl profile octa-ssl-profile
      certificate gnmiCert.pem key gnmiCertKey.pem
!
```

##### Topology

```
nodes: {
    ...
    type: ARISTA_CEOS
    vendor: ARISTA
    model: "ceos"
    os: "eos"
    config: {
        ...
        cert: {
            self_signed: {
                cert_name: "gnmiCert.pem",
                key_name: "gnmiCertKey.pem",
                key_size: 4096,
            }
        }
    }
    services: {
        key: 9339
        value: {
            name: "gnmi"
            inside: 6030
        }
    }
}
```

##### Verification

Open a `Cli` on an Arista node to confirm that gNMI is running properly:

```bash
$ kubectl exec -it -n multivendor r1 -- Cli
r1>en
r1#show management security ssl profile
   Profile                      State
---------------------------- -----------
   octa-ssl-profile             valid
   ARISTA_DEFAULT_PROFILE       valid

r1#show management api gnmi
Octa: enabled
Transport: ssl
Enabled: yes
Server: running on port 6030, in default VRF
SSL profile: octa-ssl-profile
QoS DSCP: none
Authorization required: no
Notification timestamp: last change time
```

</details>

<details>
<summary><h4>Cisco</h4></summary>

See the external 8000e with services
[README](https://github.com/openconfig/kne/blob/main/examples/cisco/8000e/README.md).

</details>

<details>
<summary><h4>Nokia</h4></summary>

See the external SR Linux
[guide](http://learn.srlinux.dev/tutorials/infrastructure/kne/srl-with-oc-services/).

</details>

<details>
<summary><h4>Juniper</h4></summary>

See the external cptx with services
[README](https://github.com/openconfig/kne/blob/main/examples/juniper/cptx-ixia/README.md).

</details>

### Using OpenConfig g* services

#### Using the CLI

<details>
<summary><h5>gNMI</h5></summary>

Install the `gNMIc` command line tool:

```bash
$ bash -c "$(curl -sL https://get-gnmic.openconfig.net)"
```

gNMI should be running on your nodes on port `9339`, so you can connect directly
using `gnmic`:

> TIP: The service external IP can be found using the [guide above](#find-the-service-external-ip).

> NOTE: The `--skip-verify` flag is important, because our self-signed keys cannot be verified.

```bash
$ gnmic subscribe -a <external-ip>:9339 --path /components --skip-verify -u <username> -p <password> --format flat
```

</details>

<details>
<summary><h5>gNOI</h5></summary>

Install the `gNOIc` command line tool:

```bash
$ bash -c "$(curl -sL https://get-gnoic.openconfig.net)"
```

gNOI should be running on your nodes on port `9337`, so you can connect directly
using `gnoic`:

> TIP: The service external IP can be found using the [guide above](#find-the-service-external-ip).

> NOTE: The `--skip-verify` flag is important, because our self-signed keys cannot be verified.

```bash
$ gnoic system time -a <external-ip>:9337 --skip-verify -u <username> -p <password>
```

</details>

<details>
<summary><h5>gRIBI</h5></summary>

Install the `gRIBIc` command line tool:

```bash
$ bash -c "$(curl -sL https://get-gribic.openconfig.net)"
```

gRIBI should be running on your nodes on port `9340`, so you can connect
directly using `gribic`:

> TIP: The service external IP can be found using the [guide above](#find-the-service-external-ip).

> NOTE: The `--skip-verify` flag is important, because our self-signed keys cannot be verified.

```bash
$ gribic -a <external-ip>:9340 --skip-verify -u <username> -p <password> get -ns DEFAULT -aft ipv4
```

</details>

#### Using Golang

> NOTE: This example uses gNMI, but the other services are very similar.

To dial in from Go (if you're not using Ondatra), you need to set your grpc
connection to skip verifying TLS certificates. Also, per the
[gNMI public specification](https://github.com/openconfig/reference/blob/master/rpc/gnmi/gnmi-specification.md#31-session-security-authentication-and-rpc-authorization),
gNMI expects that the username and password will be passed in via the metadata.
Golang usage therefore looks like:

```go
import (
    ...
    "google.golang.org/grpc"
    "google.golang.org/grpc/metadata"
    gpb "github.com/openconfig/gnmi/proto/gnmi"
    ...
)

func someFunc(ctx context.Context) {
    ctx = metadata.AppendToOutgoingContext(ctx, "username", "<username>", "password", "<password>")
    tls := &tls.Config{InsecureSkipVerify: true}
    tlsCred := credentials.NewTLS(tls)
    conn, err := grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(tlsCred))
    if err != nil { ... }
    gnmi := gpb.NewGNMIClient(conn)
    ...
}
```

#### Using Ondatra

> NOTE: This example uses gNMI, but the other services are very similar.

[KNEBind](https://github.com/openconfig/ondatra/blob/main/knebind/README.md) is
an Ondatra binding that uses the `kne` CLI to connect to an existing KNE cluster
and automatically finds appropriate devices within your topology.

To use the KNE binding, import it and pass it into `RunTests`:

```go
import (
    ...
    kinit "github.com/openconfig/ondatra/knebind/init"
    ...
)

func TestMain(m *testing.M) {
    ondatra.RunTests(m, kinit.Init)
}
```

and set the topology flag `--topology=<path>` to point to the topology file that
has been created in KNE.

ex.
`--topology=/path/to/examples/multivendor/multivendor.pb.txt`

Ondatra will manage a gNMI connection to each device, so you can use Ondatra's
helper functions to configure and read from the OpenConfig tree:

```go
import (
    "testing"

    "github.com/openconfig/ondatra"
    "github.com/openconfig/ondatra/gnmi"
    kinit "github.com/openconfig/ondatra/knebind/init"
)

func TestMain(m *testing.M) {
    ondatra.RunTests(m, kinit.Init)
}

func TestTrivial(t *testing.T) {
    // Get a DUT from the topology
    dut := ondatra.DUT(t, "dut")
    // Lookup the gNMI leaf value
    sys := gnmi.Lookup(t, dut, gnmi.OC().System().State())
    if !sys.IsPresent() {
        t.Fatalf("No System telemetry for %v", dut)
    }
}
```