# Installation

* Create VM

* Install Docker

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

* Install Kubernetes

* Install Kind

```bash
GO111MODULE="on" go get sigs.k8s.io/kind@v0.10.0
```

* Create Cluster

```bash
kind create cluster --name kne
```

* Load images into local docker

```bash
kind load docker-image networkop/meshnet:latest --name=kne
kind load docker-image ubuntu:latest --name=kne
kind load docker-image ceos:latest --name=kne
kind load docker-image ixia:latest --name=kne
kind load docker-image evo:latest --name=kne
kind load docker-image ios-xr:latest --name=kne
```

* Add MeshNet

```bash

git clone git@github.com:networkop/meshnet-cni.git
cd meshnet-cni
kubectl apply -k /manifests/base

```

* Validate meshnet is running

```bash
marcus@muerto:~/src/meshnet-cni/manifests/base$ kubectl get daemonset -n meshnet
NAME      DESIRED   CURRENT   READY   UP-TO-DATE   AVAILABLE   NODE SELECTOR                   AGE
meshnet   1         1         1       1            1           beta.kubernetes.io/arch=amd64   3h2m

```

* Add MetalLB (if you want to access pods outside of cluster)
   * Get metallb Loadbalancer

```bash
kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/master/manifests/namespace.yaml
kubectl create secret generic -n metallb-system memberlist --from-literal=secretkey="$(openssl rand -base64 128)" 
kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/master/manifests/metallb.yaml
```

   * Validate metallb is running

```bash
kubectl get pods -n metallb-system --watch
```

   * Setup pool

```bash
docker network inspect -f '{{.IPAM.Config}}' kind
```

Will return something like:

```
[{192.168.18.0/24  192.168.18.1 map[]} {fc00:f853:ccd:e793::/64   map[]}]
```

   * Create ConfigMap for subnet

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

* Build kne_cli

```
cd kne_cli
go build
```

* Create Topology

```
kne_cli create ../examples/3node-host.pb.txt
```

