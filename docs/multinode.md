# KNE with a Multi Node Cluster

## Background

A k8s cluster is made up of 1 or more nodes. Each node can hold up to 110 pods.
See the official large cluster considerations
[here](https://kubernetes.io/docs/setup/best-practices/cluster-large/). An
emulated DUT in KNE brings up 1 pod. An emulated ATE in KNE brings up 1 pod per
port. Together with the controller pods and other dependency pods, this in turn
restricts a KNE user using kind (a single node cluster) to less than ~100 DUTs +
ATE ports. For large testbeds, this is not an acceptable restriction.
Additionally each device requires CPU and other resources shared from the host.

A multi-node KNE cluster setup addresses these limitations through a
controller + worker(s) setup spread across multiple VMs.

![cluster nodes](https://d33wubrfki0l68.cloudfront.net/283cc20bb49089cb2ca54d51b4ac27720c1a7902/34424/docs/tutorials/kubernetes-basics/public/images/module_01_cluster.svg)

In this cluster diagram we can see that there is a single central `Control
Plane` along with 3 `Nodes`.

This guide will show you how to use KNE on an existing multi-node cluster, as
well as provide steps to setup a multi-node cluster on GCP.

### External cluster type

The `kne deploy` command is used to setup a cluster as well as ingress, CNI, and
vendor controllers inside the cluster. For a multi-node cluster we will be using
the `External` cluster type in the deployment config.

`External` is essentially a no-op cluster type. It is assumed a k8s cluster has
already been deployed. In this case, KNE does no cluster lifecycle management.
KNE only setups the dependencies. This guide will show you how to utilize the
`External` cluster type option to get KNE up an running on a multi-node cluster.

The `kne` CLI will be run on the host of the controller VM and the created
topology will be automatically provisioned across the worker nodes depending on
available resources on each. This setup can easily be scaled up by adding more
worker VMs with increased resources (CPU, etc.).

To conclude this guide we will bring up a
[150 node topology](https://github.com/openconfig/kne/blob/main/examples/arista/ceos-150/ceos-150.pb.txt)
in our multi-node cluster.

## Create a topology in a multi-node cluster

This guide assumes a multi-node cluster has already been set up. The cluster
should adhere to these restrictions:

- Use a pod networking add-on [compatible with MetalLB](https://metallb.universe.tf/installation/network-addons/)
- Use dockerd as the [CRI](https://kubernetes.io/docs/setup/production-environment/tools/kubeadm/install-kubeadm/#installing-runtime)

The following optional section shows how to create a multi-node cluster using
`kubeadm` on GCP. Skip directly to the
[topology creation step](#topology-creation) if your existing cluster adheres to
the above guidelines.

### Cluster setup

Using GCP we will create a 3 VM setup, 1 VM serving as the `Control Plane` + 2
VMs each serving as a worker `Node`.

#### VPC

A custom VPC is required to handle to k8s routing between the VMs. The following
commands will set up a custom VPC with a known CIDR range in an existing GCP
project.

```shell
gcloud compute networks create multinode --subnet-mode custom
gcloud compute networks subnets create multinode-nodes \
  --network multinode \
  --range 10.240.0.0/24 \
  --region us-central1
gcloud compute firewall-rules create multinode-allow-internal \
  --allow tcp,udp,icmp,ipip \
  --network multinode \
  --source-ranges 10.240.0.0/24
gcloud compute firewall-rules create multinode-allow-external \
  --allow tcp:22,tcp:6443,icmp \
  --network multinode \
  --source-ranges 0.0.0.0/0
```

#### VM Instances

We run the official production k8 solution `kubeadm` to create our cluster. We
chose `dockerd` as our Container Runtime Interface
([CRI](https://kubernetes.io/docs/setup/production-environment/tools/kubeadm/install-kubeadm/#installing-runtime))
and `flannel` as our
[pod networking add-on](https://kubernetes.io/docs/concepts/cluster-administration/addons/#networking-and-network-policy).
These tools are already installed on the KNE VM image built on ubuntu. The
created VMs in the next step will use this base image to reduce the manual setup
required.

Import the KNE VM image:

```shell
gcloud compute images import kne-f109f429-0573-4b9c-a75a-b6603f6830ae \
  --os=ubuntu-2004 \
  --source-file=gs://kne-vm-image/f109f429-0573-4b9c-a75a-b6603f6830ae.tar.gz
```

Create an SSH key pair to use for all of the VMs created below:

```shell
ssh-keygen -f /tmp/multinode-key -C user -N ""
sed -i '1s/^/user:/' /tmp/multinode-key.pub
```

##### Controller

Create the controller VM using the `gcloud` CLI. Note that the controller VM is
assigned the internal IP address `10.240.0.11` in the custom VPC.

```shell
gcloud compute instances create controller \
  --zone=us-central1-a \
  --image=kne-f109f429-0573-4b9c-a75a-b6603f6830ae \
  --machine-type=n2-standard-8 \
  --enable-nested-virtualization \
  --scopes=https://www.googleapis.com/auth/cloud-platform \
  --metadata-from-file=ssh-keys=/tmp/multinode-key.pub \
  --can-ip-forward \
  --private-network-ip 10.240.0.11 \
  --subnet multinode-nodes
```

SSH to the VM:

```shell
ssh -i /tmp/multinode-key user@<EXTERNAL IP OF VM>
```

Now run the following commands to setup the cluster:

```shell
sudo apt install kubelet=1.25.0-00 kubectl=1.25.0-00 kubeadm=1.25.0-00 --allow-downgrades -y
sudo kubeadm init --cri-socket unix:///var/run/cri-dockerd.sock --pod-network-cidr 10.244.0.0/16
mkdir -p $HOME/.kube
sudo cp -i /etc/kubernetes/admin.conf $HOME/.kube/config
sudo chown $(id -u):$(id -g) $HOME/.kube/config
```

Setup the flannel pod networking add-on:

```shell
kubectl apply -f flannel/Documentation/kube-flannel.yml
```

##### Workers

For the purposes of this CodeLab, create 2 worker VMs. Run this command 2 times
replacing `{n}` with `1` and `2`.

```shell
gcloud compute instances create worker-{n} \
  --zone=us-central1-a \
  --image=kne-f109f429-0573-4b9c-a75a-b6603f6830ae \
  --machine-type=n2-standard-64 \
  --enable-nested-virtualization \
  --scopes=https://www.googleapis.com/auth/cloud-platform \
  --metadata-from-file=ssh-keys=/tmp/multinode-key.pub \
  --can-ip-forward \
  --private-network-ip 10.240.0.2{n} \
  --subnet multinode-nodes
```

SSH to each VM:

```shell
ssh -i /tmp/multinode-key user@<EXTERNAL IP OF VM>
```

And run the following command, using the token and SHA output from cluster setup
on the controller VM:

```shell
sudo apt install kubelet=1.25.0-00 kubectl=1.25.0-00 kubeadm=1.25.0-00 --allow-downgrades -y
sudo kubeadm join 10.240.0.11:6443 \
  --token {token} \
  --discovery-token-ca-cert-hash sha256:{sha} \
  --cri-socket unix:///var/run/cri-dockerd.sock
```

### Topology Creation

SSH to the VM acting as the controller. Confirm that the worker nodes all
successfully joined the cluster:

```shell
$ kubectl get nodes
NAME         STATUS   ROLES           AGE     VERSION
controller   Ready    control-plane   4m37s   v1.25.4
worker-1     Ready    <none>          134s    v1.25.4
worker-2     Ready    <none>          110s    v1.25.4
```

Create a new docker network for use in the cluster:

```shell
docker network create multinode
```

Now deploy the KNE dependencies (CNI, ingress, vendor controllers):

```shell
kne deploy kne/deploy/kne/external-multinode.yaml
```

> IMPORTANT: Contact Arista to get access to the cEOS image.

Create the 150 node cEOS topology:

```shell
kne create kne/examples/arista/ceos-150/ceos-150.pb.txt
```

Open a second terminal on the controller VM to track the topology creation
progress:

```shell
$ kubectl get pods -n ceos-150 -o wide --watch
NAME   READY   STATUS     RESTARTS   AGE     IP             NODE       NOMINATED NODE   READINESS GATES
r1     1/1     Running    0          9m22s   10.244.1.66    worker-1   <none>           <none>
r10    0/1     Init:0/1   0          9m43s   10.244.1.59    worker-2   <none>           <none>
r100   0/1     Init:0/1   0          8m17s   10.244.1.91    worker-1   <none>           <none>
r101   1/1     Running    0          8m36s   10.244.1.84    worker-1   <none>           <none>
r102   0/1     Init:0/1   0          9m4s    10.244.1.72    worker-2   <none>           <none>
r103   0/1     Pending    0          5m20s   <none>         <none>     <none>           <none>
...
```

This command will show all of the pods with additional information including
with worker node they are running on. If any of the pods are stuck in a
`Pending` state for a long time with no `NODE` assigned, see the troubleshooting
[section](#too-many-pods).

Once the `kne create` command completes with success, your topology is ready.
Confirm by checking all nodes are `Running` using `kubectl get pods -n
ceos-150`.

Verify that a gRPC connection can be made to a pod on one of the worker VMs, the
service external IP can be determined from `kubectl get services -n ceos-150`:

TIP: `gnmi_cli` can be installed by running `go install
github.com/openconfig/gnmi/cmd/gnmi_cli@latest` if the CLI is missing from the
VM.

```shell
export GNMI_USER=admin
export GNMI_PASS=admin
gnmi_cli -a <service external ip>:6030 -q "/interfaces/interface/state" -tls_skip_verify -with_user_pass
```

## Troubleshooting

### Too many pods

If the topology being deployed has too many pods for number of worker nodes, you
may see a warning event like below when waiting for a `Pending` pod with no
assigned node:

```shell
$ kubectl describe pods r25 -n ceos-150
...
Events:
  Type     Reason            Age   From               Message
  ----     ------            ----  ----               -------
  Warning  FailedScheduling  34s   default-scheduler  0/2 nodes are available: 1 Too many pods, 1 node(s) had untolerated taint {node-role.kubernetes.io/control-plane: }. preemption: 0/2 nodes are available: 1 No preemption victims found for incoming pod, 1 Preemption is not helpful for scheduling.
```

To fix this, add another worker VM and run the necessary commands to have it
join the cluster. Then without any further action on the controller, the
assignments should resolve themselves.

### Service external IP pending

If you see some services with a `<pending>` EXTERNAL-IP, then your MetalLB
configuration does not have enough IPs to assign. This codelab uses a
[configuration](https://github.com/openconfig/kne/blob/main/deploy/kne/external-multinode.yaml)
with a pool of 200 unique external IP addresses. If your topology has more than
200 nodes then this issue will be seen. Increase the value to accommodate your
number of nodes, and redeploy.

```shell
$ kubectl get services -n ceos-150
NAME           TYPE           CLUSTER-IP       EXTERNAL-IP    PORT(S)                                     AGE
service-r1     LoadBalancer   10.106.154.237   172.18.0.110   6030:31407/TCP,22:30550/TCP,443:30990/TCP   29m
service-r10    LoadBalancer   10.98.147.137    172.18.0.103   6030:31914/TCP,22:31900/TCP,443:30555/TCP   30m
service-r100   LoadBalancer   10.101.4.72      172.18.0.135   6030:32364/TCP,22:30243/TCP,443:32621/TCP   28m
service-r101   LoadBalancer   10.98.45.234     172.18.0.128   6030:32515/TCP,22:30159/TCP,443:32329/TCP   29m
service-r102   LoadBalancer   10.107.163.133   172.18.0.116   6030:32344/TCP,22:30100/TCP,443:31541/TCP   29m
service-r103   LoadBalancer   10.96.185.215    <pending>      6030:31092/TCP,22:30549/TCP,443:32424/TCP   25m
...
```
