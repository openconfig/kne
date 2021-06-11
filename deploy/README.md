# Installation

## Create VM

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

```
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
chmod +x kubectl
mv $(GOPATH)/bin/kubectl
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

git clone git@github.com:networkop/meshnet-cni.git
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

Replace <address-range> with something like: "192.168.18.100 - 192.168.18.250"

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

```$ kubectl get services -A
NAMESPACE     NAME         TYPE           CLUSTER-IP      EXTERNAL-IP      PORT(S)                      AGE
3node-ceos    service-r1   LoadBalancer   10.96.218.147   192.168.18.100   443:30001/TCP,22:30003/TCP   5d3h
3node-ceos    service-r2   LoadBalancer   10.96.182.229   192.168.18.101   443:30004/TCP,22:30006/TCP   5d3h
3node-ceos    service-r3   LoadBalancer   10.96.222.220   192.168.18.102   443:30007/TCP,22:30009/TCP   5d3h
```

## Example how to ssh to a node

* Push base config to node

```
kubectl exec -it -n 3node-ceos r1 -- Cli
enable
config t
username netops privilege 15 role root secret netops
write
```

* SSH to Pod based on external ip

```
ssh netops@<service ip>
```
* Example

```
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
