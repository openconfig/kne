# Create a KNE topology

This is part of the How-To guide collection. This guide covers KNE cluster
deployment and topology creation.

## Overview

Creating a topology takes 2 main steps:

1. Cluster deployment
1. Topology creation

## Deploy a cluster

The first step when using KNE is to deploy a Kubernetes cluster. This can be
done using the `kne deploy` command.

```bash
$ kne help deploy
Deploy cluster.

Usage:
  kne deploy <deployment yaml> [flags]

Flags:
  -h, --help       help for deploy
      --progress   Display progress of container bringup

Global Flags:
      --kubecfg string     kubeconfig file (default "/path/to/home/{{USERNAME}}/.kube/config")
  -v, --verbosity string   log level (default "info")
```

A deployment yaml file specifies 4 things (*optional in italics*):

1. A cluster spec
2. An ingress spec
3. A CNI spec
4. *A list of controller specs*

Expand the below section for a full description of all fields in the deployment
yaml.

---

<details>
<summary><h3>Deployment yaml reference</h3></summary>

> NOTE: ~~Strikethrough~~ fields are DEPRECATED and should not be used.

Field         | Type             | Description
------------- | ---------------- | ---------------------------------------------
`cluster`     | ClusterSpec      | Spec for the cluster.
`ingress`     | IngressSpec      | Spec for the ingress.
`cni`         | CNISpec          | Spec for the CNI.
`controllers` | []ControllerSpec | List of specs for the additional controllers.

#### Cluster

Field  | Type      | Description
------ | --------- | ---------------------------------------------------
`kind` | string    | Name of the cluster type. The options currently are `Kind` or `External`.
`spec` | yaml.Node | Fields that set the options for the cluster type.

##### Kind

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

##### External

Field     | Type   | Description
--------- | ------ | -------------------------------------------------------
`network` | string | Name of the docker network to create a pool of external IP addresses for ingress to assign to services.

#### Ingress

Field  | Type      | Description
------ | --------- | ------------------------------------------------------
`kind` | string    | Name of the ingress type. The only option currently is `MetalLB`.
`spec` | yaml.Node | Fields that set the options for the ingress type.

##### MetalLB

Field           | Type       | Description
--------------- | ---------- | -----------
`ip_count`      | int        | Number of IP addresses to include in the available pool.
`manifest`      | string     | Path of the manifest yaml file to create MetalLB in the cluster. The validated manifest for use with KNE can be found [here](https://github.com/openconfig/kne/tree/main/manifests/metallb/manifest.yaml).
~~`manifests`~~ | ~~string~~ | ~~Path of the directory holding the manifests to create MetalLB in the cluster. The directory is expected to contain a file with the name `metallb-native.yaml`.~~

#### CNI

Field  | Type      | Description
------ | --------- | --------------------------------------------------
`kind` | string    | Name of the CNI type. The only option currently is `Meshnet`.
`spec` | yaml.Node | Fields that set the options for the CNI type.

##### Meshnet

Field           | Type       | Description
--------------- | ---------- | -----------
`manifest`      | string     | Path of the manifest yaml file to create Meshnet in the cluster. The validated manifest for use with KNE can be found [here](https://github.com/openconfig/kne/tree/main/manifests/meshnet/grpc/manifest.yaml).
~~`manifests`~~ | ~~string~~ | ~~Path of the directory holding the manifests to create Meshnet in the cluster. The directory is expected to contain a file with the name `manifest.yaml`.~~

#### Controllers

Field  | Type      | Description
------ | --------- | ----------------------------------------------------
`kind` | string    | Name of the controller type. The current options currently are `IxiaTG`, `SRLinux`, `CEOSLab`, and `Lemming`.
`spec` | yaml.Node | Fields that set the options for the controller type.

##### IxiaTG

Field           | Type       | Description
--------------- | ---------- | -----------
`operator`      | string     | Path of the yaml file to create an IxiaTG operator in the cluster. The validated operator for use with KNE can be found [here](https://github.com/openconfig/kne/tree/main/manifests/controllers/keysight/ixiatg-operator.yaml).
`configMap`     | string     | Path of the yaml file to create an IxiaTG config map in the cluster. The validated config map for use with KNE can be found [here](https://github.com/openconfig/kne/tree/main/manifests/controllers/keysight/ixiatg-configmap.yaml).
~~`manifests`~~ | ~~string~~ | ~~Path of the directory holding the manifests to create an IxiaTG operator in the cluster. The directory is expected to contain a file with the name `ixiatg-operator.yaml`. Optionally the directory can contain a file with the name `ixiatg-configmap.yaml` to apply a config map of the desired container images used by the controller.~~

##### SRLinux

Field           | Type       | Description
--------------- | ---------- | -----------
`operator`      | string     | Path of the yaml file to create an SRLinux operator in the cluster. The validated operator for use with KNE can be found [here](https://github.com/openconfig/kne/tree/main/manifests/controllers/srlinux/manifest.yaml).
~~`manifests`~~ | ~~string~~ | ~~Path of the directory holding the manifests to create an SRLinux operator in the cluster. The directory is expected to contain a file with the name `manifest.yaml`.~~

##### CEOSLab

Field           | Type       | Description
--------------- | ---------- | -----------
`operator`      | string     | Path of the yaml file to create a CEOSLab operator in the cluster. The validated operator for use with KNE can be found [here](https://github.com/openconfig/kne/tree/main/manifests/controllers/ceoslab/manifest.yaml).
~~`manifests`~~ | ~~string~~ | ~~Path of the directory holding the manifests to create a CEOSLab operator in the cluster. The directory is expected to contain a file with the name `manifest.yaml`.~~

##### Lemming

Field           | Type       | Description
--------------- | ---------- | -----------
`operator`      | string     | Path of the yaml file to create a Lemming operator in the cluster. The validated operator for use with KNE can be found [here](https://github.com/openconfig/kne/tree/main/manifests/controllers/lemming/manifest.yaml).
~~`manifests`~~ | ~~string~~ | ~~Path of the directory holding the manifests to create a Lemming operator in the cluster. The directory is expected to contain a file with the name `manifest.yaml`.~~

</details>

---

The basic deployment yaml file can be found in the GitHub repo at
[deploy/kne/kind-bridge.yaml](https://github.com/openconfig/kne/tree/main/deploy/kne/kind-bridge.yaml).

This config specifies `kind` as the cluster, `metallb` as the ingress, and
`meshnet` as the CNI. Additionally, the config instructs `kindnet` CNI to use
`bridge` CNI, instead of a default `ptp`. This spec can be deployed using the
following command:

```bash
kne deploy deploy/kne/kind-bridge.yaml
```

## Deploying additional vendor controllers

TIP: Additional controller deployment is not needed to follow this How-To.

Some vendors provide a controller that handles the pod lifecycle for their
nodes. If you did not specify these in your deployment configuration, you will
need to manually create them **before** deploying a topology. Currently the
following vendors use a controller:

- Keysight: `ixiatg`
- Nokia: `srlinux`
- Arista: `ceoslab`
- OpenConfig: `lemming`

These controllers can be deployed as part of [cluster
deployment](#deploy-a-cluster).

### IxiaTG Controller

```bash
kubectl apply -f manifests/controllers/keysight/ixiatg-operator.yaml
kubectl apply -f manifests/controllers/keysight/ixiatg-configmap.yaml
```

The above steps will allow deployment of the Community edition controller. For licensed controller the following additional step is required.

```bash
kubectl create secret -n ixiatg-op-system generic license-server --from-literal=addresses="<license IP addresses>"
```

When upgrading ensure the secret is deleted before the operator is upgraded.

```bash
kubectl delete secret/license-server -n ixiatg-op-system
```

See more on the
[keng-operator GitHub repo](https://github.com/open-traffic-generator/keng-operator).

### SR Linux Controller

```bash
kubectl apply -f manifests/controllers/srlinux/manifest.yaml
```

See more on the
[srl-controller GitHub repo](https://github.com/srl-labs/srl-controller).

#### cEOS Controller

```bash
kubectl apply -f manifests/controllers/ceoslab/manifest.yaml
```

See more on the
[arista-ceoslab-operator GitHub repo](https://github.com/aristanetworks/arista-ceoslab-operator).

#### lemming

To manually apply the controller run the following command:

```bash
kubectl apply -f manifests/controllers/lemming/manifest.yaml
```

## Container images

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

> NOTE: `ceos:latest` is the default image to use for a node of vendor
> `ARISTA`. This is a common image to manually pull if using a topology
> with an `ARISTA` node and not specifying an image explicitly in your
> topology textproto. Contact Arista to get access to the cEOS image.

You can check a full list of images loaded in your `kind` cluster using:

```bash
docker exec -it kne-control-plane crictl images
```

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
      --kubecfg string     kubeconfig file (default "/path/to/home/{{USERNAME}}/.kube/config")
  -v, --verbosity string   log level (default "info")
```

A topology file is a textproto of the `Topology`
[message](https://github.com/openconfig/kne/blob/df91c62eb7e2a1abbf0a803f5151dc365b6f61da/proto/topo.proto#L26).
This file specifies all of the nodes and links of your desired topology. In the
node definitions interfaces, services, and initial configs can be specified.

An example topology containing 4 DUT nodes (Arista, Cisco, Nokia, and Juniper)
and 1 ATE node (Keysight) can be found under the examples directory at
[examples/multivendor/multivendor.pb.txt](https://github.com/openconfig/kne/blob/main/examples/multivendor/multivendor.pb.txt).
The initial vendor router configs referenced in the topology are found
[here](https://github.com/openconfig/kne/tree/main/examples/multivendor)
See the [push config](interact_topology.md#push_config) section for details
about pushing config after initial creation.

Make sure to load all 4 vendor images into the cluster following the above guide:

- `ceos:latest`
- `cptx:latest`
- `xrd:latest`
- `ghcr.io/nokia/srlinux:latest`

> WARNING: This example topology requires a host with at least 16 CPU cores.

This topology can be created using the following command.

```bash
kne create examples/multivendor/multivendor.pb.txt
```

> IMPORTANT: Wait for the command to fully complete, do not use Ctrl-C to cancel
> the command. It is expected to take minutes depending on the topology and if
> initial config is pushed.

## Verify topology health

Check that all pods are healthy and `Running`:

```bash
$ kubectl get pods -A
NAMESPACE                        NAME                                                         READY   STATUS    RESTARTS   AGE
arista-ceoslab-operator-system   arista-ceoslab-operator-controller-manager-8d8f945f9-stjf7   2/2     Running   0          4m26s
ixiatg-op-system                 ixiatg-op-controller-manager-5f978b8cbf-kn9z2                2/2     Running   0          4m20s
kube-system                      coredns-78fcd69978-7gv8b                                     1/1     Running   0          4m49s
kube-system                      coredns-78fcd69978-kdncc                                     1/1     Running   0          4m49s
kube-system                      etcd-kne-control-plane                                       1/1     Running   0          5m3s
kube-system                      kindnet-ct9df                                                1/1     Running   0          4m49s
kube-system                      kube-apiserver-kne-control-plane                             1/1     Running   0          5m3s
kube-system                      kube-controller-manager-kne-control-plane                    1/1     Running   0          5m3s
kube-system                      kube-proxy-2n9n9                                             1/1     Running   0          4m49s
kube-system                      kube-scheduler-kne-control-plane                             1/1     Running   0          5m3s
lemming-operator                 lemming-controller-manager-5b9856cd44-bs8mf                  2/2     Running   0          4m33s
local-path-storage               local-path-provisioner-85494db59d-7blp6                      1/1     Running   0          4m49s
meshnet                          meshnet-5tttq                                                1/1     Running   0          4m20s
metallb-system                   controller-6d5fb97874-crjbn                                  1/1     Running   0          4m49s
metallb-system                   speaker-vn2l8                                                1/1     Running   0          4m29s
multivendor                      otg-controller                                               3/3     Running   0          72s
multivendor                      otg-port-eth1                                                2/2     Running   0          72s
multivendor                      otg-port-eth2                                                2/2     Running   0          72s
multivendor                      otg-port-eth3                                                2/2     Running   0          72s
multivendor                      otg-port-eth4                                                2/2     Running   0          72s
multivendor                      r1                                                           2/2     Running   0          72s
multivendor                      r2                                                           2/2     Running   0          72s
multivendor                      r3                                                           2/2     Running   0          72s
multivendor                      r4                                                           2/2     Running   0          72s
srlinux-controller               srlinux-controller-controller-manager-854f499fc4-b45jb       2/2     Running   0          3m59s
```

Check that all services appear as expected and have an assigned `EXTERNAL-IP`:

```bash
$ kubectl get services -n multivendor
NAME                           TYPE           CLUSTER-IP      EXTERNAL-IP     PORT(S)                                      AGE
service-gnmi-otg-controller    LoadBalancer   10.96.179.48    192.168.11.55   50051:30901/TCP                              4m9s
service-grpc-otg-controller    LoadBalancer   10.96.33.245    192.168.11.56   40051:30449/TCP                              4m9s
service-https-otg-controller   LoadBalancer   10.96.215.225   192.168.11.54   443:32556/TCP                                4m9s
service-otg-port-eth1          LoadBalancer   10.96.82.37     192.168.11.58   5555:30886/TCP,50071:30286/TCP               4m9s
service-otg-port-eth2          LoadBalancer   10.96.204.154   192.168.11.59   5555:31326/TCP,50071:31860/TCP               4m9s
service-otg-port-eth3          LoadBalancer   10.96.136.253   192.168.11.60   5555:30181/TCP,50071:31619/TCP               4m9s
service-otg-port-eth4          LoadBalancer   10.96.205.227   192.168.11.57   5555:32636/TCP,50071:31247/TCP               4m9s
service-r1                     LoadBalancer   10.96.130.198   192.168.11.50   443:32101/TCP,22:32304/TCP,6030:32011/TCP    4m12s
service-r2                     LoadBalancer   10.96.107.2     192.168.11.51   443:31942/TCP,22:30785/TCP,57400:30921/TCP   4m11s
service-r3                     LoadBalancer   10.96.80.18     192.168.11.52   22:32410/TCP                                 4m11s
service-r4                     LoadBalancer   10.96.138.204   192.168.11.53   22:31932/TCP,50051:32666/TCP                 4m10s
```

If anything is unexpected check the [Troubleshooting](troubleshoot.md) guide.

## Clean up KNE

To delete a topology use `kne delete`:

```bash
kne delete examples/multivendor/multivendor.pb.txt
```

To delete a cluster use `kne teardown`:

```bash
kne teardown deploy/kne/kind-bridge.yaml
```
