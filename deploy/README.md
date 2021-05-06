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
kubectl apply -f /manifests/base/meshnet.yml 

```

* Validate meshnet is running

```bash
marcus@muerto:~/src/meshnet-cni/manifests/base$ kubectl get daemonset -n meshnet
NAME      DESIRED   CURRENT   READY   UP-TO-DATE   AVAILABLE   NODE SELECTOR                   AGE
meshnet   1         1         1       1            1           beta.kubernetes.io/arch=amd64   3h2m

```


* Build for kne-topo

```

git clone https://github.com/networkop/meshnet-cni.git
docker pull networkop/meshnet:latest
kind load docker-image networkop/meshnet:latest --name=kne
kubectl apply -f meshnet-cni/manifests/base/meshnet.yml

```


* Validate service is ready

```
kubectl get pods -A | grep k8s-topo
default              k8s-topo-59fc575699-kr8ht                   1/1     Running   0          10d
```

* Create topology from CLI

```
kubectl exec -it deployment/k8s-topo -- k8s-topo --create examples/3node-host.yml
kubectl get pods -A
```

