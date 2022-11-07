# How to: verify and use the services on cPTX

This examples shows about how to verify and use the gRPC services on the cPTX running in the KNE cluster.
Prerequisite: follow the KNE docs [docs/README.md](https://github.com/openconfig/kne/tree/main/docs) to bring up a cluster using the KNE config `cptx-ixia.pb.txt`.
This example config shows how to connect 3 of cPTX's channelized ports with 3 of continerized ixia's (otg) ports.
Following this example a cPTX can be connected to another cPTX or any other container supported by KNE.

```bash
cPTX                    otg
----                    ---
et-0/0/1:0 (eth12) <--> eth1
et-0/0/2:0 (eth20) <--> eth2
et-0/0/3:0 (eth28) <--> eth3

```

## Get into cPTX and check the status of gRPC services

Once the KNE cluster is up, check details on the k8s services and pods and get into cPTX by launching cli.
Take a note of k8s external ip and outside port (from `cptx-ixia.pb.txt`) for dialing gRPC services.

```bash
$ kubectl get pods -A
NAMESPACE            NAME                                            READY   STATUS    RESTARTS   AGE
ixia-c               cptx                                            1/1     Running   0          3h20m
ixia-c               otg-controller                                  2/2     Running   0          3h20m
-- snip --

$ kubectl get services -A
NAMESPACE          NAME                                           TYPE           CLUSTER-IP      EXTERNAL-IP    PORT(S)                                                     AGE
default            kubernetes                                     ClusterIP      10.96.0.1       <none>         443/TCP                                                     35h
ixia-c             service-cptx                                   LoadBalancer   10.96.165.219   172.18.0.100   9337:31221/TCP,9339:32478/TCP,9340:30345/TCP,22:31347/TCP   10h
ixia-c             service-gnmi-otg-controller                    LoadBalancer   10.96.32.10     172.18.0.101   50051:31917/TCP                                             10h
-- snip --

$ kubectl exec -it -n <NAMESPACE> cptx -- cli
$ kubectl exec -it -n ixia-c cptx -- cli
root@cptx>
```

## Check the status of gRPC services

Check the status of gRPC services

```bash
root@cptx> show system connections |match 32767
tcp6       0      0 :::32767                :::*                    LISTEN      XXXXX/jsd

root@cptx>
root@cptx> show configuration system services extension-service
request-response {
    gRPC {
        ssl {
            hot-reloading;
            use-pki;
        }
    }
}
root@cptx>
```

## Dial gNMI service using external service ip and outside port

```bash
$ gnmic -a 172.18.0.100:9339 -u root -p Google123 capabilities --skip-verify
gNMI version: 0.7.0
supported models:
  - ietf-yang-metadata, IETF NETMOD (NETCONF Data Modeling Language) Working Group, 2016-08-05
  - junos-configuration-metadata, Juniper Networks, Inc., 2021-09-01
  - junos-rpc-auto-bandwidth, Juniper Networks, Inc., 2019-01-01
-- snip --

$ gnmic -a 172.18.0.100:9339 -u root -p Google123 --encoding json_ietf   get --path "/system" --skip-verify
[
  {
    "source": "172.18.0.100:1001",
    "timestamp": 1667605170991199250,
    "time": "2022-11-04T23:39:30.99119925Z",
    "updates": [
      {
        "Path": "openconfig:system",
        "values": {
          "system": {
            "gRPC-servers": {
              "gRPC-server": [
                {
                  "config": {
                    "certificate-id": "ca-ipsec",
                    "enable": true,
                    "listen-addresses": [
                      "0.0.0.0"
                    ],
                    "port": 32767,
                    "services": [
                      "openconfig-system-gRPC:GNMI"
                    ],
                    "transport-security": true
                  },
                  "counters": {
                    "counter": [
                      {
                        "service": "gRPC-counter"
                      }
                    ]
                  },
                  "name": "gRPC-server"
                }
              ]
            }
          }
        }
      }
    ]
  }
]
```

## Dial gNOI service using external service ip and outside port

```bash
$ gnoic -a 172.18.0.100:9337 --skip-verify -u root -p Google123 file stat --path /etc/config/junos-factory.conf
+-------------------+--------------------------------+----------------------+------------+------------+------+
|    Target Name    |              Path              |     LastModified     |    Perm    |   Umask    | Size |
+-------------------+--------------------------------+----------------------+------------+------------+------+
| 172.18.0.100:9337 | /etc/config/junos-factory.conf | 2022-11-04T18:53:27Z | -rw-r--r-- | -----w--w- | 155  |
+-------------------+--------------------------------+----------------------+------------+------------+------+
```

## Dial gRIBI service using external service ip and outside port

```bash
$ gribic -a 172.18.0.100:9340 -u root -p Google123 --skip-verify get --ns default --aft ipv4
INFO[0000] target 172.18.0.100:9340: final get response: entry:{network_instance:"DEFAULT" ipv4:{prefix:"198.51.100.0/24" ipv4_entry:{next_hop_group:{value:42}}} 11:1 12:0} entry:{network_instance:"DEFAULT" ipv4:{prefix:"203.0.113.1/32" ipv4_entry:{next_hop_group:{value:52}}} 11:1 12:0}
INFO[0000] got 1 results
INFO[0000] "172.18.0.100:9340":
entry: {
  network_instance: "DEFAULT"
  ipv4: {
    prefix: "198.51.100.0/24"
    ipv4_entry: {
      next_hop_group: {
        value: 42
      }
    }
  }
  11: 1
  12: 0
}
entry: {
  network_instance: "DEFAULT"
  ipv4: {
    prefix: "203.0.113.1/32"
    ipv4_entry: {
      next_hop_group: {
        value: 52
      }
    }
  }
  11: 1
  12: 0
}

```

## Notes

- cPTX can be configured in a channelized or non-channelized mode.
- cPTX will be started in channelized mode if any of the interfaces in the interface mapping of KNE config are channelized.
- cPTX ethernet interfaces to software wire interface mapping (channelized). Follow the `juniper.config` for more info. Here is an example.
    ```bash
    et-0/0/0:0 (eth4)
    et-0/0/0:1 (eth5)
    -- snip --
    et-0/0/1:0 (eth12)
    et-0/0/1:1 (eth13)
    -- snip --
    et-0/0/2:0 (eth20)
    -- snip --
    et-0/0/3:0 (eth28)
    et-0/0/4:0 (eth36)
    et-0/0/5:0 (unused)
    et-0/0/6:0 (eth40)
    -- snip --
    et-0/0/7:0 (unused)
    -- snip --
    et-0/0/11:0 (eth68)
    ```
- cPTX ethernet interfaces to software wire interface mapping (non-channelized). Here is an example.
    ```bash
    et-0/0/0 (eth4)
    et-0/0/1 (eth5)
    et-0/0/2 (eth6)
    -- snip --
    et-0/0/5 (unused)
    et-0/0/6 (eth8)
    et-0/0/7 (unused)
    et-0/0/8 (eth10)
    -- snip --
    et-0/0/11 (eth15)
    ```
- Pass gRPC client option `-skip-verify` as only self-signed TLS certificates are configured as of today.
