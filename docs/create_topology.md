# Create a KNE topology

This is part of the How-To guide collection. This guide covers KNE cluster
deployment and topology creation.

## Overview

Creating a topology takes 3 main steps:

1.  Cluster deployment

1.  Additional controller deployment

1.  Topology creation

## Deploy a cluster

The first step when using KNE is to deploy a kubernetes cluster. This can be
done using the `kne_cli deploy` command.

```bash
$ kne_cli help deploy
Deploy cluster.

Usage:
  kne_cli deploy <deployment yaml> [flags]

Flags:
  -h, --help   help for deploy

Global Flags:
      --kubecfg string     kubeconfig file (default "/usr/local/google/home/{{USERNAME}}/.kube/config")
  -v, --verbosity string   log level (default "info")
```

A deployment yaml file specifies 4 things (*optional in italics*):

1.  A cluster spec
1.  An ingress spec
1.  A CNI spec
1.  *A list of controller specs*

A full definition for valid fields in the deployment yaml can be found within
[deploy/deploy.go](https://github.com/openconfig/kne/blob/816133f1cb563555bcdcb12eb27874b77dd41d1d/deploy/deploy.go#L212).

The basic deployment yaml file can be found in the GitHub repo at
[deploy/kne/kind.yaml](https://github.com/openconfig/kne/blob/5e6cf1cbc0748bb48ebf49039bd0ad592378357a/deploy/kne/kind-bridge.yaml).

This config specifies `kind` as the cluster, `metallb` as the ingress, and
`meshnet` as the CNI. Additionally, the config instructs `kindnet` CNI to use `bridge` CNI, instead of a default `ptp`. This spec can be deployed using the following command:

```bash
$ kne_cli deploy deploy/kne/kind-bridge.yaml
```

## Deploying additional vendor controllers

Some vendors provide a controller that handles the pod lifecycle for their
nodes. These controllers need to be created **after** deploying a cluster and
**before** deploying a topology. Currently the following vendors use a
controller:

-   Keysight: `ixia-tg`
-   Nokia: `srlinux`

Currently the following vendor controllers can be deployed as part of
[cluster deployment](#deploy_a_cluster):

-   Keysight: `ixia-tg`

### IxiaTG

IMPORTANT: Contact Keysight to get access to the IxiaTG controller yaml and
container images.

TIP: IxiaTG does not have to be deployed manually, during
[cluster deployment](#deploy_a_cluster) the IxiaTG controller can be
automatically deployed if specified in the deployment yaml.

To manually apply the controller run the following command after cloning the
Keysight controller yaml:

```bash
$ kubectl apply -f ixiatg-operator.yaml
```

### SR Linux

Apply the latest version of SR Linux controller:

    ```bash
    kubectl apply -k https://github.com/srl-labs/srl-controller/config/default
    ```

Read more on Nokia SR Linux controller operations and capabilities at [srl-labs/srl-controller GitHub repo](https://github.com/srl-labs/srl-controller).

## Create a topology

After cluster deployment, a topology can be created inside of it. This can be
done using the `kne_cli create` command.

```bash
$ kne_cli help create
Create Topology

Usage:
  kne_cli create <topology file> [flags]

Flags:
      --dryrun             Generate topology but do not push to k8s
  -h, --help               help for create
      --timeout duration   Timeout for pod status enquiry

Global Flags:
      --kubecfg string     kubeconfig file (default "/usr/local/google/home/{{USERNAME}}/.kube/config")
  -v, --verbosity string   log level (default "info")
```

A topology file is a textproto of the `Topology`
[message](https://github.com/openconfig/kne/blob/df91c62eb7e2a1abbf0a803f5151dc365b6f61da/proto/topo.proto#L26).
This file specifies all of the nodes and links of your desired topology. In the
node definitions interfaces, services, and initial configs can be specified.

An example topology containing 3 Arista `cEOS` nodes and 2 Keysight `ixia-tg`
ATEs can be found at
[examples/3node-withtraffic.pb.txt](https://github.com/openconfig/kne/blob/df91c62eb7e2a1abbf0a803f5151dc365b6f61da/examples/3node-withtraffic.pb.txt).
The initial vendor router configs referenced in the topology are found
[here](https://github.com/openconfig/kne/blob/df91c62eb7e2a1abbf0a803f5151dc365b6f61da/examples/ceos-withtraffic/).
See the [push config](interact_topology.md#push_config) section for details
about pushing config after initial creation.

This topology can be created using the following command.

```bash
$ kne_cli create examples/3node-withtraffic.pb.txt
```

IMPORTANT: Wait for the command to fully complete, do not use Ctrl-C to cancel
the command. It is expected to take minutes depending on the topology and if
initial config is pushed.

### Container images

Container images can be hosted in multiple locations. For example
[DockerHub](https://hub.docker.com/) hosts open sourced containers.
[Google Artifact Registries](https://cloud.google.com/artifact-registry) can be
used to host images with access control. The
[KNE topology proto](https://github.com/openconfig/kne/blob/df91c62eb7e2a1abbf0a803f5151dc365b6f61da/proto/topo.proto#L117),
the manifests, and controllers can all specify containers that get pulled from
their source locations and get used in the cluster.

To load an image into a `kind` cluster there is a 3 step process:

1.  Pull the desired image:

    ```bash
    $ docker pull src_image:src_tag
    ```

1.  Tag the image with the desired in-cluster name:

    ```bash
    $ docker tag src_image:src_tag dst_image:dst_tag
    ```

1.  Load the image into the `kind` cluster:

    ```bash
    $ kind load docker-image dst_image:dst_tag --name=kne
    ```

Now the `dst_image:dst_tag` image will be present for use in the `kind` cluster.

NOTE: `ceos:latest` is the default image to use for a node of type
`ARISTA_CEOS`. This is a common image to manually pull if using a topology with
`ARISTA_CEOS` node and not specifying an image explicitly in your topology
textproto. Contact Arista to get access to the cEOS image.

You can check a full list of images loaded in your `kind` cluster using:

```bash
$ docker exec -it kne-control-plane crictl images
```

## Verify topology health

Check that all pods are healthy and `Running`:

```bash
$ kubectl get pods -A
NAMESPACE            NAME                                                    READY   STATUS    RESTARTS   AGE
3node-traffic        ate1                                                    2/2     Running   0          97s
3node-traffic        ate2                                                    2/2     Running   0          97s
3node-traffic        ixia-c                                                  3/3     Running   0          97s
3node-traffic        r1                                                      1/1     Running   0          99s
3node-traffic        r2                                                      1/1     Running   0          98s
3node-traffic        r3                                                      1/1     Running   0          99s
ixiatg-op-system     ixiatg-op-controller-manager-5f4d4dfb6-n9gvz            2/2     Running   0          3m14s
kube-system          coredns-78fcd69978-hhh9d                                1/1     Running   0          4m48s
kube-system          coredns-78fcd69978-zdwnw                                1/1     Running   0          4m48s
kube-system          etcd-kne-control-plane                                  1/1     Running   0          5m3s
kube-system          kindnet-vmvlf                                           1/1     Running   0          4m49s
kube-system          kube-apiserver-kne-control-plane                        1/1     Running   0          5m3s
kube-system          kube-controller-manager-kne-control-plane               1/1     Running   0          5m3s
kube-system          kube-proxy-lxb2q                                        1/1     Running   0          4m49s
kube-system          kube-scheduler-kne-control-plane                        1/1     Running   0          5m4s
local-path-storage   local-path-provisioner-85494db59d-vwkqm                 1/1     Running   0          4m48s
meshnet              meshnet-k8l75                                           1/1     Running   0          4m29s
metallb-system       controller-6d5fb97874-4pj9l                             1/1     Running   0          4m48s
metallb-system       speaker-f656g                                           1/1     Running   0          4m39s
srlinux-controller   srlinux-controller-controller-manager-fb7848755-wtc54   2/2     Running   0          2m13s
```

Check that all services appear as expected and have an assigned `EXTERNAL-IP`:

```bash
$ kubectl get services -n 3node-traffic
NAME             TYPE           CLUSTER-IP      EXTERNAL-IP     PORT(S)                                     AGE
gnmi-service     LoadBalancer   10.96.191.243   192.168.11.57   50051:30922/TCP                             4m
grpc-service     LoadBalancer   10.96.247.225   192.168.11.56   40051:31538/TCP                             4m
ixia-c-service   LoadBalancer   10.96.241.137   192.168.11.55   443:31583/TCP                               4m
service-ate1     LoadBalancer   10.96.68.104    192.168.11.51   5555:31580/TCP,50071:31365/TCP              4m2s
service-ate2     LoadBalancer   10.96.106.239   192.168.11.52   5555:32132/TCP,50071:32122/TCP              4m2s
service-r1       LoadBalancer   10.96.10.206    192.168.11.53   443:30941/TCP,22:30516/TCP,6030:32656/TCP   4m2s
service-r2       LoadBalancer   10.96.115.148   192.168.11.54   6030:32507/TCP,443:30996/TCP,22:30123/TCP   4m1s
service-r3       LoadBalancer   10.96.191.106   192.168.11.50   443:31680/TCP,22:32003/TCP,6030:31883/TCP   4m2s
```

If anything is unexpected check the [Troubleshooting](troubleshoot.md) guide.

## Clean up KNE

To delete a topology use `kne_cli delete`:

```bash
$ kne_cli delete examples/3node-withtraffic.pb.txt
```

To delete a cluster use `kind delete cluster`:

```bash
$ kind delete cluster --name=kne
```
