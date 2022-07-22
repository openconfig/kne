# Interact with a KNE topology

This is part of the How-To guide collection. This guide covers how to interact
with a KNE topology after creation.

## Push config

The `kne_cli topology push` command can be used to push configuration to a node
in a topology. For example:

```bash
kne_cli topology push examples/3node-ceos.pb.txt r1 examples/ceos-withtraffic/r1-config
```

TIP: Specify the config file in the
[topology textproto](https://github.com/openconfig/kne/blob/df91c62eb7e2a1abbf0a803f5151dc365b6f61da/examples/3node-withtraffic.pb.txt#L8)
so initial config will be pushed during topology creation.

## SSH to pod

### Configure access

TIP: This step is not needed if config with user/pass was already pushed.

Configuring access is vendor specific, the following is for an Arista `cEOS`
node `r1` in a topology called `3node-ceos`:

```bash
$ kubectl exec -it -n 3node-ceos r1 -- Cli
  enable
  config t
  username admin privilege 15 role root secret admin
  write
```

### Connect via the external IP

TIP: For the default configs found in the
[KNE GitHub repo](https://github.com/openconfig/kne/tree/main/examples) the
username/passwords can be found in the config files. Often times the password is
written as a hash. Try `admin` as a username or password if unknown.

```bash
ssh <username>@<service ip>
```

Here is an example based on the configured Arista `cEOS` node configured in the
previous section:

```bash
$ ssh admin@192.168.18.100
(admin@192.168.18.100) Password: <admin>

r1>show ip route

VRF: default Codes: C - connected, S - static, K - kernel, \
O - OSPF, IA - OSPF inter area, E1 - OSPF external type 1, \
E2 - OSPF external type 2, N1 - OSPF NSSA external type 1, \
N2 - OSPF NSSA external type2, B - BGP, B I - iBGP, B E - eBGP, \
R - RIP, I L1 - IS-IS level 1, I L2 - IS-IS level 2, \
O3 - OSPFv3, A B - BGP Aggregate, A O - OSPF Summary, \
NG - Nexthop Group Static Route, V - VXLAN Control Service, \
DH - DHCP client installed default route, M - Martian, \
DP - Dynamic Policy Route, L - VRF Leaked, \
RC - Route Cache Route \
Gateway of last resort is not set \
! IP routing not enabled \
```

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

### Using gNMI

Install a gNMI command line tool of your preference. We will be using the open
source `gnmi_cli` tool for the examples in this doc. Install by running the
following command:

```bash
go install github.com/openconfig/gnmi/cmd/gnmi_cli@latest
```

gNMI should be running on your nodes on the default port (6030), so you can
connect directly using `gnmi_cli`:

NOTE: The `--tls_skip_verify` flag is important, because our self-signed keys
cannot be verified.

```bash
export GNMI_USER=admin
export GNMI_PASS=admin
gnmi_cli -a 192.168.18.100:6030 -q "/interfaces/interface/state" -tls_skip_verify -with_user_pass
```

### gNMI over gRPC in go

To dial in from Go (if you're not using Ondatra), you need to set your grpc
connection to skip verifying TLS certificates. Also, per the
[gNMI public specification](https://github.com/openconfig/reference/blob/master/rpc/gnmi/gnmi-specification.md#31-session-security-authentication-and-rpc-authorization),
gNMI expects that the username and password will be passed in via the metadata.
Golang usage therefore looks like:

```go
import (
    "google3/third_party/golang/grpc/grpc"
    "google3/third_party/golang/grpc/metadata/metadata"
    ...
)

func someFunc(ctx context.Context) {
    ctx = metadata.AppendToOutgoingContext(ctx, "username", "somename", "password", "somepassword")
    tls := &tls.Config{InsecureSkipVerify: true}
    tlsCred := credentials.NewTLS(tls)
    conn, err := grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(tlsCred))
    if err != nil { ... }
    gnmi := gpb.NewGNMIClient(conn)
    ...
}
```

### gNMI over gRPC with Ondatra

[KNEBind](https://github.com/openconfig/ondatra/blob/main/knebind/knebind.go) is
implemented by using the `kne_cli` within the ondatra framework. Currently the
expectation is the user has already brought up a cluster and now wants to
develop tests against that cluster for use in vendor certification. See the
[README](https://github.com/openconfig/ondatra/blob/main/knebind/README.md)
for details.

Ondatra will manage a gNMI connection to each device, so you can use Ondatra's
helper functions to configure and read from the OpenConfig tree:

```go
import (
    "testing"

    "google3/ops/netops/lab/wbb/go/wbbtest"
    "google3/third_party/openconfig/ondatra/ondatra"
)

func TestMain(m *testing.M) {
    ondatra.RunTests(m, wbbtest.New)
}

func TestTrivial(t *testing.T) {
  // Get a DUT from the topology
  dut := ondatra.DUT(t, "dut1")
  // Set the config value of a node
  dut.Config().System().Hostname().Set("dut1")
  // Wait for a specific state value
    dut.Telemetry().System().Hostname().Await(t, time.Second, "dut1")
  // Read a state value directly
  hostname := dut.Telemetry().System().Hostname().Get(t)
  if hostname != "dut1" {
    t.Errorf("Expected hostname %v, got %v", "dut1", hostname)
  }
}
```

In the example above, `dut.Config().System().Hostname()` corresponds to the path
`system/config/hostname`, while `dut.Telemetry().System().Hostname()`
corresponds to path `system/state/hostname`. There is a small delay between when
the config is set and when the state catches up, hence the need for Await.
