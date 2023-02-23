# How to: verify and use the services on Cisco 8000e

## Check perquisites and create a kne topology using topology [8000e-ixia.pb.txt](8000e-ixia.pb.txt)

- Ensure you have a healthy kind cluster. Please refer [setup](../../../docs/setup.md) and [topology](../../../docs/create_topology.md) documents for the detailed instructions.
- Verify if nested virtualization is configured correctly by checking presence of /dev/kvm (`ls /dev/kvm`).
- Verify if Open vSwitch is installed by running `ovs-vswitchd  --version`.
- Set pid_max <= 1048575 using  `echo "kernel.pid_max=1048575" >> /etc/sysctl.conf` or `sysctl kernel.pid_max=1048575`.
- Create a KNE topology using `kne create path/to/8000e-ixia.pb.txt`

The following instructions assumes that a kne cluster is created using given topology in [8000e-ixia.pb.txt](8000e-ixia.pb.txt).

## Make sure the topology is healthy

- Make sure nodes are up and running by running command `kubectl get pods -A`. The output of the command should contain namespace `cisco-ixia` and 6 nodes with status `Running`. 4 otg ports (`otg-port-*`), one otg controller (`otg-controller`), and one cisco 8000e (`8000e`) are expected to be shown if the topology is created successfully.
  
``` bash
kubectl get pods  -A
NAMESPACE            NAME                                            READY   STATUS    RESTARTS   AGE
cisco-ixia           8000e                                           1/1     Running   0          4m28s
cisco-ixia           otg-controller                                  2/2     Running   0          4m28s
cisco-ixia           otg-port-eth1                                   2/2     Running   0          4m28s
cisco-ixia           otg-port-eth2                                   2/2     Running   0          4m28s
cisco-ixia           otg-port-eth3                                   2/2     Running   0          4m28s
cisco-ixia           otg-port-eth4                                   2/2     Running   0          4m27s
-- omitted -- 
 
```

- Make sure external ip are mapped correctly by running command `kubectl get services -n cisco-ixia`. It is expected an external ip is assigned to each of the six nodes mentioned above.  Also, the port mapping of the gnmi/gnoi/gribi/p4rt/ssh services for 8000e should match the port mapping in the topology file.  
  
``` bash
kubectl get services -n cisco-ixia
NAME                           TYPE           CLUSTER-IP      EXTERNAL-IP   PORT(S)                                                                    AGE
service-8000e                  LoadBalancer   10.96.195.26    172.18.0.50   22:31633/TCP,9339:31543/TCP,9340:30751/TCP,9337:30331/TCP,9559:31439/TCP   7m45s
service-gnmi-otg-controller    LoadBalancer   10.96.187.227   172.18.0.51   50051:31810/TCP                                                            7m45s
service-grpc-otg-controller    LoadBalancer   10.96.6.73      172.18.0.52   40051:31158/TCP                                                            7m45s
service-https-otg-controller   LoadBalancer   10.96.65.192    172.18.0.53   8443:30126/TCP                                                             7m45s
service-otg-port-eth1          LoadBalancer   10.96.70.21     172.18.0.54   5555:30373/TCP,50071:32694/TCP                                             7m45s
service-otg-port-eth2          LoadBalancer   10.96.166.2     172.18.0.55   5555:31205/TCP,50071:32376/TCP                                             7m45s
service-otg-port-eth3          LoadBalancer   10.96.108.38    172.18.0.56   5555:32396/TCP,50071:30361/TCP                                             7m45s
service-otg-port-eth4          LoadBalancer   10.96.24.228    172.18.0.57   5555:31664/TCP,50071:30416/TCP                                             7m44s
 ```

## Check 8000e status

- Based on the above output, you may use `ssh cisco@172.18.0.50` with user/pass (`cisco/cisco123`) to access cisco e8000 instance (`e8000`).

``` bash
ssh cisco@172.18.0.50 
Password: 
Last login: Thu Feb 23 05:03:48 2023 from 10.244.0.1

RP/0/RP0/CPU0:ios#  
```

- You can also use   `kubectl exec -it -n cisco-ixia  8000e --  telnet 0 60000` to get console access to the device.
  
``` bash
 kubectl exec -it -n cisco-ixia  8000e --  telnet 0 60000
Defaulted container "vxr" out of: vxr, init-vxr (init)
Trying 0.0.0.0...
Connected to 0.
Escape character is '^]'.

RP/0/RP0/CPU0:ios#
 ```

To check if the grpc is configured, you may use `show running-config grpc` after login to the router. By default grpc for 8000e is configured using tls without authentication (`insecure: false & skip_verify: true`).

``` bash
RP/0/RP0/CPU0:ios#show running-config grpc
Thu Feb 23 06:04:04.562 UTC
grpc
 dscp cs4
 port 57400
 max-streams 128
 max-streams-per-user 128
 address-family dual
 max-request-total 256
 max-request-per-user 32
!
RP/0/RP0/CPU0:ios#
```

## Test GNMI using external service ip and outside port

``` bash
gnmic -a 172.18.0.50:9339 -u cisco -p cisco123 capabilities --skip-verify
gNMI version: 0.8.0
supported encodings:
  - JSON_IETF
  - ASCII
  - PROTO
supported models:
  - openconfig-bgp-types, OpenConfig working group, 5.3.1
  - openconfig-bgp-errors, OpenConfig working group, 5.3.1
  -- omitted --

 gnmic -a 172.18.0.50:9339 -u cisco -p cisco123 --encoding json_ietf   get --path "/system/memory" --skip-verify
[
  {
    "source": "172.18.0.50:9339",
    "timestamp": 1677133935654197129,
    "time": "2023-02-23T06:32:15.654197129Z",
    "updates": [
      {
        "Path": "openconfig:system/memory",
        "values": {
          "system/memory": {
            "state": {
              "physical": "12354125824",
              "reserved": "0"
            }
          }
        }
      }
    ]
  }
]
 
```

## Test gNOI service using external service ip and outside port

``` bash
gnoic -a 172.18.0.50:9337 --skip-verify -u cisco -p cisco123 system ping --destination 44.44.44.44
100 bytes from 44.44.44.44: icmp_seq=1 ttl=255 time=3ns
100 bytes from 44.44.44.44: icmp_seq=2 ttl=255 time=1ns
100 bytes from 44.44.44.44: icmp_seq=3 ttl=255 time=1ns
100 bytes from 44.44.44.44: icmp_seq=4 ttl=255 time=1ns
100 bytes from 44.44.44.44: icmp_seq=5 ttl=255 time=1ns
---  ping statistics ---
5 packets sent, 5 packets received, 0.00% packet loss
round-trip min/avg/max/stddev = 1.000/1.000/3.000/1.000 ms
```

## Test gRIBI service using external service ip and outside port

``` bash
gribic -a 172.18.0.50:9340 -u cisco -p cisco --skip-verify flush  --ns DEFAULT 
INFO[0000] got 1 results                                
INFO[0000] "172.18.0.50:9340": timestamp: 1677161921484040943
result: OK 
$ 

```
