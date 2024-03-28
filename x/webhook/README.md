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

```
./containerize.sh
```

## Deployment

For the webhook to be effective it must be deployed when the k8s cluster is up
but before the KNE topology is created.

Start by deploying the kubernetes cluster

```
$ kne deploy kne/deploy/kne/kind-bridge.yaml
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
$ kubectl apply -f kne/x/webhook/manifests/
```

This should result in the webhook pod to be present.

```
default                          kne-assembly-webhook-f5b8cf987-lpxjt                         1/1     Running   0          5s
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
$ kne create kne/x/webhook/examples/topology.textproto
```

You should now see r1 with 2 containers instead of the one, this is
because the webhook has injected the alpine linux container.

```
webhook-example                         r1                                                            3/3     Running   0          24s
webhook-example                         r2                                                            3/3     Running   0          22s
```

## Removing the webhook

Removing the webhook can be achieved by deleting the loaded manifests from the
k8s cluster.

```
$ kubectl delete -f kne/x/webhook/manifests/
```

## Debugging

In order to obtain the logs of the webhook you can use the following command.

```
$ kubectl logs -l app=kne-assembly-webhook -f
```

In the logs you should see output similar to this:

```
I1215 11:54:46.729680       1 main.go:25] Listening on port 443...
I1215 11:59:16.870292       1 mutate.go:30] Mutating pod r0
I1215 11:59:18.952032       1 mutate.go:30] Mutating pod r1
I1215 11:59:18.960733       1 admission.go:47] multi container pod not requested for this container
```

This output shows that it mutated the pod r1 but not r2 since
the label was not added to that KNE node.
