# KNE Troubleshooting

This is part of the How-To guide collection. This guide covers KNE
troubleshooting techniques and common issues.

## General tips

### kubectl

The first step in troubleshooting general issues is familiarizing yourself with
common `kubectl`
[commands](https://kubernetes.io/docs/reference/kubectl/cheatsheet/).

- `get pods/services`: Useful for determining basic state info

- `describe pods`: Useful for more verbose state info

- `logs`: Useful to get a dump of all pod logs

The `-n <namespace>` flag is necessary to specify the namespace to inspect. In
KNE there are several namespaces:

- one namespace per topology
- one namespace for `meshnet` CNI
- one namespace for `metallb` ingress
- one namespace per vendor controller
- `default` kube namespace

For an exhaustive list use the `-A` flag instead of `-n`.

## Common issues

### Cannot SSH into instance

```bash
$ ssh 192.168.18.100
ssh: connect to host 192.168.18.100 port 22: Connection refused
```

Validate the pod is running:

```bash
$ kubectl get pod r1 -n 3node-ceos
NAME READY STATUS RESTARTS AGE
r1 1/1 Running 0 4m47s
```

Validate service is exposed:

```bash
$ kubectl get services r1 -n 3node-ceos
NAME TYPE CLUSTER-IP EXTERNAL-IP PORT(S) AGE
service-r1 LoadBalancer 10.96.134.70 192.168.18.100 443:30001/TCP,22:30004/TCP,6030:30005/TCP 4m22s
```

Validate you can attach to container:

```bash
$ kubectl exec -it r1 -n 3node-ceos -- Cli
Defaulted container "r1" out of: r1, init-r1 (init)
error: Internal error occurred: error executing command in
container: failed to exec in container: failed to start exec
"68abfa4f3742c86f49ec00dff629728d96e589c6848d5247e29d396365d6b697": OCI
runtime exec failed: exec failed: container_linux.go:370: starting container
process caused: exec: "Cli": executable file not found in $PATH: unknown
```

### After reboot of machine KNE cluster stuck in init state

```bash
$ kubectl get pods -n 3node-ceos
NAMESPACE NAME READY STATUS RESTARTS AGE
3node-ceos r1 0/1 Init:0/1 1 7d22h
3node-ceos r2 0/1 Init:0/1 1 7d22h
3node-ceos r3 0/1 Init:0/1 1 7d22h
```

This problem is due to a bug in `meshnet` until this is fixed upstream you will
just need to delete and recreate the topology.

### MetalLB crash loops

```bash
$ kubectl get pods -n metallb-system
NAMESPACE NAME READY STATUS RESTARTS AGE
metallb-system controller-675995489c-2rv4w 0/1 CrashLoopBackOff 4 3m7s
metallb-system speaker-nnm9f 1/1 Running 0 3m7s
```

This issue is often fixed by deleting and redeploying your cluster.

### Pods stuck in ImagePullBackOff

```bash
$ kubectl get pods -A
NAMESPACE NAME READY STATUS RESTARTS AGE
ceos-simple r1 0/1 ImagePullBackOff 0 44m
ceos-simple r2 0/1 ImagePullBackOff 0 44m
kube-system coredns-78fcd69978-kqj86 1/1 Running 0 45m
kube-system coredns-78fcd69978-t68cd 1/1 Running 0 45m
kube-system etcd-kne-control-plane 1/1 Running 0 45m
kube-system kindnet-nx5z6 1/1 Running 0 45m
kube-system kube-apiserver-kne-control-plane 1/1 Running 0 45m
kube-system kube-controller-manager-kne-control-plane 1/1 Running 0 45m
kube-system kube-proxy-zdjlh 1/1 Running 0 45m
kube-system kube-scheduler-kne-control-plane 1/1 Running 0 45m
local-path-storage local-path-provisioner-85494db59d-r8cs7 1/1 Running 0 45m
meshnet meshnet-9z756 1/1 Running 0 44m
metallb-system controller-6cc57c4567-xjmmc 1/1 Running 0 45m
metallb-system speaker-c4mdm 1/1 Running 0 44m
```

This is due to an issue with your cluster fetching container images. Follow the
container image access [steps](#container_images) and then delete/recreate the
topology. To check which image is causing the issue, use `kubectl describe`
command on the problematic pod.

### Pods stuck in Init

After creating a topology, some pods may get stuck in an `Init:0/1` state while
others may be stuck `Pending`.

```bash
$ kubectl get pods -A
NAMESPACE NAME READY STATUS RESTARTS AGE
3node-traffic ate1 2/2 Running 1 12m
3node-traffic ate2 0/2 Init:0/1 0 12m
3node-traffic r1 0/1 Init:0/1 0 12m
3node-traffic r2 0/1 Pending 0 12m
3node-traffic r3 0/1 Pending 0 12m
...
```

You may also see logs for the `init-container` on one of the `Init:0/1` pods
that look like this:

```bash
$ kubectl logs pod/ate2 init-container -n 3node-traffic
Waiting for all 2 interfaces to be connected
Connected 1 interfaces out of 2
Connected 1 interfaces out of 2
Connected 1 interfaces out of 2
...
```

The Pending pods may have an error like the following:

```bash
$ kubectl describe pod r2 -n 3node-traffic
...
Events:
Type Reason Age From Message
Warning FailedScheduling 9s (x19 over 18m) default-scheduler 0/1 nodes are available: 1 Insufficient cpu.
```

Fix To fix this issue you will need a host with more (v)CPUs and memory. It is
recommended to run on a machine with at least 8 (v)CPUs and 32 GB memory. You
may also need to deploy a smaller topology instead.
