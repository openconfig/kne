package pods

import (
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
)


// Variables are named
//
//   name#data    Raw json from kubectl
//   name#pod     Data converted into a corev1.Pod
//   name#status  PodStatus version
//   name#string  PodStatus as a string

func json2pod(j string) *corev1.Pod {
	var pod corev1.Pod
	if err := json.Unmarshal(([]byte)(j), &pod); err != nil {
		panic(err)
	}
	return &pod
}

var (
	ceos1pod  = json2pod(ceos1data)
	ceos2pod  = json2pod(ceos2data)
	ceos3pod  = json2pod(ceos3data)
	ceos4pod  = json2pod(ceos4data)
	ceos5pod  = json2pod(ceos5data)
	ceos6pod  = json2pod(ceos6data)
	ceos6Ipod = json2pod(ceos6Idata) // second init container is different
	ceos7pod  = json2pod(ceos7data)

	ixia1pod = json2pod(ixia1data)
	ixia2pod = json2pod(ixia2data)
	ixia3pod = json2pod(ixia3data)

	meshnet1pod = json2pod(meshnet1data)
)

// We only use 4 pods when checking PodStatus.String as these cover all the cases.
var (
	ceos1string = `{Name: "arista-ceoslab-operator-controller-manager-66cb57484f-86lcz", UID: "cdbf21b3-a2e3-4c26-95a5-956945c8c122", Namespace: "arista-ceoslab-operator-system", Phase: "Pending"}`
	ceos2string = `{Name: "arista-ceoslab-operator-controller-manager-66cb57484f-86lcz", UID: "cdbf21b3-a2e3-4c26-95a5-956945c8c122", Namespace: "arista-ceoslab-operator-system", Phase: "Pending", Containers: {{Name: "kube-rbac-proxy"Image: "gcr.io/kubebuilder/kube-rbac-proxy:v0.11.0"Reason: "ContainerCreating"}, {Name: "manager"Image: "ghcr.io/aristanetworks/arista-ceoslab-operator:v2.0.1"Reason: "ContainerCreating"}}}`
	ceos3string = `{Name: "arista-ceoslab-operator-controller-manager-66cb57484f-86lcz", UID: "cdbf21b3-a2e3-4c26-95a5-956945c8c122", Namespace: "arista-ceoslab-operator-system", Phase: "Running", Containers: {{Name: "kube-rbac-proxy"Image: "gcr.io/kubebuilder/kube-rbac-proxy:v0.11.0"Ready: "true"}, {Name: "manager"Image: "ghcr.io/aristanetworks/arista-ceoslab-operator:v2.0.1"}}}`
	ceos6string = `{Name: "ceos", UID: "ec41a7f2-4f33-4eaf-8d34-df552c81445d", Namespace: "multivendor", Phase: "Pending", Containers: {{Name: "ceos"Image: "ceos:latest"Reason: "PodInitializing"}}, InitContainers: {{Name: "init-ceos"Image: "networkop/init-wait:latest"Reason: "PodInitializing"}, {Name: "init2-ceos"Image: "networkop/init-wait:latest"Reason: "PodInitializing"}}}`
)

var (
	ceos1data = `{
    "apiVersion": "v1",
    "kind": "Pod",
    "metadata": {
        "annotations": {
            "kubectl.kubernetes.io/default-container": "manager"
        },
        "creationTimestamp": "2023-03-24T16:41:45Z",
        "generateName": "arista-ceoslab-operator-controller-manager-66cb57484f-",
        "labels": {
            "control-plane": "controller-manager",
            "pod-template-hash": "66cb57484f"
        },
        "name": "arista-ceoslab-operator-controller-manager-66cb57484f-86lcz",
        "namespace": "arista-ceoslab-operator-system",
        "ownerReferences": [
            {
                "apiVersion": "apps/v1",
                "blockOwnerDeletion": true,
                "controller": true,
                "kind": "ReplicaSet",
                "name": "arista-ceoslab-operator-controller-manager-66cb57484f",
                "uid": "cf1c1859-7ce5-4f0c-8d92-4944a2e25f9d"
            }
        ],
        "resourceVersion": "827",
        "uid": "cdbf21b3-a2e3-4c26-95a5-956945c8c122"
    },
    "spec": {
        "containers": [
            {
                "args": [
                    "--secure-listen-address=0.0.0.0:8443",
                    "--upstream=http://127.0.0.1:8080/",
                    "--logtostderr=true",
                    "--v=0"
                ],
                "image": "gcr.io/kubebuilder/kube-rbac-proxy:v0.11.0",
                "imagePullPolicy": "IfNotPresent",
                "name": "kube-rbac-proxy",
                "ports": [
                    {
                        "containerPort": 8443,
                        "name": "https",
                        "protocol": "TCP"
                    }
                ],
                "resources": {
                    "limits": {
                        "cpu": "500m",
                        "memory": "128Mi"
                    },
                    "requests": {
                        "cpu": "5m",
                        "memory": "64Mi"
                    }
                },
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-j7pw9",
                        "readOnly": true
                    }
                ]
            },
            {
                "args": [
                    "--health-probe-bind-address=:8081",
                    "--metrics-bind-address=127.0.0.1:8080",
                    "--leader-elect"
                ],
                "command": [
                    "/manager"
                ],
                "image": "ghcr.io/aristanetworks/arista-ceoslab-operator:v2.0.1",
                "imagePullPolicy": "IfNotPresent",
                "livenessProbe": {
                    "failureThreshold": 3,
                    "httpGet": {
                        "path": "/healthz",
                        "port": 8081,
                        "scheme": "HTTP"
                    },
                    "initialDelaySeconds": 15,
                    "periodSeconds": 20,
                    "successThreshold": 1,
                    "timeoutSeconds": 1
                },
                "name": "manager",
                "readinessProbe": {
                    "failureThreshold": 3,
                    "httpGet": {
                        "path": "/readyz",
                        "port": 8081,
                        "scheme": "HTTP"
                    },
                    "initialDelaySeconds": 5,
                    "periodSeconds": 10,
                    "successThreshold": 1,
                    "timeoutSeconds": 1
                },
                "resources": {
                    "limits": {
                        "cpu": "500m",
                        "memory": "128Mi"
                    },
                    "requests": {
                        "cpu": "10m",
                        "memory": "64Mi"
                    }
                },
                "securityContext": {
                    "allowPrivilegeEscalation": false
                },
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-j7pw9",
                        "readOnly": true
                    }
                ]
            }
        ],
        "dnsPolicy": "ClusterFirst",
        "enableServiceLinks": true,
        "preemptionPolicy": "PreemptLowerPriority",
        "priority": 0,
        "restartPolicy": "Always",
        "schedulerName": "default-scheduler",
        "securityContext": {
            "runAsNonRoot": true
        },
        "serviceAccount": "arista-ceoslab-operator-controller-manager",
        "serviceAccountName": "arista-ceoslab-operator-controller-manager",
        "terminationGracePeriodSeconds": 10,
        "tolerations": [
            {
                "effect": "NoExecute",
                "key": "node.kubernetes.io/not-ready",
                "operator": "Exists",
                "tolerationSeconds": 300
            },
            {
                "effect": "NoExecute",
                "key": "node.kubernetes.io/unreachable",
                "operator": "Exists",
                "tolerationSeconds": 300
            }
        ],
        "volumes": [
            {
                "name": "kube-api-access-j7pw9",
                "projected": {
                    "defaultMode": 420,
                    "sources": [
                        {
                            "serviceAccountToken": {
                                "expirationSeconds": 3607,
                                "path": "token"
                            }
                        },
                        {
                            "configMap": {
                                "items": [
                                    {
                                        "key": "ca.crt",
                                        "path": "ca.crt"
                                    }
                                ],
                                "name": "kube-root-ca.crt"
                            }
                        },
                        {
                            "downwardAPI": {
                                "items": [
                                    {
                                        "fieldRef": {
                                            "apiVersion": "v1",
                                            "fieldPath": "metadata.namespace"
                                        },
                                        "path": "namespace"
                                    }
                                ]
                            }
                        }
                    ]
                }
            }
        ]
    },
    "status": {
        "phase": "Pending",
        "qosClass": "Burstable"
    }
}
`
	ceos1status = PodStatus{
		Name:      "arista-ceoslab-operator-controller-manager-66cb57484f-86lcz",
		UID:       "cdbf21b3-a2e3-4c26-95a5-956945c8c122",
		Namespace: "arista-ceoslab-operator-system",
		Phase:     "Pending",
	}

	ceos2data = `{
    "apiVersion": "v1",
    "kind": "Pod",
    "metadata": {
        "annotations": {
            "kubectl.kubernetes.io/default-container": "manager"
        },
        "creationTimestamp": "2023-03-24T16:41:45Z",
        "generateName": "arista-ceoslab-operator-controller-manager-66cb57484f-",
        "labels": {
            "control-plane": "controller-manager",
            "pod-template-hash": "66cb57484f"
        },
        "name": "arista-ceoslab-operator-controller-manager-66cb57484f-86lcz",
        "namespace": "arista-ceoslab-operator-system",
        "ownerReferences": [
            {
                "apiVersion": "apps/v1",
                "blockOwnerDeletion": true,
                "controller": true,
                "kind": "ReplicaSet",
                "name": "arista-ceoslab-operator-controller-manager-66cb57484f",
                "uid": "cf1c1859-7ce5-4f0c-8d92-4944a2e25f9d"
            }
        ],
        "resourceVersion": "834",
        "uid": "cdbf21b3-a2e3-4c26-95a5-956945c8c122"
    },
    "spec": {
        "containers": [
            {
                "args": [
                    "--secure-listen-address=0.0.0.0:8443",
                    "--upstream=http://127.0.0.1:8080/",
                    "--logtostderr=true",
                    "--v=0"
                ],
                "image": "gcr.io/kubebuilder/kube-rbac-proxy:v0.11.0",
                "imagePullPolicy": "IfNotPresent",
                "name": "kube-rbac-proxy",
                "ports": [
                    {
                        "containerPort": 8443,
                        "name": "https",
                        "protocol": "TCP"
                    }
                ],
                "resources": {
                    "limits": {
                        "cpu": "500m",
                        "memory": "128Mi"
                    },
                    "requests": {
                        "cpu": "5m",
                        "memory": "64Mi"
                    }
                },
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-j7pw9",
                        "readOnly": true
                    }
                ]
            },
            {
                "args": [
                    "--health-probe-bind-address=:8081",
                    "--metrics-bind-address=127.0.0.1:8080",
                    "--leader-elect"
                ],
                "command": [
                    "/manager"
                ],
                "image": "ghcr.io/aristanetworks/arista-ceoslab-operator:v2.0.1",
                "imagePullPolicy": "IfNotPresent",
                "livenessProbe": {
                    "failureThreshold": 3,
                    "httpGet": {
                        "path": "/healthz",
                        "port": 8081,
                        "scheme": "HTTP"
                    },
                    "initialDelaySeconds": 15,
                    "periodSeconds": 20,
                    "successThreshold": 1,
                    "timeoutSeconds": 1
                },
                "name": "manager",
                "readinessProbe": {
                    "failureThreshold": 3,
                    "httpGet": {
                        "path": "/readyz",
                        "port": 8081,
                        "scheme": "HTTP"
                    },
                    "initialDelaySeconds": 5,
                    "periodSeconds": 10,
                    "successThreshold": 1,
                    "timeoutSeconds": 1
                },
                "resources": {
                    "limits": {
                        "cpu": "500m",
                        "memory": "128Mi"
                    },
                    "requests": {
                        "cpu": "10m",
                        "memory": "64Mi"
                    }
                },
                "securityContext": {
                    "allowPrivilegeEscalation": false
                },
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-j7pw9",
                        "readOnly": true
                    }
                ]
            }
        ],
        "dnsPolicy": "ClusterFirst",
        "enableServiceLinks": true,
        "nodeName": "kne-control-plane",
        "preemptionPolicy": "PreemptLowerPriority",
        "priority": 0,
        "restartPolicy": "Always",
        "schedulerName": "default-scheduler",
        "securityContext": {
            "runAsNonRoot": true
        },
        "serviceAccount": "arista-ceoslab-operator-controller-manager",
        "serviceAccountName": "arista-ceoslab-operator-controller-manager",
        "terminationGracePeriodSeconds": 10,
        "tolerations": [
            {
                "effect": "NoExecute",
                "key": "node.kubernetes.io/not-ready",
                "operator": "Exists",
                "tolerationSeconds": 300
            },
            {
                "effect": "NoExecute",
                "key": "node.kubernetes.io/unreachable",
                "operator": "Exists",
                "tolerationSeconds": 300
            }
        ],
        "volumes": [
            {
                "name": "kube-api-access-j7pw9",
                "projected": {
                    "defaultMode": 420,
                    "sources": [
                        {
                            "serviceAccountToken": {
                                "expirationSeconds": 3607,
                                "path": "token"
                            }
                        },
                        {
                            "configMap": {
                                "items": [
                                    {
                                        "key": "ca.crt",
                                        "path": "ca.crt"
                                    }
                                ],
                                "name": "kube-root-ca.crt"
                            }
                        },
                        {
                            "downwardAPI": {
                                "items": [
                                    {
                                        "fieldRef": {
                                            "apiVersion": "v1",
                                            "fieldPath": "metadata.namespace"
                                        },
                                        "path": "namespace"
                                    }
                                ]
                            }
                        }
                    ]
                }
            }
        ]
    },
    "status": {
        "conditions": [
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:41:45Z",
                "status": "True",
                "type": "Initialized"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:41:45Z",
                "message": "containers with unready status: [kube-rbac-proxy manager]",
                "reason": "ContainersNotReady",
                "status": "False",
                "type": "Ready"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:41:45Z",
                "message": "containers with unready status: [kube-rbac-proxy manager]",
                "reason": "ContainersNotReady",
                "status": "False",
                "type": "ContainersReady"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:41:45Z",
                "status": "True",
                "type": "PodScheduled"
            }
        ],
        "containerStatuses": [
            {
                "image": "gcr.io/kubebuilder/kube-rbac-proxy:v0.11.0",
                "imageID": "",
                "lastState": {},
                "name": "kube-rbac-proxy",
                "ready": false,
                "restartCount": 0,
                "started": false,
                "state": {
                    "waiting": {
                        "reason": "ContainerCreating"
                    }
                }
            },
            {
                "image": "ghcr.io/aristanetworks/arista-ceoslab-operator:v2.0.1",
                "imageID": "",
                "lastState": {},
                "name": "manager",
                "ready": false,
                "restartCount": 0,
                "started": false,
                "state": {
                    "waiting": {
                        "reason": "ContainerCreating"
                    }
                }
            }
        ],
        "hostIP": "192.168.8.2",
        "phase": "Pending",
        "qosClass": "Burstable",
        "startTime": "2023-03-24T16:41:45Z"
    }
}
`
	ceos2status = PodStatus{
		Name:      "arista-ceoslab-operator-controller-manager-66cb57484f-86lcz",
		UID:       "cdbf21b3-a2e3-4c26-95a5-956945c8c122",
		Namespace: "arista-ceoslab-operator-system",
		Phase:     "Pending",
		Containers: []ContainerStatus{
			{Name: "kube-rbac-proxy",
				Image:   "gcr.io/kubebuilder/kube-rbac-proxy:v0.11.0",
				Ready:   false,
				Reason:  "ContainerCreating",
				Message: ""},
			{
				Name:    "manager",
				Image:   "ghcr.io/aristanetworks/arista-ceoslab-operator:v2.0.1",
				Reason:  "ContainerCreating",
				Message: "",
			},
		},
	}
	ceos3data = `{
    "apiVersion": "v1",
    "kind": "Pod",
    "metadata": {
        "annotations": {
            "kubectl.kubernetes.io/default-container": "manager"
        },
        "creationTimestamp": "2023-03-24T16:41:45Z",
        "generateName": "arista-ceoslab-operator-controller-manager-66cb57484f-",
        "labels": {
            "control-plane": "controller-manager",
            "pod-template-hash": "66cb57484f"
        },
        "name": "arista-ceoslab-operator-controller-manager-66cb57484f-86lcz",
        "namespace": "arista-ceoslab-operator-system",
        "ownerReferences": [
            {
                "apiVersion": "apps/v1",
                "blockOwnerDeletion": true,
                "controller": true,
                "kind": "ReplicaSet",
                "name": "arista-ceoslab-operator-controller-manager-66cb57484f",
                "uid": "cf1c1859-7ce5-4f0c-8d92-4944a2e25f9d"
            }
        ],
        "resourceVersion": "866",
        "uid": "cdbf21b3-a2e3-4c26-95a5-956945c8c122"
    },
    "spec": {
        "containers": [
            {
                "args": [
                    "--secure-listen-address=0.0.0.0:8443",
                    "--upstream=http://127.0.0.1:8080/",
                    "--logtostderr=true",
                    "--v=0"
                ],
                "image": "gcr.io/kubebuilder/kube-rbac-proxy:v0.11.0",
                "imagePullPolicy": "IfNotPresent",
                "name": "kube-rbac-proxy",
                "ports": [
                    {
                        "containerPort": 8443,
                        "name": "https",
                        "protocol": "TCP"
                    }
                ],
                "resources": {
                    "limits": {
                        "cpu": "500m",
                        "memory": "128Mi"
                    },
                    "requests": {
                        "cpu": "5m",
                        "memory": "64Mi"
                    }
                },
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-j7pw9",
                        "readOnly": true
                    }
                ]
            },
            {
                "args": [
                    "--health-probe-bind-address=:8081",
                    "--metrics-bind-address=127.0.0.1:8080",
                    "--leader-elect"
                ],
                "command": [
                    "/manager"
                ],
                "image": "ghcr.io/aristanetworks/arista-ceoslab-operator:v2.0.1",
                "imagePullPolicy": "IfNotPresent",
                "livenessProbe": {
                    "failureThreshold": 3,
                    "httpGet": {
                        "path": "/healthz",
                        "port": 8081,
                        "scheme": "HTTP"
                    },
                    "initialDelaySeconds": 15,
                    "periodSeconds": 20,
                    "successThreshold": 1,
                    "timeoutSeconds": 1
                },
                "name": "manager",
                "readinessProbe": {
                    "failureThreshold": 3,
                    "httpGet": {
                        "path": "/readyz",
                        "port": 8081,
                        "scheme": "HTTP"
                    },
                    "initialDelaySeconds": 5,
                    "periodSeconds": 10,
                    "successThreshold": 1,
                    "timeoutSeconds": 1
                },
                "resources": {
                    "limits": {
                        "cpu": "500m",
                        "memory": "128Mi"
                    },
                    "requests": {
                        "cpu": "10m",
                        "memory": "64Mi"
                    }
                },
                "securityContext": {
                    "allowPrivilegeEscalation": false
                },
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-j7pw9",
                        "readOnly": true
                    }
                ]
            }
        ],
        "dnsPolicy": "ClusterFirst",
        "enableServiceLinks": true,
        "nodeName": "kne-control-plane",
        "preemptionPolicy": "PreemptLowerPriority",
        "priority": 0,
        "restartPolicy": "Always",
        "schedulerName": "default-scheduler",
        "securityContext": {
            "runAsNonRoot": true
        },
        "serviceAccount": "arista-ceoslab-operator-controller-manager",
        "serviceAccountName": "arista-ceoslab-operator-controller-manager",
        "terminationGracePeriodSeconds": 10,
        "tolerations": [
            {
                "effect": "NoExecute",
                "key": "node.kubernetes.io/not-ready",
                "operator": "Exists",
                "tolerationSeconds": 300
            },
            {
                "effect": "NoExecute",
                "key": "node.kubernetes.io/unreachable",
                "operator": "Exists",
                "tolerationSeconds": 300
            }
        ],
        "volumes": [
            {
                "name": "kube-api-access-j7pw9",
                "projected": {
                    "defaultMode": 420,
                    "sources": [
                        {
                            "serviceAccountToken": {
                                "expirationSeconds": 3607,
                                "path": "token"
                            }
                        },
                        {
                            "configMap": {
                                "items": [
                                    {
                                        "key": "ca.crt",
                                        "path": "ca.crt"
                                    }
                                ],
                                "name": "kube-root-ca.crt"
                            }
                        },
                        {
                            "downwardAPI": {
                                "items": [
                                    {
                                        "fieldRef": {
                                            "apiVersion": "v1",
                                            "fieldPath": "metadata.namespace"
                                        },
                                        "path": "namespace"
                                    }
                                ]
                            }
                        }
                    ]
                }
            }
        ]
    },
    "status": {
        "conditions": [
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:41:45Z",
                "status": "True",
                "type": "Initialized"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:41:45Z",
                "message": "containers with unready status: [manager]",
                "reason": "ContainersNotReady",
                "status": "False",
                "type": "Ready"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:41:45Z",
                "message": "containers with unready status: [manager]",
                "reason": "ContainersNotReady",
                "status": "False",
                "type": "ContainersReady"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:41:45Z",
                "status": "True",
                "type": "PodScheduled"
            }
        ],
        "containerStatuses": [
            {
                "containerID": "containerd://6b2c82de9295a77cee346280ec605a66e53316dd51d6db19132017c025683445",
                "image": "gcr.io/kubebuilder/kube-rbac-proxy:v0.11.0",
                "imageID": "gcr.io/kubebuilder/kube-rbac-proxy@sha256:0df4ae70e3bd0feffcec8f5cdb428f4abe666b667af991269ec5cb0bbda65869",
                "lastState": {},
                "name": "kube-rbac-proxy",
                "ready": true,
                "restartCount": 0,
                "started": true,
                "state": {
                    "running": {
                        "startedAt": "2023-03-24T16:41:48Z"
                    }
                }
            },
            {
                "containerID": "containerd://e1dced1c61e7ec5cff99208579b8ab25aae8d8080c07d4b0b2b41340541e7dba",
                "image": "ghcr.io/aristanetworks/arista-ceoslab-operator:v2.0.1",
                "imageID": "ghcr.io/aristanetworks/arista-ceoslab-operator@sha256:cd7c12b30096843b20304911705b178db4c13c915223b0e58a6d4c4f800c24d1",
                "lastState": {},
                "name": "manager",
                "ready": false,
                "restartCount": 0,
                "started": true,
                "state": {
                    "running": {
                        "startedAt": "2023-03-24T16:41:50Z"
                    }
                }
            }
        ],
        "hostIP": "192.168.8.2",
        "phase": "Running",
        "podIP": "10.244.0.8",
        "podIPs": [
            {
                "ip": "10.244.0.8"
            }
        ],
        "qosClass": "Burstable",
        "startTime": "2023-03-24T16:41:45Z"
    }
}
`
	ceos3status = PodStatus{Name: "arista-ceoslab-operator-controller-manager-66cb57484f-86lcz",
		UID:       "cdbf21b3-a2e3-4c26-95a5-956945c8c122",
		Namespace: "arista-ceoslab-operator-system",
		Phase:     "Running",
		Containers: []ContainerStatus{
			{
				Name:  "kube-rbac-proxy",
				Image: "gcr.io/kubebuilder/kube-rbac-proxy:v0.11.0",
				Ready: true,
			},
			{
				Name:  "manager",
				Image: "ghcr.io/aristanetworks/arista-ceoslab-operator:v2.0.1",
			},
		},
	}

	ceos4data = `{
    "apiVersion": "v1",
    "kind": "Pod",
    "metadata": {
        "annotations": {
            "kubectl.kubernetes.io/default-container": "manager"
        },
        "creationTimestamp": "2023-03-24T16:41:45Z",
        "generateName": "arista-ceoslab-operator-controller-manager-66cb57484f-",
        "labels": {
            "control-plane": "controller-manager",
            "pod-template-hash": "66cb57484f"
        },
        "name": "arista-ceoslab-operator-controller-manager-66cb57484f-86lcz",
        "namespace": "arista-ceoslab-operator-system",
        "ownerReferences": [
            {
                "apiVersion": "apps/v1",
                "blockOwnerDeletion": true,
                "controller": true,
                "kind": "ReplicaSet",
                "name": "arista-ceoslab-operator-controller-manager-66cb57484f",
                "uid": "cf1c1859-7ce5-4f0c-8d92-4944a2e25f9d"
            }
        ],
        "resourceVersion": "881",
        "uid": "cdbf21b3-a2e3-4c26-95a5-956945c8c122"
    },
    "spec": {
        "containers": [
            {
                "args": [
                    "--secure-listen-address=0.0.0.0:8443",
                    "--upstream=http://127.0.0.1:8080/",
                    "--logtostderr=true",
                    "--v=0"
                ],
                "image": "gcr.io/kubebuilder/kube-rbac-proxy:v0.11.0",
                "imagePullPolicy": "IfNotPresent",
                "name": "kube-rbac-proxy",
                "ports": [
                    {
                        "containerPort": 8443,
                        "name": "https",
                        "protocol": "TCP"
                    }
                ],
                "resources": {
                    "limits": {
                        "cpu": "500m",
                        "memory": "128Mi"
                    },
                    "requests": {
                        "cpu": "5m",
                        "memory": "64Mi"
                    }
                },
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-j7pw9",
                        "readOnly": true
                    }
                ]
            },
            {
                "args": [
                    "--health-probe-bind-address=:8081",
                    "--metrics-bind-address=127.0.0.1:8080",
                    "--leader-elect"
                ],
                "command": [
                    "/manager"
                ],
                "image": "ghcr.io/aristanetworks/arista-ceoslab-operator:v2.0.1",
                "imagePullPolicy": "IfNotPresent",
                "livenessProbe": {
                    "failureThreshold": 3,
                    "httpGet": {
                        "path": "/healthz",
                        "port": 8081,
                        "scheme": "HTTP"
                    },
                    "initialDelaySeconds": 15,
                    "periodSeconds": 20,
                    "successThreshold": 1,
                    "timeoutSeconds": 1
                },
                "name": "manager",
                "readinessProbe": {
                    "failureThreshold": 3,
                    "httpGet": {
                        "path": "/readyz",
                        "port": 8081,
                        "scheme": "HTTP"
                    },
                    "initialDelaySeconds": 5,
                    "periodSeconds": 10,
                    "successThreshold": 1,
                    "timeoutSeconds": 1
                },
                "resources": {
                    "limits": {
                        "cpu": "500m",
                        "memory": "128Mi"
                    },
                    "requests": {
                        "cpu": "10m",
                        "memory": "64Mi"
                    }
                },
                "securityContext": {
                    "allowPrivilegeEscalation": false
                },
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-j7pw9",
                        "readOnly": true
                    }
                ]
            }
        ],
        "dnsPolicy": "ClusterFirst",
        "enableServiceLinks": true,
        "nodeName": "kne-control-plane",
        "preemptionPolicy": "PreemptLowerPriority",
        "priority": 0,
        "restartPolicy": "Always",
        "schedulerName": "default-scheduler",
        "securityContext": {
            "runAsNonRoot": true
        },
        "serviceAccount": "arista-ceoslab-operator-controller-manager",
        "serviceAccountName": "arista-ceoslab-operator-controller-manager",
        "terminationGracePeriodSeconds": 10,
        "tolerations": [
            {
                "effect": "NoExecute",
                "key": "node.kubernetes.io/not-ready",
                "operator": "Exists",
                "tolerationSeconds": 300
            },
            {
                "effect": "NoExecute",
                "key": "node.kubernetes.io/unreachable",
                "operator": "Exists",
                "tolerationSeconds": 300
            }
        ],
        "volumes": [
            {
                "name": "kube-api-access-j7pw9",
                "projected": {
                    "defaultMode": 420,
                    "sources": [
                        {
                            "serviceAccountToken": {
                                "expirationSeconds": 3607,
                                "path": "token"
                            }
                        },
                        {
                            "configMap": {
                                "items": [
                                    {
                                        "key": "ca.crt",
                                        "path": "ca.crt"
                                    }
                                ],
                                "name": "kube-root-ca.crt"
                            }
                        },
                        {
                            "downwardAPI": {
                                "items": [
                                    {
                                        "fieldRef": {
                                            "apiVersion": "v1",
                                            "fieldPath": "metadata.namespace"
                                        },
                                        "path": "namespace"
                                    }
                                ]
                            }
                        }
                    ]
                }
            }
        ]
    },
    "status": {
        "conditions": [
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:41:45Z",
                "status": "True",
                "type": "Initialized"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:41:55Z",
                "status": "True",
                "type": "Ready"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:41:55Z",
                "status": "True",
                "type": "ContainersReady"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:41:45Z",
                "status": "True",
                "type": "PodScheduled"
            }
        ],
        "containerStatuses": [
            {
                "containerID": "containerd://6b2c82de9295a77cee346280ec605a66e53316dd51d6db19132017c025683445",
                "image": "gcr.io/kubebuilder/kube-rbac-proxy:v0.11.0",
                "imageID": "gcr.io/kubebuilder/kube-rbac-proxy@sha256:0df4ae70e3bd0feffcec8f5cdb428f4abe666b667af991269ec5cb0bbda65869",
                "lastState": {},
                "name": "kube-rbac-proxy",
                "ready": true,
                "restartCount": 0,
                "started": true,
                "state": {
                    "running": {
                        "startedAt": "2023-03-24T16:41:48Z"
                    }
                }
            },
            {
                "containerID": "containerd://e1dced1c61e7ec5cff99208579b8ab25aae8d8080c07d4b0b2b41340541e7dba",
                "image": "ghcr.io/aristanetworks/arista-ceoslab-operator:v2.0.1",
                "imageID": "ghcr.io/aristanetworks/arista-ceoslab-operator@sha256:cd7c12b30096843b20304911705b178db4c13c915223b0e58a6d4c4f800c24d1",
                "lastState": {},
                "name": "manager",
                "ready": true,
                "restartCount": 0,
                "started": true,
                "state": {
                    "running": {
                        "startedAt": "2023-03-24T16:41:50Z"
                    }
                }
            }
        ],
        "hostIP": "192.168.8.2",
        "phase": "Running",
        "podIP": "10.244.0.8",
        "podIPs": [
            {
                "ip": "10.244.0.8"
            }
        ],
        "qosClass": "Burstable",
        "startTime": "2023-03-24T16:41:45Z"
    }
}
`
	ceos4status = PodStatus{
		Name:      "arista-ceoslab-operator-controller-manager-66cb57484f-86lcz",
		UID:       "cdbf21b3-a2e3-4c26-95a5-956945c8c122",
		Namespace: "arista-ceoslab-operator-system",
		Phase:     "Running",
		Containers: []ContainerStatus{
			{
				Name:    "kube-rbac-proxy",
				Image:   "gcr.io/kubebuilder/kube-rbac-proxy:v0.11.0",
				Ready:   true,
				Reason:  "",
				Message: ""},
			{
				Name:    "manager",
				Image:   "ghcr.io/aristanetworks/arista-ceoslab-operator:v2.0.1",
				Ready:   true,
				Reason:  "",
				Message: ""},
		},
	}

	ceos5data = `{
    "apiVersion": "v1",
    "kind": "Pod",
    "metadata": {
        "creationTimestamp": "2023-03-24T16:42:12Z",
        "labels": {
            "app": "ceos",
            "model": "ceos",
            "os": "eos",
            "topo": "multivendor",
            "vendor": "ARISTA",
            "version": ""
        },
        "name": "ceos",
        "namespace": "multivendor",
        "ownerReferences": [
            {
                "apiVersion": "ceoslab.arista.com/v1alpha1",
                "blockOwnerDeletion": true,
                "controller": true,
                "kind": "CEosLabDevice",
                "name": "ceos",
                "uid": "4c92d4bb-afa4-4320-9157-8d5371df1510"
            }
        ],
        "resourceVersion": "1149",
        "uid": "ec41a7f2-4f33-4eaf-8d34-df552c81445d"
    },
    "spec": {
        "containers": [
            {
                "args": [
                    "systemd.setenv=CEOS=1",
                    "systemd.setenv=EOS_PLATFORM=ceoslab",
                    "systemd.setenv=ETBA=1",
                    "systemd.setenv=INTFTYPE=eth",
                    "systemd.setenv=SKIP_ZEROTOUCH_BARRIER_IN_SYSDBINIT=1",
                    "systemd.setenv=container=docker"
                ],
                "command": [
                    "/sbin/init"
                ],
                "env": [
                    {
                        "name": "CEOS",
                        "value": "1"
                    },
                    {
                        "name": "EOS_PLATFORM",
                        "value": "ceoslab"
                    },
                    {
                        "name": "ETBA",
                        "value": "1"
                    },
                    {
                        "name": "INTFTYPE",
                        "value": "eth"
                    },
                    {
                        "name": "SKIP_ZEROTOUCH_BARRIER_IN_SYSDBINIT",
                        "value": "1"
                    },
                    {
                        "name": "container",
                        "value": "docker"
                    }
                ],
                "image": "ceos:latest",
                "imagePullPolicy": "IfNotPresent",
                "name": "ceos",
                "resources": {
                    "requests": {
                        "cpu": "500m",
                        "memory": "1Gi"
                    }
                },
                "securityContext": {
                    "privileged": true
                },
                "startupProbe": {
                    "exec": {
                        "command": [
                            "wfw",
                            "-t",
                            "5"
                        ]
                    },
                    "failureThreshold": 24,
                    "periodSeconds": 5,
                    "successThreshold": 1,
                    "timeoutSeconds": 5
                },
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/mnt/flash/startup-config",
                        "name": "volume-ceos-config",
                        "subPath": "startup-config"
                    },
                    {
                        "mountPath": "/mnt/flash/EosIntfMapping.json",
                        "name": "volume-configmap-intfmapping-ceos",
                        "subPath": "EosIntfMapping.json"
                    },
                    {
                        "mountPath": "/mnt/flash/rc.eos",
                        "name": "volume-configmap-rceos-ceos",
                        "subPath": "rc.eos"
                    },
                    {
                        "mountPath": "/mnt/flash/gnmiCert.pem",
                        "name": "volume-secret-selfsigned-ceos-0",
                        "subPath": "gnmiCert.pem"
                    },
                    {
                        "mountPath": "/mnt/flash/gnmiCertKey.pem",
                        "name": "volume-secret-selfsigned-ceos-0",
                        "subPath": "gnmiCertKey.pem"
                    },
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-h9hc8",
                        "readOnly": true
                    }
                ]
            }
        ],
        "dnsPolicy": "ClusterFirst",
        "enableServiceLinks": true,
        "initContainers": [
            {
                "args": [
                    "11",
                    "0"
                ],
                "image": "networkop/init-wait:latest",
                "imagePullPolicy": "IfNotPresent",
                "name": "init-ceos",
                "resources": {},
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-h9hc8",
                        "readOnly": true
                    }
                ]
            }
        ],
        "preemptionPolicy": "PreemptLowerPriority",
        "priority": 0,
        "restartPolicy": "Always",
        "schedulerName": "default-scheduler",
        "securityContext": {},
        "serviceAccount": "default",
        "serviceAccountName": "default",
        "terminationGracePeriodSeconds": 0,
        "tolerations": [
            {
                "effect": "NoExecute",
                "key": "node.kubernetes.io/not-ready",
                "operator": "Exists",
                "tolerationSeconds": 300
            },
            {
                "effect": "NoExecute",
                "key": "node.kubernetes.io/unreachable",
                "operator": "Exists",
                "tolerationSeconds": 300
            }
        ],
        "volumes": [
            {
                "configMap": {
                    "defaultMode": 420,
                    "name": "ceos-config"
                },
                "name": "volume-ceos-config"
            },
            {
                "configMap": {
                    "defaultMode": 420,
                    "name": "configmap-intfmapping-ceos"
                },
                "name": "volume-configmap-intfmapping-ceos"
            },
            {
                "configMap": {
                    "defaultMode": 509,
                    "name": "configmap-rceos-ceos"
                },
                "name": "volume-configmap-rceos-ceos"
            },
            {
                "name": "volume-secret-selfsigned-ceos-0",
                "secret": {
                    "defaultMode": 420,
                    "secretName": "secret-selfsigned-ceos-0"
                }
            },
            {
                "name": "kube-api-access-h9hc8",
                "projected": {
                    "defaultMode": 420,
                    "sources": [
                        {
                            "serviceAccountToken": {
                                "expirationSeconds": 3607,
                                "path": "token"
                            }
                        },
                        {
                            "configMap": {
                                "items": [
                                    {
                                        "key": "ca.crt",
                                        "path": "ca.crt"
                                    }
                                ],
                                "name": "kube-root-ca.crt"
                            }
                        },
                        {
                            "downwardAPI": {
                                "items": [
                                    {
                                        "fieldRef": {
                                            "apiVersion": "v1",
                                            "fieldPath": "metadata.namespace"
                                        },
                                        "path": "namespace"
                                    }
                                ]
                            }
                        }
                    ]
                }
            }
        ]
    },
    "status": {
        "phase": "Pending",
        "qosClass": "Burstable"
    }
}
`
	ceos5status = PodStatus{
		Name:      "ceos",
		UID:       "ec41a7f2-4f33-4eaf-8d34-df552c81445d",
		Namespace: "multivendor",
		Phase:     "Pending",
	}

	ceos6data = `{
    "apiVersion": "v1",
    "kind": "Pod",
    "metadata": {
        "creationTimestamp": "2023-03-24T16:42:12Z",
        "labels": {
            "app": "ceos",
            "model": "ceos",
            "os": "eos",
            "topo": "multivendor",
            "vendor": "ARISTA",
            "version": ""
        },
        "name": "ceos",
        "namespace": "multivendor",
        "ownerReferences": [
            {
                "apiVersion": "ceoslab.arista.com/v1alpha1",
                "blockOwnerDeletion": true,
                "controller": true,
                "kind": "CEosLabDevice",
                "name": "ceos",
                "uid": "4c92d4bb-afa4-4320-9157-8d5371df1510"
            }
        ],
        "resourceVersion": "1158",
        "uid": "ec41a7f2-4f33-4eaf-8d34-df552c81445d"
    },
    "spec": {
        "containers": [
            {
                "args": [
                    "systemd.setenv=CEOS=1",
                    "systemd.setenv=EOS_PLATFORM=ceoslab",
                    "systemd.setenv=ETBA=1",
                    "systemd.setenv=INTFTYPE=eth",
                    "systemd.setenv=SKIP_ZEROTOUCH_BARRIER_IN_SYSDBINIT=1",
                    "systemd.setenv=container=docker"
                ],
                "command": [
                    "/sbin/init"
                ],
                "env": [
                    {
                        "name": "CEOS",
                        "value": "1"
                    },
                    {
                        "name": "EOS_PLATFORM",
                        "value": "ceoslab"
                    },
                    {
                        "name": "ETBA",
                        "value": "1"
                    },
                    {
                        "name": "INTFTYPE",
                        "value": "eth"
                    },
                    {
                        "name": "SKIP_ZEROTOUCH_BARRIER_IN_SYSDBINIT",
                        "value": "1"
                    },
                    {
                        "name": "container",
                        "value": "docker"
                    }
                ],
                "image": "ceos:latest",
                "imagePullPolicy": "IfNotPresent",
                "name": "ceos",
                "resources": {
                    "requests": {
                        "cpu": "500m",
                        "memory": "1Gi"
                    }
                },
                "securityContext": {
                    "privileged": true
                },
                "startupProbe": {
                    "exec": {
                        "command": [
                            "wfw",
                            "-t",
                            "5"
                        ]
                    },
                    "failureThreshold": 24,
                    "periodSeconds": 5,
                    "successThreshold": 1,
                    "timeoutSeconds": 5
                },
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/mnt/flash/startup-config",
                        "name": "volume-ceos-config",
                        "subPath": "startup-config"
                    },
                    {
                        "mountPath": "/mnt/flash/EosIntfMapping.json",
                        "name": "volume-configmap-intfmapping-ceos",
                        "subPath": "EosIntfMapping.json"
                    },
                    {
                        "mountPath": "/mnt/flash/rc.eos",
                        "name": "volume-configmap-rceos-ceos",
                        "subPath": "rc.eos"
                    },
                    {
                        "mountPath": "/mnt/flash/gnmiCert.pem",
                        "name": "volume-secret-selfsigned-ceos-0",
                        "subPath": "gnmiCert.pem"
                    },
                    {
                        "mountPath": "/mnt/flash/gnmiCertKey.pem",
                        "name": "volume-secret-selfsigned-ceos-0",
                        "subPath": "gnmiCertKey.pem"
                    },
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-h9hc8",
                        "readOnly": true
                    }
                ]
            }
        ],
        "dnsPolicy": "ClusterFirst",
        "enableServiceLinks": true,
        "initContainers": [
            {
                "args": [
                    "11",
                    "0"
                ],
                "image": "networkop/init-wait:latest",
                "imagePullPolicy": "IfNotPresent",
                "name": "init-ceos",
                "resources": {},
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-h9hc8",
                        "readOnly": true
                    }
                ]
            }
        ],
        "nodeName": "kne-control-plane",
        "preemptionPolicy": "PreemptLowerPriority",
        "priority": 0,
        "restartPolicy": "Always",
        "schedulerName": "default-scheduler",
        "securityContext": {},
        "serviceAccount": "default",
        "serviceAccountName": "default",
        "terminationGracePeriodSeconds": 0,
        "tolerations": [
            {
                "effect": "NoExecute",
                "key": "node.kubernetes.io/not-ready",
                "operator": "Exists",
                "tolerationSeconds": 300
            },
            {
                "effect": "NoExecute",
                "key": "node.kubernetes.io/unreachable",
                "operator": "Exists",
                "tolerationSeconds": 300
            }
        ],
        "volumes": [
            {
                "configMap": {
                    "defaultMode": 420,
                    "name": "ceos-config"
                },
                "name": "volume-ceos-config"
            },
            {
                "configMap": {
                    "defaultMode": 420,
                    "name": "configmap-intfmapping-ceos"
                },
                "name": "volume-configmap-intfmapping-ceos"
            },
            {
                "configMap": {
                    "defaultMode": 509,
                    "name": "configmap-rceos-ceos"
                },
                "name": "volume-configmap-rceos-ceos"
            },
            {
                "name": "volume-secret-selfsigned-ceos-0",
                "secret": {
                    "defaultMode": 420,
                    "secretName": "secret-selfsigned-ceos-0"
                }
            },
            {
                "name": "kube-api-access-h9hc8",
                "projected": {
                    "defaultMode": 420,
                    "sources": [
                        {
                            "serviceAccountToken": {
                                "expirationSeconds": 3607,
                                "path": "token"
                            }
                        },
                        {
                            "configMap": {
                                "items": [
                                    {
                                        "key": "ca.crt",
                                        "path": "ca.crt"
                                    }
                                ],
                                "name": "kube-root-ca.crt"
                            }
                        },
                        {
                            "downwardAPI": {
                                "items": [
                                    {
                                        "fieldRef": {
                                            "apiVersion": "v1",
                                            "fieldPath": "metadata.namespace"
                                        },
                                        "path": "namespace"
                                    }
                                ]
                            }
                        }
                    ]
                }
            }
        ]
    },
    "status": {
        "conditions": [
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:42:12Z",
                "message": "containers with incomplete status: [init-ceos]",
                "reason": "ContainersNotInitialized",
                "status": "False",
                "type": "Initialized"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:42:12Z",
                "message": "containers with unready status: [ceos]",
                "reason": "ContainersNotReady",
                "status": "False",
                "type": "Ready"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:42:12Z",
                "message": "containers with unready status: [ceos]",
                "reason": "ContainersNotReady",
                "status": "False",
                "type": "ContainersReady"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:42:12Z",
                "status": "True",
                "type": "PodScheduled"
            }
        ],
        "containerStatuses": [
            {
                "image": "ceos:latest",
                "imageID": "",
                "lastState": {},
                "name": "ceos",
                "ready": false,
                "restartCount": 0,
                "started": false,
                "state": {
                    "waiting": {
                        "reason": "PodInitializing"
                    }
                }
            }
        ],
        "hostIP": "192.168.8.2",
        "initContainerStatuses": [
            {
                "image": "networkop/init-wait:latest",
                "imageID": "",
                "lastState": {},
                "name": "init-ceos",
                "ready": false,
                "restartCount": 0,
                "state": {
                    "waiting": {
                        "reason": "PodInitializing"
                    }
                }
            },
            {
                "image": "networkop/init-wait:latest",
                "imageID": "",
                "lastState": {},
                "name": "init2-ceos",
                "ready": false,
                "restartCount": 0,
                "state": {
                    "waiting": {
                        "reason": "PodInitializing"
                    }
                }
            }
        ],
        "phase": "Pending",
        "qosClass": "Burstable",
        "startTime": "2023-03-24T16:42:12Z"
    }
}
`
	ceos6status = PodStatus{
		Name:      "ceos",
		UID:       "ec41a7f2-4f33-4eaf-8d34-df552c81445d",
		Namespace: "multivendor",
		Phase:     "Pending",
		Containers: []ContainerStatus{
			{
				Name:    "ceos",
				Image:   "ceos:latest",
				Ready:   false,
				Reason:  "PodInitializing",
				Message: ""},
		},
		InitContainers: []ContainerStatus{
			{
				Name:   "init-ceos",
				Image:  "networkop/init-wait:latest",
				Reason: "PodInitializing",
			},
			{
				Name:   "init2-ceos",
				Image:  "networkop/init-wait:latest",
				Reason: "PodInitializing",
			},
		},
	}

	ceos6Idata = `{
    "apiVersion": "v1",
    "kind": "Pod",
    "metadata": {
        "creationTimestamp": "2023-03-24T16:42:12Z",
        "labels": {
            "app": "ceos",
            "model": "ceos",
            "os": "eos",
            "topo": "multivendor",
            "vendor": "ARISTA",
            "version": ""
        },
        "name": "ceos",
        "namespace": "multivendor",
        "ownerReferences": [
            {
                "apiVersion": "ceoslab.arista.com/v1alpha1",
                "blockOwnerDeletion": true,
                "controller": true,
                "kind": "CEosLabDevice",
                "name": "ceos",
                "uid": "4c92d4bb-afa4-4320-9157-8d5371df1510"
            }
        ],
        "resourceVersion": "1158",
        "uid": "ec41a7f2-4f33-4eaf-8d34-df552c81445d"
    },
    "spec": {
        "containers": [
            {
                "args": [
                    "systemd.setenv=CEOS=1",
                    "systemd.setenv=EOS_PLATFORM=ceoslab",
                    "systemd.setenv=ETBA=1",
                    "systemd.setenv=INTFTYPE=eth",
                    "systemd.setenv=SKIP_ZEROTOUCH_BARRIER_IN_SYSDBINIT=1",
                    "systemd.setenv=container=docker"
                ],
                "command": [
                    "/sbin/init"
                ],
                "env": [
                    {
                        "name": "CEOS",
                        "value": "1"
                    },
                    {
                        "name": "EOS_PLATFORM",
                        "value": "ceoslab"
                    },
                    {
                        "name": "ETBA",
                        "value": "1"
                    },
                    {
                        "name": "INTFTYPE",
                        "value": "eth"
                    },
                    {
                        "name": "SKIP_ZEROTOUCH_BARRIER_IN_SYSDBINIT",
                        "value": "1"
                    },
                    {
                        "name": "container",
                        "value": "docker"
                    }
                ],
                "image": "ceos:latest",
                "imagePullPolicy": "IfNotPresent",
                "name": "ceos",
                "resources": {
                    "requests": {
                        "cpu": "500m",
                        "memory": "1Gi"
                    }
                },
                "securityContext": {
                    "privileged": true
                },
                "startupProbe": {
                    "exec": {
                        "command": [
                            "wfw",
                            "-t",
                            "5"
                        ]
                    },
                    "failureThreshold": 24,
                    "periodSeconds": 5,
                    "successThreshold": 1,
                    "timeoutSeconds": 5
                },
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/mnt/flash/startup-config",
                        "name": "volume-ceos-config",
                        "subPath": "startup-config"
                    },
                    {
                        "mountPath": "/mnt/flash/EosIntfMapping.json",
                        "name": "volume-configmap-intfmapping-ceos",
                        "subPath": "EosIntfMapping.json"
                    },
                    {
                        "mountPath": "/mnt/flash/rc.eos",
                        "name": "volume-configmap-rceos-ceos",
                        "subPath": "rc.eos"
                    },
                    {
                        "mountPath": "/mnt/flash/gnmiCert.pem",
                        "name": "volume-secret-selfsigned-ceos-0",
                        "subPath": "gnmiCert.pem"
                    },
                    {
                        "mountPath": "/mnt/flash/gnmiCertKey.pem",
                        "name": "volume-secret-selfsigned-ceos-0",
                        "subPath": "gnmiCertKey.pem"
                    },
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-h9hc8",
                        "readOnly": true
                    }
                ]
            }
        ],
        "dnsPolicy": "ClusterFirst",
        "enableServiceLinks": true,
        "initContainers": [
            {
                "args": [
                    "11",
                    "0"
                ],
                "image": "networkop/init-wait:latest",
                "imagePullPolicy": "IfNotPresent",
                "name": "init-ceos",
                "resources": {},
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-h9hc8",
                        "readOnly": true
                    }
                ]
            }
        ],
        "nodeName": "kne-control-plane",
        "preemptionPolicy": "PreemptLowerPriority",
        "priority": 0,
        "restartPolicy": "Always",
        "schedulerName": "default-scheduler",
        "securityContext": {},
        "serviceAccount": "default",
        "serviceAccountName": "default",
        "terminationGracePeriodSeconds": 0,
        "tolerations": [
            {
                "effect": "NoExecute",
                "key": "node.kubernetes.io/not-ready",
                "operator": "Exists",
                "tolerationSeconds": 300
            },
            {
                "effect": "NoExecute",
                "key": "node.kubernetes.io/unreachable",
                "operator": "Exists",
                "tolerationSeconds": 300
            }
        ],
        "volumes": [
            {
                "configMap": {
                    "defaultMode": 420,
                    "name": "ceos-config"
                },
                "name": "volume-ceos-config"
            },
            {
                "configMap": {
                    "defaultMode": 420,
                    "name": "configmap-intfmapping-ceos"
                },
                "name": "volume-configmap-intfmapping-ceos"
            },
            {
                "configMap": {
                    "defaultMode": 509,
                    "name": "configmap-rceos-ceos"
                },
                "name": "volume-configmap-rceos-ceos"
            },
            {
                "name": "volume-secret-selfsigned-ceos-0",
                "secret": {
                    "defaultMode": 420,
                    "secretName": "secret-selfsigned-ceos-0"
                }
            },
            {
                "name": "kube-api-access-h9hc8",
                "projected": {
                    "defaultMode": 420,
                    "sources": [
                        {
                            "serviceAccountToken": {
                                "expirationSeconds": 3607,
                                "path": "token"
                            }
                        },
                        {
                            "configMap": {
                                "items": [
                                    {
                                        "key": "ca.crt",
                                        "path": "ca.crt"
                                    }
                                ],
                                "name": "kube-root-ca.crt"
                            }
                        },
                        {
                            "downwardAPI": {
                                "items": [
                                    {
                                        "fieldRef": {
                                            "apiVersion": "v1",
                                            "fieldPath": "metadata.namespace"
                                        },
                                        "path": "namespace"
                                    }
                                ]
                            }
                        }
                    ]
                }
            }
        ]
    },
    "status": {
        "conditions": [
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:42:12Z",
                "message": "containers with incomplete status: [init-ceos]",
                "reason": "ContainersNotInitialized",
                "status": "False",
                "type": "Initialized"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:42:12Z",
                "message": "containers with unready status: [ceos]",
                "reason": "ContainersNotReady",
                "status": "False",
                "type": "Ready"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:42:12Z",
                "message": "containers with unready status: [ceos]",
                "reason": "ContainersNotReady",
                "status": "False",
                "type": "ContainersReady"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:42:12Z",
                "status": "True",
                "type": "PodScheduled"
            }
        ],
        "containerStatuses": [
            {
                "image": "ceos:latest",
                "imageID": "",
                "lastState": {},
                "name": "ceos",
                "ready": false,
                "restartCount": 0,
                "started": false,
                "state": {
                    "waiting": {
                        "reason": "PodInitializing"
                    }
                }
            }
        ],
        "hostIP": "192.168.8.2",
        "initContainerStatuses": [
            {
                "image": "networkop/init-wait:latest",
                "imageID": "",
                "lastState": {},
                "name": "init-ceos",
                "ready": false,
                "restartCount": 0,
                "state": {
                    "waiting": {
                        "reason": "PodInitializing"
                    }
                }
            },
            {
                "image": "networkop/init-wait:latest",
                "imageID": "",
                "lastState": {},
                "name": "init2-ceos",
                "ready": false,
                "restartCount": 0,
                "state": {
                    "waiting": {
                        "reason": "PodInTheSky"
                    }
                }
            }
        ],
        "phase": "Pending",
        "qosClass": "Burstable",
        "startTime": "2023-03-24T16:42:12Z"
    }
}
`
	ceos6Istatus = PodStatus{
		Name:      "ceos",
		UID:       "ec41a7f2-4f33-4eaf-8d34-df552c81445d",
		Namespace: "multivendor",
		Phase:     "Pending",
		Containers: []ContainerStatus{
			{
				Name:    "ceos",
				Image:   "ceos:latest",
				Ready:   false,
				Reason:  "PodInitializing",
				Message: ""},
		},
		InitContainers: []ContainerStatus{
			{
				Name:   "init-ceos",
				Image:  "networkop/init-wait:latest",
				Reason: "PodInitializing",
			},
			{
				Name:   "init2-ceos",
				Image:  "networkop/init-wait:latest",
				Reason: "PodInTheSky",
			},
		},
	}
	ceos7data = `{
    "apiVersion": "v1",
    "kind": "Pod",
    "metadata": {
        "creationTimestamp": "2023-03-24T16:42:12Z",
        "labels": {
            "app": "ceos",
            "model": "ceos",
            "os": "eos",
            "topo": "multivendor",
            "vendor": "ARISTA",
            "version": ""
        },
        "name": "ceos",
        "namespace": "multivendor",
        "ownerReferences": [
            {
                "apiVersion": "ceoslab.arista.com/v1alpha1",
                "blockOwnerDeletion": true,
                "controller": true,
                "kind": "CEosLabDevice",
                "name": "ceos",
                "uid": "4c92d4bb-afa4-4320-9157-8d5371df1510"
            }
        ],
        "resourceVersion": "1404",
        "uid": "ec41a7f2-4f33-4eaf-8d34-df552c81445d"
    },
    "spec": {
        "containers": [
            {
                "args": [
                    "systemd.setenv=CEOS=1",
                    "systemd.setenv=EOS_PLATFORM=ceoslab",
                    "systemd.setenv=ETBA=1",
                    "systemd.setenv=INTFTYPE=eth",
                    "systemd.setenv=SKIP_ZEROTOUCH_BARRIER_IN_SYSDBINIT=1",
                    "systemd.setenv=container=docker"
                ],
                "command": [
                    "/sbin/init"
                ],
                "env": [
                    {
                        "name": "CEOS",
                        "value": "1"
                    },
                    {
                        "name": "EOS_PLATFORM",
                        "value": "ceoslab"
                    },
                    {
                        "name": "ETBA",
                        "value": "1"
                    },
                    {
                        "name": "INTFTYPE",
                        "value": "eth"
                    },
                    {
                        "name": "SKIP_ZEROTOUCH_BARRIER_IN_SYSDBINIT",
                        "value": "1"
                    },
                    {
                        "name": "container",
                        "value": "docker"
                    }
                ],
                "image": "ceos:latest",
                "imagePullPolicy": "IfNotPresent",
                "name": "ceos",
                "resources": {
                    "requests": {
                        "cpu": "500m",
                        "memory": "1Gi"
                    }
                },
                "securityContext": {
                    "privileged": true
                },
                "startupProbe": {
                    "exec": {
                        "command": [
                            "wfw",
                            "-t",
                            "5"
                        ]
                    },
                    "failureThreshold": 24,
                    "periodSeconds": 5,
                    "successThreshold": 1,
                    "timeoutSeconds": 5
                },
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/mnt/flash/startup-config",
                        "name": "volume-ceos-config",
                        "subPath": "startup-config"
                    },
                    {
                        "mountPath": "/mnt/flash/EosIntfMapping.json",
                        "name": "volume-configmap-intfmapping-ceos",
                        "subPath": "EosIntfMapping.json"
                    },
                    {
                        "mountPath": "/mnt/flash/rc.eos",
                        "name": "volume-configmap-rceos-ceos",
                        "subPath": "rc.eos"
                    },
                    {
                        "mountPath": "/mnt/flash/gnmiCert.pem",
                        "name": "volume-secret-selfsigned-ceos-0",
                        "subPath": "gnmiCert.pem"
                    },
                    {
                        "mountPath": "/mnt/flash/gnmiCertKey.pem",
                        "name": "volume-secret-selfsigned-ceos-0",
                        "subPath": "gnmiCertKey.pem"
                    },
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-h9hc8",
                        "readOnly": true
                    }
                ]
            }
        ],
        "dnsPolicy": "ClusterFirst",
        "enableServiceLinks": true,
        "initContainers": [
            {
                "args": [
                    "11",
                    "0"
                ],
                "image": "networkop/init-wait:latest",
                "imagePullPolicy": "IfNotPresent",
                "name": "init-ceos",
                "resources": {},
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-h9hc8",
                        "readOnly": true
                    }
                ]
            }
        ],
        "nodeName": "kne-control-plane",
        "preemptionPolicy": "PreemptLowerPriority",
        "priority": 0,
        "restartPolicy": "Always",
        "schedulerName": "default-scheduler",
        "securityContext": {},
        "serviceAccount": "default",
        "serviceAccountName": "default",
        "terminationGracePeriodSeconds": 0,
        "tolerations": [
            {
                "effect": "NoExecute",
                "key": "node.kubernetes.io/not-ready",
                "operator": "Exists",
                "tolerationSeconds": 300
            },
            {
                "effect": "NoExecute",
                "key": "node.kubernetes.io/unreachable",
                "operator": "Exists",
                "tolerationSeconds": 300
            }
        ],
        "volumes": [
            {
                "configMap": {
                    "defaultMode": 420,
                    "name": "ceos-config"
                },
                "name": "volume-ceos-config"
            },
            {
                "configMap": {
                    "defaultMode": 420,
                    "name": "configmap-intfmapping-ceos"
                },
                "name": "volume-configmap-intfmapping-ceos"
            },
            {
                "configMap": {
                    "defaultMode": 509,
                    "name": "configmap-rceos-ceos"
                },
                "name": "volume-configmap-rceos-ceos"
            },
            {
                "name": "volume-secret-selfsigned-ceos-0",
                "secret": {
                    "defaultMode": 420,
                    "secretName": "secret-selfsigned-ceos-0"
                }
            },
            {
                "name": "kube-api-access-h9hc8",
                "projected": {
                    "defaultMode": 420,
                    "sources": [
                        {
                            "serviceAccountToken": {
                                "expirationSeconds": 3607,
                                "path": "token"
                            }
                        },
                        {
                            "configMap": {
                                "items": [
                                    {
                                        "key": "ca.crt",
                                        "path": "ca.crt"
                                    }
                                ],
                                "name": "kube-root-ca.crt"
                            }
                        },
                        {
                            "downwardAPI": {
                                "items": [
                                    {
                                        "fieldRef": {
                                            "apiVersion": "v1",
                                            "fieldPath": "metadata.namespace"
                                        },
                                        "path": "namespace"
                                    }
                                ]
                            }
                        }
                    ]
                }
            }
        ]
    },
    "status": {
        "conditions": [
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:42:29Z",
                "status": "True",
                "type": "Initialized"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:42:12Z",
                "message": "containers with unready status: [ceos]",
                "reason": "ContainersNotReady",
                "status": "False",
                "type": "Ready"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:42:12Z",
                "message": "containers with unready status: [ceos]",
                "reason": "ContainersNotReady",
                "status": "False",
                "type": "ContainersReady"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:42:12Z",
                "status": "True",
                "type": "PodScheduled"
            }
        ],
        "containerStatuses": [
            {
                "image": "ceos:latest",
                "imageID": "",
                "lastState": {},
                "name": "ceos",
                "ready": false,
                "restartCount": 0,
                "started": false,
                "state": {
                    "waiting": {
                        "message": "rpc error: code = Unknown desc = failed to pull and unpack image \"docker.io/library/ceos:latest\": failed to resolve reference \"docker.io/library/ceos:latest\": pull access denied, repository does not exist or may require authorization: server message: insufficient_scope: authorization failed",
                        "reason": "ErrImagePull"
                    }
                }
            }
        ],
        "hostIP": "192.168.8.2",
        "initContainerStatuses": [
            {
                "containerID": "containerd://ba3b38aac71af3108010f5ca1f7e0698a0bc1aeb76959450539f41b70f937cab",
                "image": "docker.io/networkop/init-wait:latest",
                "imageID": "docker.io/networkop/init-wait@sha256:a54e253ce78be8ea66942051296c3676e254bc37460e19a9eb540517faaaf4d7",
                "lastState": {},
                "name": "init-ceos",
                "ready": true,
                "restartCount": 0,
                "state": {
                    "terminated": {
                        "containerID": "containerd://ba3b38aac71af3108010f5ca1f7e0698a0bc1aeb76959450539f41b70f937cab",
                        "exitCode": 0,
                        "finishedAt": "2023-03-24T16:42:29Z",
                        "reason": "Completed",
                        "startedAt": "2023-03-24T16:42:29Z"
                    }
                }
            }
        ],
        "phase": "Pending",
        "podIP": "10.244.0.17",
        "podIPs": [
            {
                "ip": "10.244.0.17"
            }
        ],
        "qosClass": "Burstable",
        "startTime": "2023-03-24T16:42:12Z"
    }
}
`
	ceos7status = PodStatus{
		Name:      "ceos",
		UID:       "ec41a7f2-4f33-4eaf-8d34-df552c81445d",
		Namespace: "multivendor",
		Phase:     "Pending",
		Containers: []ContainerStatus{
			{
				Name:    "ceos",
				Image:   "ceos:latest",
				Ready:   false,
				Reason:  "ErrImagePull",
				Message: `rpc error: code = Unknown desc = failed to pull and unpack image "docker.io/library/ceos:latest": failed to resolve reference "docker.io/library/ceos:latest": pull access denied, repository does not exist or may require authorization: server message: insufficient_scope: authorization failed`},
		},
		InitContainers: []ContainerStatus{
			{
				Name:  "init-ceos",
				Image: "docker.io/networkop/init-wait:latest",
				Ready: true,
			},
		},
	}

	ixia1data = `{
    "apiVersion": "v1",
    "kind": "Pod",
    "metadata": {
        "creationTimestamp": "2023-03-24T16:41:02Z",
        "generateName": "ixiatg-op-controller-manager-7b5db775d9-",
        "labels": {
            "control-plane": "controller-manager",
            "pod-template-hash": "7b5db775d9"
        },
        "name": "ixiatg-op-controller-manager-7b5db775d9-kz2mb",
        "namespace": "ixiatg-op-system",
        "ownerReferences": [
            {
                "apiVersion": "apps/v1",
                "blockOwnerDeletion": true,
                "controller": true,
                "kind": "ReplicaSet",
                "name": "ixiatg-op-controller-manager-7b5db775d9",
                "uid": "7960b609-1877-4fb8-8010-5a9fbd4695c7"
            }
        ],
        "resourceVersion": "621",
        "uid": "53fd2ce7-bcf3-489a-9f4a-aa457e01980f"
    },
    "spec": {
        "containers": [
            {
                "args": [
                    "--secure-listen-address=0.0.0.0:8443",
                    "--upstream=http://127.0.0.1:8080/",
                    "--logtostderr=true",
                    "--v=10"
                ],
                "image": "gcr.io/kubebuilder/kube-rbac-proxy:v0.8.0",
                "imagePullPolicy": "IfNotPresent",
                "name": "kube-rbac-proxy",
                "ports": [
                    {
                        "containerPort": 8443,
                        "name": "https",
                        "protocol": "TCP"
                    }
                ],
                "resources": {},
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-c9mg6",
                        "readOnly": true
                    }
                ]
            },
            {
                "args": [
                    "--health-probe-bind-address=:8081",
                    "--metrics-bind-address=127.0.0.1:8080",
                    "--leader-elect"
                ],
                "command": [
                    "/manager"
                ],
                "image": "ghcr.io/open-traffic-generator/ixia-c-operator:0.3.1",
                "imagePullPolicy": "IfNotPresent",
                "livenessProbe": {
                    "failureThreshold": 3,
                    "httpGet": {
                        "path": "/healthz",
                        "port": 8081,
                        "scheme": "HTTP"
                    },
                    "initialDelaySeconds": 15,
                    "periodSeconds": 20,
                    "successThreshold": 1,
                    "timeoutSeconds": 1
                },
                "name": "manager",
                "readinessProbe": {
                    "failureThreshold": 3,
                    "httpGet": {
                        "path": "/readyz",
                        "port": 8081,
                        "scheme": "HTTP"
                    },
                    "initialDelaySeconds": 5,
                    "periodSeconds": 10,
                    "successThreshold": 1,
                    "timeoutSeconds": 1
                },
                "resources": {
                    "limits": {
                        "cpu": "100m",
                        "memory": "200Mi"
                    },
                    "requests": {
                        "cpu": "100m",
                        "memory": "20Mi"
                    }
                },
                "securityContext": {
                    "allowPrivilegeEscalation": false
                },
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-c9mg6",
                        "readOnly": true
                    }
                ]
            }
        ],
        "dnsPolicy": "ClusterFirst",
        "enableServiceLinks": true,
        "imagePullSecrets": [
            {
                "name": "ixia-pull-secret"
            }
        ],
        "nodeName": "kne-control-plane",
        "preemptionPolicy": "PreemptLowerPriority",
        "priority": 0,
        "restartPolicy": "Always",
        "schedulerName": "default-scheduler",
        "securityContext": {
            "runAsNonRoot": true
        },
        "serviceAccount": "ixiatg-op-controller-manager",
        "serviceAccountName": "ixiatg-op-controller-manager",
        "terminationGracePeriodSeconds": 10,
        "tolerations": [
            {
                "effect": "NoExecute",
                "key": "node.kubernetes.io/not-ready",
                "operator": "Exists",
                "tolerationSeconds": 300
            },
            {
                "effect": "NoExecute",
                "key": "node.kubernetes.io/unreachable",
                "operator": "Exists",
                "tolerationSeconds": 300
            }
        ],
        "volumes": [
            {
                "name": "kube-api-access-c9mg6",
                "projected": {
                    "defaultMode": 420,
                    "sources": [
                        {
                            "serviceAccountToken": {
                                "expirationSeconds": 3607,
                                "path": "token"
                            }
                        },
                        {
                            "configMap": {
                                "items": [
                                    {
                                        "key": "ca.crt",
                                        "path": "ca.crt"
                                    }
                                ],
                                "name": "kube-root-ca.crt"
                            }
                        },
                        {
                            "downwardAPI": {
                                "items": [
                                    {
                                        "fieldRef": {
                                            "apiVersion": "v1",
                                            "fieldPath": "metadata.namespace"
                                        },
                                        "path": "namespace"
                                    }
                                ]
                            }
                        }
                    ]
                }
            }
        ]
    },
    "status": {
        "conditions": [
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:41:02Z",
                "status": "True",
                "type": "PodScheduled"
            }
        ],
        "phase": "Pending",
        "qosClass": "Burstable"
    }
}
`
	ixia1status = PodStatus{
		Name:      "ixiatg-op-controller-manager-7b5db775d9-kz2mb",
		UID:       "53fd2ce7-bcf3-489a-9f4a-aa457e01980f",
		Namespace: "ixiatg-op-system",
		Phase:     "Pending",
	}

	ixia2data = `{
    "apiVersion": "v1",
    "kind": "Pod",
    "metadata": {
        "creationTimestamp": "2023-03-24T16:41:02Z",
        "generateName": "ixiatg-op-controller-manager-7b5db775d9-",
        "labels": {
            "control-plane": "controller-manager",
            "pod-template-hash": "7b5db775d9"
        },
        "name": "ixiatg-op-controller-manager-7b5db775d9-kz2mb",
        "namespace": "ixiatg-op-system",
        "ownerReferences": [
            {
                "apiVersion": "apps/v1",
                "blockOwnerDeletion": true,
                "controller": true,
                "kind": "ReplicaSet",
                "name": "ixiatg-op-controller-manager-7b5db775d9",
                "uid": "7960b609-1877-4fb8-8010-5a9fbd4695c7"
            }
        ],
        "resourceVersion": "626",
        "uid": "53fd2ce7-bcf3-489a-9f4a-aa457e01980f"
    },
    "spec": {
        "containers": [
            {
                "args": [
                    "--secure-listen-address=0.0.0.0:8443",
                    "--upstream=http://127.0.0.1:8080/",
                    "--logtostderr=true",
                    "--v=10"
                ],
                "image": "gcr.io/kubebuilder/kube-rbac-proxy:v0.8.0",
                "imagePullPolicy": "IfNotPresent",
                "name": "kube-rbac-proxy",
                "ports": [
                    {
                        "containerPort": 8443,
                        "name": "https",
                        "protocol": "TCP"
                    }
                ],
                "resources": {},
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-c9mg6",
                        "readOnly": true
                    }
                ]
            },
            {
                "args": [
                    "--health-probe-bind-address=:8081",
                    "--metrics-bind-address=127.0.0.1:8080",
                    "--leader-elect"
                ],
                "command": [
                    "/manager"
                ],
                "image": "ghcr.io/open-traffic-generator/ixia-c-operator:0.3.1",
                "imagePullPolicy": "IfNotPresent",
                "livenessProbe": {
                    "failureThreshold": 3,
                    "httpGet": {
                        "path": "/healthz",
                        "port": 8081,
                        "scheme": "HTTP"
                    },
                    "initialDelaySeconds": 15,
                    "periodSeconds": 20,
                    "successThreshold": 1,
                    "timeoutSeconds": 1
                },
                "name": "manager",
                "readinessProbe": {
                    "failureThreshold": 3,
                    "httpGet": {
                        "path": "/readyz",
                        "port": 8081,
                        "scheme": "HTTP"
                    },
                    "initialDelaySeconds": 5,
                    "periodSeconds": 10,
                    "successThreshold": 1,
                    "timeoutSeconds": 1
                },
                "resources": {
                    "limits": {
                        "cpu": "100m",
                        "memory": "200Mi"
                    },
                    "requests": {
                        "cpu": "100m",
                        "memory": "20Mi"
                    }
                },
                "securityContext": {
                    "allowPrivilegeEscalation": false
                },
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-c9mg6",
                        "readOnly": true
                    }
                ]
            }
        ],
        "dnsPolicy": "ClusterFirst",
        "enableServiceLinks": true,
        "imagePullSecrets": [
            {
                "name": "ixia-pull-secret"
            }
        ],
        "nodeName": "kne-control-plane",
        "preemptionPolicy": "PreemptLowerPriority",
        "priority": 0,
        "restartPolicy": "Always",
        "schedulerName": "default-scheduler",
        "securityContext": {
            "runAsNonRoot": true
        },
        "serviceAccount": "ixiatg-op-controller-manager",
        "serviceAccountName": "ixiatg-op-controller-manager",
        "terminationGracePeriodSeconds": 10,
        "tolerations": [
            {
                "effect": "NoExecute",
                "key": "node.kubernetes.io/not-ready",
                "operator": "Exists",
                "tolerationSeconds": 300
            },
            {
                "effect": "NoExecute",
                "key": "node.kubernetes.io/unreachable",
                "operator": "Exists",
                "tolerationSeconds": 300
            }
        ],
        "volumes": [
            {
                "name": "kube-api-access-c9mg6",
                "projected": {
                    "defaultMode": 420,
                    "sources": [
                        {
                            "serviceAccountToken": {
                                "expirationSeconds": 3607,
                                "path": "token"
                            }
                        },
                        {
                            "configMap": {
                                "items": [
                                    {
                                        "key": "ca.crt",
                                        "path": "ca.crt"
                                    }
                                ],
                                "name": "kube-root-ca.crt"
                            }
                        },
                        {
                            "downwardAPI": {
                                "items": [
                                    {
                                        "fieldRef": {
                                            "apiVersion": "v1",
                                            "fieldPath": "metadata.namespace"
                                        },
                                        "path": "namespace"
                                    }
                                ]
                            }
                        }
                    ]
                }
            }
        ]
    },
    "status": {
        "conditions": [
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:41:02Z",
                "status": "True",
                "type": "Initialized"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:41:02Z",
                "message": "containers with unready status: [kube-rbac-proxy manager]",
                "reason": "ContainersNotReady",
                "status": "False",
                "type": "Ready"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:41:02Z",
                "message": "containers with unready status: [kube-rbac-proxy manager]",
                "reason": "ContainersNotReady",
                "status": "False",
                "type": "ContainersReady"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:41:02Z",
                "status": "True",
                "type": "PodScheduled"
            }
        ],
        "containerStatuses": [
            {
                "image": "gcr.io/kubebuilder/kube-rbac-proxy:v0.8.0",
                "imageID": "",
                "lastState": {},
                "name": "kube-rbac-proxy",
                "ready": false,
                "restartCount": 0,
                "started": false,
                "state": {
                    "waiting": {
                        "reason": "ContainerCreating"
                    }
                }
            },
            {
                "image": "ghcr.io/open-traffic-generator/ixia-c-operator:0.3.1",
                "imageID": "",
                "lastState": {},
                "name": "manager",
                "ready": false,
                "restartCount": 0,
                "started": false,
                "state": {
                    "waiting": {
                        "reason": "ContainerCreating"
                    }
                }
            }
        ],
        "hostIP": "192.168.8.2",
        "phase": "Pending",
        "qosClass": "Burstable",
        "startTime": "2023-03-24T16:41:02Z"
    }
}
`
	ixia2status = PodStatus{
		Name:      "ixiatg-op-controller-manager-7b5db775d9-kz2mb",
		UID:       "53fd2ce7-bcf3-489a-9f4a-aa457e01980f",
		Namespace: "ixiatg-op-system",
		Phase:     "Pending",
		Containers: []ContainerStatus{
			{
				Name:   "kube-rbac-proxy",
				Image:  "gcr.io/kubebuilder/kube-rbac-proxy:v0.8.0",
				Reason: "ContainerCreating",
			},
			{
				Name:   "manager",
				Image:  "ghcr.io/open-traffic-generator/ixia-c-operator:0.3.1",
				Reason: "ContainerCreating",
			},
		},
	}

	meshnet1data = `{
    "apiVersion": "v1",
    "kind": "Pod",
    "metadata": {
        "creationTimestamp": "2023-03-24T16:41:02Z",
        "generateName": "meshnet-",
        "labels": {
            "app": "meshnet",
            "controller-revision-hash": "75558679b9",
            "name": "meshnet",
            "pod-template-generation": "1"
        },
        "name": "meshnet-gdsj6",
        "namespace": "meshnet",
        "ownerReferences": [
            {
                "apiVersion": "apps/v1",
                "blockOwnerDeletion": true,
                "controller": true,
                "kind": "DaemonSet",
                "name": "meshnet",
                "uid": "4448143b-3e4f-4e5b-8825-0a46c8a8890e"
            }
        ],
        "resourceVersion": "642",
        "uid": "9980cafe-0b1a-4eff-b3ae-4c905a0535d4"
    },
    "spec": {
        "affinity": {
            "nodeAffinity": {
                "requiredDuringSchedulingIgnoredDuringExecution": {
                    "nodeSelectorTerms": [
                        {
                            "matchFields": [
                                {
                                    "key": "metadata.name",
                                    "operator": "In",
                                    "values": [
                                        "kne-control-plane"
                                    ]
                                }
                            ]
                        }
                    ]
                }
            }
        },
        "containers": [
            {
                "env": [
                    {
                        "name": "HOST_IP",
                        "valueFrom": {
                            "fieldRef": {
                                "apiVersion": "v1",
                                "fieldPath": "status.hostIP"
                            }
                        }
                    },
                    {
                        "name": "INTER_NODE_LINK_TYPE",
                        "value": "GRPC"
                    }
                ],
                "image": "us-west1-docker.pkg.dev/kne-external/kne/networkop/meshnet:v0.3.1",
                "imagePullPolicy": "IfNotPresent",
                "name": "meshnet",
                "resources": {
                    "limits": {
                        "memory": "1000Mi"
                    },
                    "requests": {
                        "cpu": "100m",
                        "memory": "1000Mi"
                    }
                },
                "securityContext": {
                    "privileged": true
                },
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/etc/cni/net.d",
                        "name": "cni-cfg"
                    },
                    {
                        "mountPath": "/opt/cni/bin",
                        "name": "cni-bin"
                    },
                    {
                        "mountPath": "/var/run/netns",
                        "mountPropagation": "Bidirectional",
                        "name": "var-run-netns"
                    },
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-rctrf",
                        "readOnly": true
                    }
                ]
            }
        ],
        "dnsPolicy": "ClusterFirst",
        "enableServiceLinks": true,
        "hostIPC": true,
        "hostNetwork": true,
        "hostPID": true,
        "nodeName": "kne-control-plane",
        "nodeSelector": {
            "kubernetes.io/arch": "amd64"
        },
        "preemptionPolicy": "PreemptLowerPriority",
        "priority": 0,
        "restartPolicy": "Always",
        "schedulerName": "default-scheduler",
        "securityContext": {},
        "serviceAccount": "meshnet",
        "serviceAccountName": "meshnet",
        "terminationGracePeriodSeconds": 30,
        "tolerations": [
            {
                "effect": "NoSchedule",
                "operator": "Exists"
            },
            {
                "effect": "NoExecute",
                "key": "node.kubernetes.io/not-ready",
                "operator": "Exists"
            },
            {
                "effect": "NoExecute",
                "key": "node.kubernetes.io/unreachable",
                "operator": "Exists"
            },
            {
                "effect": "NoSchedule",
                "key": "node.kubernetes.io/disk-pressure",
                "operator": "Exists"
            },
            {
                "effect": "NoSchedule",
                "key": "node.kubernetes.io/memory-pressure",
                "operator": "Exists"
            },
            {
                "effect": "NoSchedule",
                "key": "node.kubernetes.io/pid-pressure",
                "operator": "Exists"
            },
            {
                "effect": "NoSchedule",
                "key": "node.kubernetes.io/unschedulable",
                "operator": "Exists"
            },
            {
                "effect": "NoSchedule",
                "key": "node.kubernetes.io/network-unavailable",
                "operator": "Exists"
            }
        ],
        "volumes": [
            {
                "hostPath": {
                    "path": "/opt/cni/bin",
                    "type": ""
                },
                "name": "cni-bin"
            },
            {
                "hostPath": {
                    "path": "/etc/cni/net.d",
                    "type": ""
                },
                "name": "cni-cfg"
            },
            {
                "hostPath": {
                    "path": "/var/run/netns",
                    "type": ""
                },
                "name": "var-run-netns"
            },
            {
                "name": "kube-api-access-rctrf",
                "projected": {
                    "defaultMode": 420,
                    "sources": [
                        {
                            "serviceAccountToken": {
                                "expirationSeconds": 3607,
                                "path": "token"
                            }
                        },
                        {
                            "configMap": {
                                "items": [
                                    {
                                        "key": "ca.crt",
                                        "path": "ca.crt"
                                    }
                                ],
                                "name": "kube-root-ca.crt"
                            }
                        },
                        {
                            "downwardAPI": {
                                "items": [
                                    {
                                        "fieldRef": {
                                            "apiVersion": "v1",
                                            "fieldPath": "metadata.namespace"
                                        },
                                        "path": "namespace"
                                    }
                                ]
                            }
                        }
                    ]
                }
            }
        ]
    },
    "status": {
        "conditions": [
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:41:02Z",
                "status": "True",
                "type": "Initialized"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:41:06Z",
                "status": "True",
                "type": "Ready"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:41:06Z",
                "status": "True",
                "type": "ContainersReady"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:41:02Z",
                "status": "True",
                "type": "PodScheduled"
            }
        ],
        "containerStatuses": [
            {
                "containerID": "containerd://edac8386fab91bf66ef9b6068295db05071bde53dd014c719445001a10b8d5e2",
                "image": "us-west1-docker.pkg.dev/kne-external/kne/networkop/meshnet:v0.3.1",
                "imageID": "us-west1-docker.pkg.dev/kne-external/kne/networkop/meshnet@sha256:90973cb7e8e5f9fa52b2bc14c3d8eb75378810b255c014fc562dc258a74a8cd7",
                "lastState": {},
                "name": "meshnet",
                "ready": true,
                "restartCount": 0,
                "started": true,
                "state": {
                    "running": {
                        "startedAt": "2023-03-24T16:41:05Z"
                    }
                }
            }
        ],
        "hostIP": "192.168.8.2",
        "phase": "Running",
        "podIP": "192.168.8.2",
        "podIPs": [
            {
                "ip": "192.168.8.2"
            }
        ],
        "qosClass": "Burstable",
        "startTime": "2023-03-24T16:41:02Z"
    }
}
`
	meshnet1status = PodStatus{
		Name:      "meshnet-gdsj6",
		UID:       "9980cafe-0b1a-4eff-b3ae-4c905a0535d4",
		Namespace: "meshnet",
		Phase:     "Running",
		Containers: []ContainerStatus{
			{
				Name:  "meshnet",
				Image: "us-west1-docker.pkg.dev/kne-external/kne/networkop/meshnet:v0.3.1",
				Ready: true,
			},
		},
	}

	ixia3data = `{
    "apiVersion": "v1",
    "kind": "Pod",
    "metadata": {
        "creationTimestamp": "2023-03-24T16:41:02Z",
        "generateName": "ixiatg-op-controller-manager-7b5db775d9-",
        "labels": {
            "control-plane": "controller-manager",
            "pod-template-hash": "7b5db775d9"
        },
        "name": "ixiatg-op-controller-manager-7b5db775d9-kz2mb",
        "namespace": "ixiatg-op-system",
        "ownerReferences": [
            {
                "apiVersion": "apps/v1",
                "blockOwnerDeletion": true,
                "controller": true,
                "kind": "ReplicaSet",
                "name": "ixiatg-op-controller-manager-7b5db775d9",
                "uid": "7960b609-1877-4fb8-8010-5a9fbd4695c7"
            }
        ],
        "resourceVersion": "657",
        "uid": "53fd2ce7-bcf3-489a-9f4a-aa457e01980f"
    },
    "spec": {
        "containers": [
            {
                "args": [
                    "--secure-listen-address=0.0.0.0:8443",
                    "--upstream=http://127.0.0.1:8080/",
                    "--logtostderr=true",
                    "--v=10"
                ],
                "image": "gcr.io/kubebuilder/kube-rbac-proxy:v0.8.0",
                "imagePullPolicy": "IfNotPresent",
                "name": "kube-rbac-proxy",
                "ports": [
                    {
                        "containerPort": 8443,
                        "name": "https",
                        "protocol": "TCP"
                    }
                ],
                "resources": {},
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-c9mg6",
                        "readOnly": true
                    }
                ]
            },
            {
                "args": [
                    "--health-probe-bind-address=:8081",
                    "--metrics-bind-address=127.0.0.1:8080",
                    "--leader-elect"
                ],
                "command": [
                    "/manager"
                ],
                "image": "ghcr.io/open-traffic-generator/ixia-c-operator:0.3.1",
                "imagePullPolicy": "IfNotPresent",
                "livenessProbe": {
                    "failureThreshold": 3,
                    "httpGet": {
                        "path": "/healthz",
                        "port": 8081,
                        "scheme": "HTTP"
                    },
                    "initialDelaySeconds": 15,
                    "periodSeconds": 20,
                    "successThreshold": 1,
                    "timeoutSeconds": 1
                },
                "name": "manager",
                "readinessProbe": {
                    "failureThreshold": 3,
                    "httpGet": {
                        "path": "/readyz",
                        "port": 8081,
                        "scheme": "HTTP"
                    },
                    "initialDelaySeconds": 5,
                    "periodSeconds": 10,
                    "successThreshold": 1,
                    "timeoutSeconds": 1
                },
                "resources": {
                    "limits": {
                        "cpu": "100m",
                        "memory": "200Mi"
                    },
                    "requests": {
                        "cpu": "100m",
                        "memory": "20Mi"
                    }
                },
                "securityContext": {
                    "allowPrivilegeEscalation": false
                },
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-c9mg6",
                        "readOnly": true
                    }
                ]
            }
        ],
        "dnsPolicy": "ClusterFirst",
        "enableServiceLinks": true,
        "imagePullSecrets": [
            {
                "name": "ixia-pull-secret"
            }
        ],
        "nodeName": "kne-control-plane",
        "preemptionPolicy": "PreemptLowerPriority",
        "priority": 0,
        "restartPolicy": "Always",
        "schedulerName": "default-scheduler",
        "securityContext": {
            "runAsNonRoot": true
        },
        "serviceAccount": "ixiatg-op-controller-manager",
        "serviceAccountName": "ixiatg-op-controller-manager",
        "terminationGracePeriodSeconds": 10,
        "tolerations": [
            {
                "effect": "NoExecute",
                "key": "node.kubernetes.io/not-ready",
                "operator": "Exists",
                "tolerationSeconds": 300
            },
            {
                "effect": "NoExecute",
                "key": "node.kubernetes.io/unreachable",
                "operator": "Exists",
                "tolerationSeconds": 300
            }
        ],
        "volumes": [
            {
                "name": "kube-api-access-c9mg6",
                "projected": {
                    "defaultMode": 420,
                    "sources": [
                        {
                            "serviceAccountToken": {
                                "expirationSeconds": 3607,
                                "path": "token"
                            }
                        },
                        {
                            "configMap": {
                                "items": [
                                    {
                                        "key": "ca.crt",
                                        "path": "ca.crt"
                                    }
                                ],
                                "name": "kube-root-ca.crt"
                            }
                        },
                        {
                            "downwardAPI": {
                                "items": [
                                    {
                                        "fieldRef": {
                                            "apiVersion": "v1",
                                            "fieldPath": "metadata.namespace"
                                        },
                                        "path": "namespace"
                                    }
                                ]
                            }
                        }
                    ]
                }
            }
        ]
    },
    "status": {
        "conditions": [
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:41:02Z",
                "status": "True",
                "type": "Initialized"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:41:02Z",
                "message": "containers with unready status: [manager]",
                "reason": "ContainersNotReady",
                "status": "False",
                "type": "Ready"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:41:02Z",
                "message": "containers with unready status: [manager]",
                "reason": "ContainersNotReady",
                "status": "False",
                "type": "ContainersReady"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-24T16:41:02Z",
                "status": "True",
                "type": "PodScheduled"
            }
        ],
        "containerStatuses": [
            {
                "containerID": "containerd://4ff3350898caa2a707fbacc78375eb1f032f70961cfa999b3053a048a8eda4bc",
                "image": "gcr.io/kubebuilder/kube-rbac-proxy:v0.8.0",
                "imageID": "gcr.io/kubebuilder/kube-rbac-proxy@sha256:db06cc4c084dd0253134f156dddaaf53ef1c3fb3cc809e5d81711baa4029ea4c",
                "lastState": {},
                "name": "kube-rbac-proxy",
                "ready": true,
                "restartCount": 0,
                "started": true,
                "state": {
                    "running": {
                        "startedAt": "2023-03-24T16:41:07Z"
                    }
                }
            },
            {
                "containerID": "containerd://2f69e4e109842b198534badca065147a6c64de6af15c652c98e909e4e7395d0c",
                "image": "ghcr.io/open-traffic-generator/ixia-c-operator:0.3.1",
                "imageID": "ghcr.io/open-traffic-generator/ixia-c-operator@sha256:157c99a77f89db86ba5074656c9b43b8edce828e863704b631e624cbfac7e813",
                "lastState": {},
                "name": "manager",
                "ready": false,
                "restartCount": 0,
                "started": true,
                "state": {
                    "running": {
                        "startedAt": "2023-03-24T16:41:12Z"
                    }
                }
            }
        ],
        "hostIP": "192.168.8.2",
        "phase": "Running",
        "podIP": "10.244.0.6",
        "podIPs": [
            {
                "ip": "10.244.0.6"
            }
        ],
        "qosClass": "Burstable",
        "startTime": "2023-03-24T16:41:02Z"
    }
}
`
	ixia3status = PodStatus{Name: "ixiatg-op-controller-manager-7b5db775d9-kz2mb",
		UID:       "53fd2ce7-bcf3-489a-9f4a-aa457e01980f",
		Namespace: "ixiatg-op-system",
		Phase:     "Running",
		Containers: []ContainerStatus{{Name: "kube-rbac-proxy",
			Image: "gcr.io/kubebuilder/kube-rbac-proxy:v0.8.0",
			Ready: true,
		},
			{Name: "manager",
				Image: "ghcr.io/open-traffic-generator/ixia-c-operator:0.3.1",
			},
		},
	}
)
