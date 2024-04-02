# KNE Mutating Webhook

This directory contains the code and configurations (in the form of manifests)
for the mutating webhook. The webhook should be deployed onto a KNE
cluster.

This webhook can be used to mutate any K8 resources. This directory contains
the generic webhook along with an example mutator that simply adds an alpine
linux container to created pods.

To develop custom a custom mutation simply change the mutate function in the
examples subdirectory.

## Build

Run:

```
$ ./containerize.sh
```

to build the webhook container from the binary `main.go`.

## Deployment

For the webhook to be effective it must be deployed when the k8s cluster is up
but before the KNE topology is created.

Start by deploying the kubernetes cluster

```
$ kne deploy ../../deploy/kne/kind-bridge.yaml
```

You should be in this state:

```
$ kubectl get pods -A
NAMESPACE                        NAME                                                          READY   STATUS    RESTARTS   AGE
arista-ceoslab-operator-system   arista-ceoslab-operator-controller-manager-5cb5fb9db4-7jqp9   2/2     Running   0          45h
ixiatg-op-system                 ixiatg-op-controller-manager-5947cd6f59-jq5pw                 2/2     Running   0          45h
kube-system                      coredns-787d4945fb-8x8wf                                      1/1     Running   0          45h
kube-system                      coredns-787d4945fb-ng7hf                                      1/1     Running   0          45h
kube-system                      etcd-kne-control-plane                                        1/1     Running   0          45h
kube-system                      kindnet-zlwzz                                                 1/1     Running   0          45h
kube-system                      kube-apiserver-kne-control-plane                              1/1     Running   0          45h
kube-system                      kube-controller-manager-kne-control-plane                     1/1     Running   0          45h
kube-system                      kube-proxy-kwsqm                                              1/1     Running   0          45h
kube-system                      kube-scheduler-kne-control-plane                              1/1     Running   0          45h
lemming-operator                 lemming-controller-manager-6fc9d47f7d-vnshj                   2/2     Running   0          45h
local-path-storage               local-path-provisioner-c8855d4bb-8m9bp                        1/1     Running   0          45h
meshnet                          meshnet-ddm8q                                                 1/1     Running   0          45h
metallb-system                   controller-8bb68977b-vx99n                                    1/1     Running   0          45h
metallb-system                   speaker-hj8jf                                                 1/1     Running   0          45h
srlinux-controller               srlinux-controller-controller-manager-57f8c48bf-6kqlg         2/2     Running   0          45h
```

At this point the k8s cluster is up and operational. We can now load the webhook
manifests.

```
$ kind load docker-image webhook:latest --name kne
```

```
$ kubectl apply -f manifests/
```

This should result in the webhook pod to be present.

```
$ kubectl get pods -A
...
default                          kne-assembly-webhook-f5b8cf987-lpxjt                         1/1     Running   0          5s
...
```

We can now create the KNE topology.

*Note* The KNE topology must have the label `webhook:enabled` for each node, as in
[this example](examples/topology.textproto),
otherwise the webhook will ignore the pod upon create.

```
labels {
    key: "webhook"
    value: "enabled"
}
```

Use the normal KNE command to create the topology.

```
$ kne create examples/topology.textproto
```

You should now see r1 with 2 containers instead of the one, this is
because the webhook has injected the alpine linux container.

```
$ kubectl get pods -n webhook-example
r1                                                            3/3     Running   0          24s
r2                                                            2/2     Running   0          22s
```

```
$ kubectl describe pod r1 -n webhook-example
...
Containers:
  r1:
    Container ID:  containerd://0dd84381ac5970d796c866adf73022c1ed5610ceb40546e443a35b7eff6a3f39
    Image:         alpine:latest
    Image ID:      docker.io/library/alpine@sha256:c5b1261d6d3e43071626931fc004f70149baeba2c8ec672bd4f27761f8e1ad6b
    Port:          <none>
    Host Port:     <none>
    Command:
      /bin/sh
      -c
$ kubectl get pods -n webhook-example
      sleep 2000000000000
    State:          Running
      Started:      Tue, 02 Apr 2024 23:24:40 +0000
    Ready:          True
    Restart Count:  0
    Environment:    <none>
    Mounts:
      /var/run/secrets/kubernetes.io/serviceaccount from kube-api-access-kgrn6 (ro)
  alpine:
    Container ID:  containerd://be7db4c4704b415d0dda1d5ed64b70c7f271086670aa803f105399dc95e35ad8
    Image:         alpine:latest
    Image ID:      docker.io/library/alpine@sha256:c5b1261d6d3e43071626931fc004f70149baeba2c8ec672bd4f27761f8e1ad6b
    Port:          <none>
    Host Port:     <none>
    Command:
      /bin/sh
      -c
      sleep 2000000000000
    State:          Running
      Started:      Tue, 02 Apr 2024 23:24:40 +0000
    Ready:          True
    Restart Count:  0
    Requests:
      cpu:        500m
      memory:     1Gi
    Environment:  <none>
    Mounts:
      /var/run/secrets/kubernetes.io/serviceaccount from kube-api-access-kgrn6 (ro)
...
```

## Removing the webhook

Removing the webhook can be achieved by deleting the loaded manifests from the
k8s cluster.

```
$ kubectl delete -f manifests/
```

## Debugging

In order to obtain the logs of the webhook you can use the following command.

$ kubectl get pods -n webhook-example
```
$ kubectl logs -l app=kne-assembly-webhook -f
```

In the logs you should see output similar to this:

```
I1215 11:54:46.729680       1 main.go:25] Listening on port 443...
I0402 23:24:36.383536       1 mutate.go:45] Mutating &TypeMeta{Kind:Pod,APIVersion:v1,}
I0402 23:24:36.394188       1 mutate.go:45] Mutating &TypeMeta{Kind:Pod,APIVersion:v1,}
I0402 23:24:36.394227       1 addcontainer.go:34] Ignoring pod "r2", mutation not requested
```

This output shows that it mutated the pod r1 but not r2 since
the label was not added to that KNE node.

### TLS

Run:

```
$ ./secure/genCerts.sh
```

to optionally update the TLS certs in the manifest files. It handles updating
`manifests/tls.secret.yaml` however the `caBundle` in
`manifests/mutating.config.yaml` will need to be updated manually based on the
output of the script. This is not required but may be useful.

## Customize the webhook

Edit `main.go` to specify any mutation functions as desired. The example uses
the mutation function found in `examples/addcontainer/addcontainer.go` but any
mutation function is supported. This includes mutating services and other
resources besides just pods. However you may also have to change
`manifests/mutating.config.yaml` to select other resources types than just
pods.

After customization is done, rebuild the container and reapply the manifests.
