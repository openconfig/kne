# Create a KNE topology

This is part of the How-To guide collection. This guide covers KNE cluster
deployment and topology creation.

## Overview

Creating a topology takes 3 main steps:

1. Cluster deployment
2. Additional controller deployment
3. Topology creation

## Deploy a cluster

The first step when using KNE is to deploy a Kubernetes cluster. This can be
done using the `kne deploy` command.

```bash
$ kne help deploy
Deploy cluster.

Usage:
  kne deploy <deployment yaml> [flags]

Flags:
  -h, --help   help for deploy

Global Flags:
      --kubecfg string     kubeconfig file (default "/usr/local/google/home/{{USERNAME}}/.kube/config")
  -v, --verbosity string   log level (default "info")
```

A deployment yaml file specifies 4 things (*optional in italics*):

1. A cluster spec
2. An ingress spec
3. A CNI spec
4. *A list of controller specs*

A full definition for valid fields in the deployment yaml can be found within
[deploy/deploy.go](https://github.com/openconfig/kne/blob/816133f1cb563555bcdcb12eb27874b77dd41d1d/deploy/deploy.go#L212).

Expand the below section for a full description of all fields in the deployment
yaml.

---

<details>
<summary><h3>Deployment yaml reference</h3></summary>

Field         | Type             | Description
------------- | ---------------- | ---------------------------------------------
`cluster`     | ClusterSpec      | Spec for the cluster.
`ingress`     | IngressSpec      | Spec for the ingress.
`cni`         | CNISpec          | Spec for the CNI.
`controllers` | []ControllerSpec | List of specs for the additional controllers.

### Cluster

Field  | Type      | Description
------ | --------- | ------------------------------------------------------
`kind` | string    | Name of the cluster type. The only option currently is`Kind`.
`spec` | yaml.Node | Fields that set the options for the cluster type.

#### Kind

Field                      | Type              | Description
-------------------------- | ----------------- | --------------------------
`name`                     | string            | Cluster name, overrides `KIND_CLUSTER_NAME`, config (default `kind`).
`recycle`                  | bool              | Reuse an existing cluster of the same name if it exists.
`version`                  | string            | Desired version of the `kubectl` client.
`image`                    | string            | Node docker image to use for booting the cluster.
`retain`                   | bool              | Retain nodes for debugging when cluster creation fails.
`wait`                     | time.Duration     | Wait for control plane node to be ready (default 0s).
`kubecfg`                  | string            | Sets kubeconfig path instead of `$KUBECONFIG` or `$HOME/.kube/config`.
`googleArtifactRegistries` | []string          | List of Google Artifact Registries to setup credentials for in the cluster. Example value for registry would be `us-west1-docker.pkg.dev`. Credentials used are associated with the configured `gcloud` user on the host.
`containerImages`          | map[string]string | Map of source images to target images for containers to load in the cluster. Empty values cause the source image to be loaded into the cluster without being renamed.
`config`                   | string            | Path to a kind config file.
`additionalManifests`      | []string          | List of paths to manifests to be applied using `kubectl` directly after cluster creation.

### Ingress

Field  | Type      | Description
------ | --------- | ------------------------------------------------------
`kind` | string    | Name of the ingress type. The only option currently is `MetalLB`.
`spec` | yaml.Node | Fields that set the options for the ingress type.

#### MetalLB

Field       | Type   | Description
----------- | ------ | -----------
`ip_count`  | int    | Number of IP addresses to include in the available pool.
`manifests` | string | Path of the directory holding the manifests to create MetalLB in the cluster. The directory is expected to contain a file with the name `metallb-native.yaml`. The validated manifest for use with KNE can be found [here](https://github.com/openconfig/kne/tree/main/manifests/metallb).

### CNI

Field  | Type      | Description
------ | --------- | --------------------------------------------------
`kind` | string    | Name of the CNI type. The only option currently is `Meshnet`.
`spec` | yaml.Node | Fields that set the options for the CNI type.

#### Meshnet

Field       | Type   | Description
----------- | ------ | -----------
`manifests` | string | Path of the directory holding the manifests to create Meshnet in the cluster. The directory is expected to contain a file with the name `kustomization.yaml`. The validated manifest for use with KNE can be found [here](https://github.com/openconfig/kne/tree/main/manifests/meshnet/base).

### Controllers

Field  | Type      | Description
------ | --------- | ----------------------------------------------------
`kind` | string    | Name of the controller type. The current options currently are `IxiaTG`, `SRLinux`, and `CEOSLab`.
`spec` | yaml.Node | Fields that set the options for the controller type.

#### IxiaTG

Field       | Type            | Description
----------- | --------------- | --------------------------------------------
`configMap` | IxiaTGConfigMap | Map of images used by the IxiaTG controller.
`manifests` | string          | Path of the directory holding the manifests to create an IxiaTG controller in the cluster. The directory is expected to contain a file with the name `ixiatg-operator.yaml`. Optionally the directory can contain a file with the name `ixia-configmap.yaml` to apply a config map of the desired container images used by the controller.                                  :

- IxiaTGConfigMap

Field     | Type          | Description
--------- | ------------- | ---------------------------------------------
`release` | string        | Version of the release these images should be associated with.
`images`  | []IxiaTGImage | List of IxiaTG images.

- IxiaTGImage

Field  | Type   | Description
------ | ------ | -------------------------------------------------------
`name` | string | Name of the image type for the controller to interpret.
`path` | string | Path of the container image.
`tag`  | string | Tag of the container image.

#### SRLinux

Field       | Type   | Description
----------- | ------ | -----------------------------------------------------
`manifests` | string | Path of the directory holding the manifests to create an SRLinux controller in the cluster. The directory is expected to contain a file with the name `kustomization.yaml`.

#### CEOSLab

Field       | Type   | Description
----------- | ------ | -----------------------------------------------------
`manifests` | string | Path of the directory holding the manifests to create a CEOSLab controller in the cluster. The directory is expected to contain a file with the name `manifest.yaml`.

</details>

---

The basic deployment yaml file can be found in the GitHub repo at
[deploy/kne/kind-bridge.yaml](https://github.com/openconfig/kne/blob/5e6cf1cbc0748bb48ebf49039bd0ad592378357a/deploy/kne/kind-bridge.yaml).

This config specifies `kind` as the cluster, `metallb` as the ingress, and
`meshnet` as the CNI. Additionally, the config instructs `kindnet` CNI to use
`bridge` CNI, instead of a default `ptp`. This spec can be deployed using the
following command:

```bash
kne deploy deploy/kne/kind-bridge.yaml
```

## Deploying additional vendor controllers

Some vendors provide a controller that handles the pod lifecycle for their
nodes. If you did not specify these in your deployment configuration, you will
need to manually create them **before** deploying a topology. Currently the
following vendors use a controller:

- Keysight: `ixia-tg`
- Nokia: `srlinux`
- Arista: `cEOS`

These controllers can be deployed as part of [cluster
deployment](#deploy-a-cluster).

### IxiaTG Controller

> IMPORTANT: Contact Keysight to get access to the IxiaTG controller yaml and
> container images.

To manually apply the controller run the following command after cloning the
Keysight controller yaml:

```bash
kubectl apply -f ixiatg-operator.yaml
```

### SR Linux Controller

> IMPORTANT: Make sure to use the `kind-bridge.yaml` deployment config. This is
> because the SR Linux controller + containers require the `bridge` cluster CNI
> instead of the default `ptp` cluster CNI that `kind` uses.

```bash
kubectl apply -k https://github.com/srl-labs/srl-controller/config/default
```

See more on the
[srl-controller GitHub repo](https://github.com/srl-labs/srl-controller).

#### cEOS Controller

> IMPORTANT: Contact Arista to get access to the cEOS container image.

To manually apply the controller, first clone the `arista-ceoslab-operator`
[repo](setup.md#arista-ceoslab-operator) and then run the following command:

```bash
kubectl apply -f https://github.com/arista-ceoslab-operator/config/kustomized/manifest.yaml
```

See more on the
[arista-ceoslab-operator GitHub repo](https://github.com/aristanetworks/arista-ceoslab-operator).

## Create a topology

After cluster deployment, a topology can be created inside of it. This can be
done using the `kne create` command.

```bash
$ kne help create
Create Topology

Usage:
  kne create <topology file> [flags]

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
kne create examples/3node-withtraffic.pb.txt
```

> IMPORTANT: Wait for the command to fully complete, do not use Ctrl-C to cancel
> the command. It is expected to take minutes depending on the topology and if
> initial config is pushed.

### Container images

Container images can be hosted in multiple locations. For example
[DockerHub](https://hub.docker.com/) hosts open sourced containers. [Google
Artifact Registries](https://cloud.google.com/artifact-registry) can be used to
host images with access control. The [KNE topology
proto](https://github.com/openconfig/kne/blob/df91c62eb7e2a1abbf0a803f5151dc365b6f61da/proto/topo.proto#L117),
the manifests, and controllers can all specify containers that get pulled from
their source locations and get used in the cluster.

To load an image into a `kind` cluster there is a 3 step process:

1. Pull the desired image:

    ```bash
    docker pull src_image:src_tag
    ```

2. Tag the image with the desired in-cluster name:

    ```bash
    docker tag src_image:src_tag dst_image:dst_tag
    ```

3. Load the image into the `kind` cluster:

    ```bash
    kind load docker-image dst_image:dst_tag --name=kne
    ```

Now the `dst_image:dst_tag` image will be present for use in the `kind` cluster.

> NOTE: `ceos:latest` is the default image to use for a node of type
> `ARISTA_CEOS`. This is a common image to manually pull if using a topology
> with `ARISTA_CEOS` node and not specifying an image explicitly in your
> topology textproto. Contact Arista to get access to the cEOS image.

You can check a full list of images loaded in your `kind` cluster using:

```bash
docker exec -it kne-control-plane crictl images
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

To delete a topology use `kne delete`:

```bash
kne delete examples/3node-withtraffic.pb.txt
```

To delete a cluster use `kind delete cluster`:

```bash
kind delete cluster --name=kne
```
