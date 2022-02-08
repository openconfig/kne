# Installation

## Use kne_cli deploy with configuration yaml

You will still need the following installed see below:

* docker
* kubectl
* kind

```bash
kne_cli deploy deploy/kne/kind.yaml
```

The new deployment command will perform all the below operations in a single call based on a yaml file you can customize.

* Deploy kind cluster
  * name will be the name of the kind cluster
  * recycle will allow an existing cluster to be deployed on
  * version is the kind version
  * image is the node image to pull
* Install Metallb
  * Based on manifests/metallb checked in configurations
  * Read docker configuration and generate proper configuration file
  * Wait for deployment to complete
* Install Meshnet
  * Based on manifests/meshnet check in configurations
  * Wait for pods to be healthy

* From start to finish this will take ~1 min to bring up the cluster.
* After the deployment is finished additional steps might be required:
  * Import container images to the cluster image store (unless an image is available for pulling)
  * Install controllers for nodes which are managed externally

* Once you have loaded any specific controllers or images your topology requires

```bash
kne_cli create <topology file>
```

## Install GO

```bash
curl -LO https://golang.org/dl/go1.17.1.linux-amd64.tar.gz
tar -xzf go1.17.1.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo mv go /usr/local

```

## Install Docker

```bash
$ sudo apt-get update

$ sudo apt-get install \
    apt-transport-https \
    ca-certificates \
    curl \
    gnupg-agent \
    software-properties-common

$ curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo apt-key add -

$ sudo add-apt-repository \
   "deb [arch=amd64] https://download.docker.com/linux/ubuntu \
   $(lsb_release -cs) \
   stable"

 $ sudo apt-get update
 $ sudo apt-get install docker-ce docker-ce-cli containerd.io
```

## [Install Kubernetes](https://kubernetes.io/docs/tasks/tools/install-kubectl-linux/#install-kubectl-on-linux)

```bash
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
chmod +x kubectl
mkdir -p ~/go/bin
mv kubectl ~/go/bin/kubectl
```

## Install Kind

```bash
GO111MODULE="on" go get sigs.k8s.io/kind
```

## Create Cluster

```bash
kind create cluster --name kne
```

## Load images into local docker

```bash
kind load docker-image networkop/meshnet:latest --name=kne
kind load docker-image networkop/init-wait:latest --name=kne
kind load docker-image alpine:latest --name=kne
kind load docker-image ubuntu:latest --name=kne
kind load docker-image ceos:latest --name=kne
kind load docker-image ixia:latest --name=kne
kind load docker-image evo:latest --name=kne
kind load docker-image ios-xr:latest --name=kne
```

## Add MeshNet

### Clone repo

```bash
git clone git@github.com:h-fam/meshnet-cni.git
cd meshnet-cni
kubectl apply -k manifests/base
```

### Validate meshnet is running

```bash
marcus@muerto:~/src/meshnet-cni/manifests/base$ kubectl get daemonset -n meshnet
NAME      DESIRED   CURRENT   READY   UP-TO-DATE   AVAILABLE   NODE SELECTOR                   AGE
meshnet   1         1         1       1            1           beta.kubernetes.io/arch=amd64   3h2m
```

## Add MetalLB (if you want to access pods outside of cluster)

### Get metallb Loadbalancer

```bash
kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/master/manifests/namespace.yaml
kubectl create secret generic -n metallb-system memberlist --from-literal=secretkey="$(openssl rand -base64 128)" 
kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/master/manifests/metallb.yaml
```

### Validate metallb is running

```bash
kubectl get pods -n metallb-system --watch
```

### Setup pool

```bash
docker network inspect -f '{{.IPAM.Config}}' kind
```

Will return something like:

```
[{192.168.18.0/24  192.168.18.1 map[]} {fc00:f853:ccd:e793::/64   map[]}]
```

### Create ConfigMap for subnet

Replace \<address-range\> with something like: "192.168.18.100 - 192.168.18.250"

```
apiVersion: v1
kind: ConfigMap
metadata:
  namespace: metallb-system
  name: config
data:
  config: |
   address-pools:
    - name: default
      protocol: layer2
      addresses:
      - <address-range>
```

Example:

```
cat > ./metallb-configmap.yaml << EOF
apiVersion: v1
kind: ConfigMap
metadata:
  namespace: metallb-system
  name: config
data:
  config: |
   address-pools:
    - name: default
      protocol: layer2
      addresses:
      - 192.168.18.100 - 192.168.18.250

EOF
kubectl apply -f ./metallb-configmap.yaml
```

## Build kne_cli

```
cd kne_cli
go build
```

## Create Topology

```
kne_cli create ../examples/3node-host.pb.txt
```

## See your pods

```
$ kubectl get pods -A
NAMESPACE            NAME                                        READY   STATUS    RESTARTS   AGE
3node-ceos           r1                                          1/1     Running   0          5d3h
3node-ceos           r2                                          1/1     Running   0          5d3h
3node-ceos           r3                                          1/1     Running   0          5d3h
```

## See your services

```
$ kubectl get services -A
NAMESPACE     NAME         TYPE           CLUSTER-IP      EXTERNAL-IP      PORT(S)                      AGE
3node-ceos    service-r1   LoadBalancer   10.96.218.147   192.168.18.100   443:30001/TCP,22:30003/TCP   5d3h
3node-ceos    service-r2   LoadBalancer   10.96.182.229   192.168.18.101   443:30004/TCP,22:30006/TCP   5d3h
3node-ceos    service-r3   LoadBalancer   10.96.222.220   192.168.18.102   443:30007/TCP,22:30009/TCP   5d3h
```

## Example how to ssh to a node

* Push base config to node

```bash
kubectl exec -it -n 3node-ceos r1 -- Cli
enable
config t
username netops privilege 15 role root secret netops
write
```

* SSH to Pod based on external ip

```bash
ssh netops@<service ip>
```

* Example

```bash
ssh netops@192.168.18.100
(netops@192.168.18.100) Password: 
r1>show ip route

VRF: default
Codes: C - connected, S - static, K - kernel, 
       O - OSPF, IA - OSPF inter area, E1 - OSPF external type 1,
       E2 - OSPF external type 2, N1 - OSPF NSSA external type 1,
       N2 - OSPF NSSA external type2, B - BGP, B I - iBGP, B E - eBGP,
       R - RIP, I L1 - IS-IS level 1, I L2 - IS-IS level 2,
       O3 - OSPFv3, A B - BGP Aggregate, A O - OSPF Summary,
       NG - Nexthop Group Static Route, V - VXLAN Control Service,
       DH - DHCP client installed default route, M - Martian,
       DP - Dynamic Policy Route, L - VRF Leaked,
       RC - Route Cache Route

Gateway of last resort is not set


! IP routing not enabled
r1>
```


## Example on using grpcurl to access provided services

### Reflection example

```
grpcurl -insecure -H 'username:admin' -H 'password:admin' 192.168.16.51:6030 list
gnmi.gNMI
gnoi.certificate.CertificateManagement
gnoi.factory_reset.FactoryReset
gnoi.os.OS
gnoi.system.System
grpc.reflection.v1alpha.ServerReflection
```

```
grpcurl -insecure -H 'username:admin' -H 'password:admin' 192.168.16.51:6030 describe gnmi.gNMI
gnmi.gNMI is a service:
service gNMI {
  rpc Capabilities ( .gnmi.CapabilityRequest ) returns ( .gnmi.CapabilityResponse );
  rpc Get ( .gnmi.GetRequest ) returns ( .gnmi.GetResponse );
  rpc Set ( .gnmi.SetRequest ) returns ( .gnmi.SetResponse );
  rpc Subscribe ( stream .gnmi.SubscribeRequest ) returns ( stream .gnmi.SubscribeResponse );
}
```

### gNMI example

```
grpcurl -insecure -H 'username:admin' -H 'password:admin' -d @ 192.168.16.51:6030 gnmi.gNMI.Subscribe << EOM
{
  "subscribe": {
    "mode": 1,
    "subscription": {
      "path": {
        "elem": [
          {
            "name": "interfaces"
          },
          {
            "name": "interface",
            "key": {
              "name": "Ethernet1"
            }
          },
          {
            "name": "ethernet"
          },
          {
            "name": "state"
          },    
          {
            "name": "counters"
          }       
        ]
      }
    }
  }
}
EOM

```

#### Response
```
{
  "update": {
    "timestamp": "1643673996534934536",
    "prefix": {
      "elem": [
        {
          "name": "interfaces"
        },
        {
          "name": "interface",
          "key": {
            "name": "Ethernet1"
          }
        },
        {
          "name": "ethernet"
        },
        {
          "name": "state"
        },
        {
          "name": "counters"
        }
      ]
    },
    "update": [
      {
        "path": {
          "elem": [
            {
              "name": "in-crc-errors"
            }
          ]
        },
        "val": {
          "uintVal": "0"
        }
      },
      {
        "path": {
          "elem": [
            {
              "name": "in-fragment-frames"
            }
          ]
        },
        "val": {
          "uintVal": "0"
        }
      },
      {
        "path": {
          "elem": [
            {
              "name": "in-jabber-frames"
            }
          ]
        },
        "val": {
          "uintVal": "0"
        }
      },
      {
        "path": {
          "elem": [
            {
              "name": "in-mac-control-frames"
            }
          ]
        },
        "val": {
          "uintVal": "0"
        }
      },
      {
        "path": {
          "elem": [
            {
              "name": "in-mac-pause-frames"
            }
          ]
        },
        "val": {
          "uintVal": "0"
        }
      },
      {
        "path": {
          "elem": [
            {
              "name": "in-oversize-frames"
            }
          ]
        },
        "val": {
          "uintVal": "0"
        }
      },
      {
        "path": {
          "elem": [
            {
              "name": "out-mac-control-frames"
            }
          ]
        },
        "val": {
          "uintVal": "0"
        }
      },
      {
        "path": {
          "elem": [
            {
              "name": "out-mac-pause-frames"
            }
          ]
        },
        "val": {
          "uintVal": "0"
        }
      }
    ]
  }
}
{
  "syncResponse": true
}

```

### gRIBI example

#### Request

```
$ grpcurl -insecure -H 'username:admin' -H 'password:admin' -d @ 192.168.16.51:6031 gribi.gRIBI.Modify << EOM
{
    "params": {
        "redundancy": 1,
        "persistence": 1
    }
}
{
    "election_id": {
        "high": 1,
        "low": 1
    }
}   
{
    "operation": {
        "id": 1,
        "network_instance": "default",
        "op": 1,
        "next_hop": {
            "index": 1,
            "next_hop": {
                "ip_address": {
                    "value": "100.0.0.2"
                }
            }
        },
        "election_id": {
            "high": 1,
            "low": 1
          }
    }
}
EOM
```

### Response
```
{
  "sessionParamsResult": {
    
  }
}
{
  "electionId": {
    "high": "1",
    "low": "1"
  }
}
{
  "result": [
    {
      "id": "1",
      "status": "RIB_PROGRAMMED",
      "timestamp": "1644343687209115978"
    }
  ]
}
```


#### Request

```
grpcurl -insecure -H 'username:admin' -H 'password:admin' -d @ 192.168.16.51:6031 gribi.gRIBI.Get << EOM
{
  "name": "default",
  "aft": 1
}
EOM
```

#### Response
```
{
  "entry": [
    {
      "networkInstance": "default",
      "nextHop": {
        "index": "1",
        "nextHop": {
          "networkInstance": {
            "value": "default"
          },
          "ipAddress": {
            "value": "100.0.0.2"
          }
        }
      }
    }
  ]
}

```