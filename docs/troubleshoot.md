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

Use the `--progress` option to monitor the state of the pods as KNE is coming up.

### Pods stuck in ImagePullBackOff

```bash
$ kubectl get pods -A
NAMESPACE     NAME   READY   STATUS             RESTARTS   AGE
multivendor   r1     0/1     ImagePullBackOff   0          44m
```

This is due to an issue with your cluster fetching container images. Follow the
container image access [steps](create_topology.md#container_images) and then
delete/recreate the topology. To check which image is causing the issue, use
`kubectl describe` command on the problematic pod.

KNE should normally detect this condition and fail the deployment.

### Pods stuck in Init

After creating a topology, some pods may get stuck in an `Init:0/1` state while
others may be stuck `Pending`.

```bash
$ kubectl get pods -A
NAMESPACE     NAME   READY   STATUS     RESTARTS   AGE
multivendor   r1     0/1     Init:0/1   0          12m
multivendor   r2     0/1     Pending    0          12m
...
```

You may also see logs for the `init-container` on one of the `Init:0/1` pods
that look like this:

```bash
$ kubectl logs r1 init-r1 -n multivendor
Waiting for all 2 interfaces to be connected
Connected 1 interfaces out of 2
Connected 1 interfaces out of 2
Connected 1 interfaces out of 2
...
```

The Pending pods may have an error like the following:

```bash
$ kubectl describe pod r2 -n multivendor
...
Events:
Type Reason Age From Message
Warning FailedScheduling 9s (x19 over 18m) default-scheduler 0/1 nodes are available: 1 Insufficient cpu.
```

Fix To fix this issue you will need a VM instance with more vCPUs and memory.
A machine with 16 vCPUs should be
sufficient. Optionally you can deploy a smaller topology instead.

### Cannot SSH into instance

```bash
$ ssh 192.168.18.100
ssh: connect to host 192.168.18.100 port 22: Connection refused
```

Validate the pod is running:

```bash
$ kubectl get pod r1 -n multivendor
NAME   READY   STATUS    RESTARTS   AGE
r1     1/1     Running   0          4m47s
```

Validate service is exposed:

```bash
$ kubectl get services r1 -n multivendor
NAME         TYPE           CLUSTER-IP     EXTERNAL-IP      PORT(S)                                     AGE
service-r1   LoadBalancer   10.96.134.70   192.168.18.100   443:30001/TCP,22:30004/TCP,6030:30005/TCP   4m22s
```

Validate you can `exec` on the container:

```bash
$ kubectl exec -it r1 -n multivendor -- Cli
Defaulted container "r1" out of: r1, init-r1 (init)
error: Internal error occurred: error executing command in
container: failed to exec in container: failed to start exec
"68abfa4f3742c86f49ec00dff629728d96e589c6848d5247e29d396365d6b697": OCI
runtime exec failed: exec failed: container_linux.go:370: starting container
process caused: exec: "Cli": executable file not found in $PATH: unknown
```

Fix the systemd path and cgroups directory and then delete/recreate topology.

### Vendor container is `Running` but `System is not yet ready...`

If you see something similar to the following on a `cptx` Juniper node:

```bash
$ kubectl exec -it r4 -n multivendor -- cli
Defaulted container "r4" out of: r4, init-r4 (init)
System is not yet ready...
```

then your host likely does not support nested virtualization. Run the following
to confirm, if the output is `0` then the host does not support nested
virtualization.

```bash
$ grep -cw vmx /proc/cpuinfo
0
```

Enable nested virtualization or move to a new machine that supports it.
When done, run the following ensuring a non-zero output:

```bash
$ grep -cw vmx /proc/cpuinfo
16
```

Then delete your cluster and start again.
