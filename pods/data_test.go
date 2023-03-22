package pods

import (
	"bytes"
	"fmt"
)

// This file contains three variables used for testing.  The data was pruned
// from "kubectl get pods -A -o json --watch"
//
// rawStream contains the individual json blobs
//
// consolidated contains the output as if --watch was not provided
//
// wantStatus is the data above as a slice of PodStatus

var rawStream = []string{
	`{
    "apiVersion": "v1",
    "kind": "Pod",
    "metadata": {
        "creationTimestamp": "2023-03-22T15:38:12Z",
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
                "uid": "eaf5ed6d-3689-4e96-b9ce-d1eb5047a870"
            }
        ],
        "resourceVersion": "1102",
        "uid": "bc60a503-a347-4409-9b33-e5f29f888cd6"
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
                        "name": "kube-api-access-d449m",
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
                        "name": "kube-api-access-d449m",
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
                "name": "kube-api-access-d449m",
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
`,
	`{
    "apiVersion": "v1",
    "kind": "Pod",
    "metadata": {
        "creationTimestamp": "2023-03-22T15:38:12Z",
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
                "uid": "eaf5ed6d-3689-4e96-b9ce-d1eb5047a870"
            }
        ],
        "resourceVersion": "1111",
        "uid": "bc60a503-a347-4409-9b33-e5f29f888cd6"
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
                        "name": "kube-api-access-d449m",
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
                        "name": "kube-api-access-d449m",
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
                "name": "kube-api-access-d449m",
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
                "lastTransitionTime": "2023-03-22T15:38:12Z",
                "message": "containers with incomplete status: [init-ceos]",
                "reason": "ContainersNotInitialized",
                "status": "False",
                "type": "Initialized"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-22T15:38:12Z",
                "message": "containers with unready status: [ceos]",
                "reason": "ContainersNotReady",
                "status": "False",
                "type": "Ready"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-22T15:38:12Z",
                "message": "containers with unready status: [ceos]",
                "reason": "ContainersNotReady",
                "status": "False",
                "type": "ContainersReady"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-22T15:38:12Z",
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
            }
        ],
        "phase": "Pending",
        "qosClass": "Burstable",
        "startTime": "2023-03-22T15:38:12Z"
    }
}
`,
	`{
    "apiVersion": "v1",
    "kind": "Pod",
    "metadata": {
        "creationTimestamp": "2023-03-22T15:38:13Z",
        "labels": {
            "app": "otg-controller"
        },
        "name": "otg-controller",
        "namespace": "multivendor",
        "resourceVersion": "1269",
        "uid": "45154f21-3636-400d-9679-a2c23dbe519a"
    },
    "spec": {
        "containers": [
            {
                "args": [
                    "--accept-eula",
                    "--debug"
                ],
                "image": "ghcr.io/open-traffic-generator/licensed/ixia-c-controller:0.0.1-3807",
                "imagePullPolicy": "IfNotPresent",
                "name": "ixia-c",
                "resources": {},
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/home/ixia-c/controller/config",
                        "name": "config",
                        "readOnly": true
                    },
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-s6d9p",
                        "readOnly": true
                    }
                ]
            },
            {
                "args": [
                    "-http-server",
                    "https://localhost:8443",
                    "--debug"
                ],
                "image": "ghcr.io/open-traffic-generator/ixia-c-gnmi-server:1.10.14",
                "imagePullPolicy": "IfNotPresent",
                "name": "gnmi",
                "ports": [
                    {
                        "containerPort": 50051,
                        "name": "gnmi",
                        "protocol": "TCP"
                    }
                ],
                "resources": {},
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-s6d9p",
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
        "securityContext": {},
        "serviceAccount": "default",
        "serviceAccountName": "default",
        "terminationGracePeriodSeconds": 5,
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
                    "name": "controller-config"
                },
                "name": "config"
            },
            {
                "name": "kube-api-access-s6d9p",
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
                "lastTransitionTime": "2023-03-22T15:38:13Z",
                "status": "True",
                "type": "Initialized"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-22T15:38:13Z",
                "message": "containers with unready status: [ixia-c gnmi]",
                "reason": "ContainersNotReady",
                "status": "False",
                "type": "Ready"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-22T15:38:13Z",
                "message": "containers with unready status: [ixia-c gnmi]",
                "reason": "ContainersNotReady",
                "status": "False",
                "type": "ContainersReady"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-22T15:38:13Z",
                "status": "True",
                "type": "PodScheduled"
            }
        ],
        "containerStatuses": [
            {
                "image": "ghcr.io/open-traffic-generator/ixia-c-gnmi-server:1.10.14",
                "imageID": "",
                "lastState": {},
                "name": "gnmi",
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
                "image": "ghcr.io/open-traffic-generator/licensed/ixia-c-controller:0.0.1-3807",
                "imageID": "",
                "lastState": {},
                "name": "ixia-c",
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
        "qosClass": "BestEffort",
        "startTime": "2023-03-22T15:38:13Z"
    }
}
`,
	`{
    "apiVersion": "v1",
    "kind": "Pod",
    "metadata": {
        "creationTimestamp": "2023-03-22T15:38:13Z",
        "labels": {
            "app": "otg-port-eth1",
            "topo": "multivendor"
        },
        "name": "otg-port-eth1",
        "namespace": "multivendor",
        "resourceVersion": "1271",
        "uid": "a75d155b-fc81-4288-8664-d49c3cfa4ebc"
    },
    "spec": {
        "containers": [
            {
                "env": [
                    {
                        "name": "ARG_CORE_LIST",
                        "value": "2 3 4"
                    },
                    {
                        "name": "ARG_IFACE_LIST",
                        "value": "virtual@af_packet,eth1"
                    },
                    {
                        "name": "OPT_NO_HUGEPAGES",
                        "value": "Yes"
                    },
                    {
                        "name": "OPT_LISTEN_PORT",
                        "value": "5555"
                    }
                ],
                "image": "ghcr.io/open-traffic-generator/ixia-c-traffic-engine:1.6.0.30",
                "imagePullPolicy": "IfNotPresent",
                "name": "otg-port-eth1-traffic-engine",
                "resources": {},
                "securityContext": {
                    "privileged": true
                },
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-5hf5d",
                        "readOnly": true
                    }
                ]
            },
            {
                "env": [
                    {
                        "name": "INTF_LIST",
                        "value": "eth1"
                    }
                ],
                "image": "ghcr.io/open-traffic-generator/licensed/ixia-c-protocol-engine:1.00.0.271",
                "imagePullPolicy": "IfNotPresent",
                "livenessProbe": {
                    "failureThreshold": 3,
                    "initialDelaySeconds": 10,
                    "periodSeconds": 1,
                    "successThreshold": 1,
                    "tcpSocket": {
                        "port": 50071
                    },
                    "terminationGracePeriodSeconds": 1,
                    "timeoutSeconds": 1
                },
                "name": "otg-port-eth1-protocol-engine",
                "resources": {},
                "securityContext": {
                    "privileged": true
                },
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-5hf5d",
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
        "initContainers": [
            {
                "args": [
                    "2",
                    "10"
                ],
                "image": "networkop/init-wait:latest",
                "imagePullPolicy": "IfNotPresent",
                "name": "init-container",
                "resources": {},
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-5hf5d",
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
        "terminationGracePeriodSeconds": 5,
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
                "name": "kube-api-access-5hf5d",
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
                "lastTransitionTime": "2023-03-22T15:38:13Z",
                "message": "containers with incomplete status: [init-container]",
                "reason": "ContainersNotInitialized",
                "status": "False",
                "type": "Initialized"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-22T15:38:13Z",
                "message": "containers with unready status: [otg-port-eth1-traffic-engine otg-port-eth1-protocol-engine]",
                "reason": "ContainersNotReady",
                "status": "False",
                "type": "Ready"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-22T15:38:13Z",
                "message": "containers with unready status: [otg-port-eth1-traffic-engine otg-port-eth1-protocol-engine]",
                "reason": "ContainersNotReady",
                "status": "False",
                "type": "ContainersReady"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-22T15:38:13Z",
                "status": "True",
                "type": "PodScheduled"
            }
        ],
        "containerStatuses": [
            {
                "image": "ghcr.io/open-traffic-generator/licensed/ixia-c-protocol-engine:1.00.0.271",
                "imageID": "",
                "lastState": {},
                "name": "otg-port-eth1-protocol-engine",
                "ready": false,
                "restartCount": 0,
                "started": false,
                "state": {
                    "waiting": {
                        "reason": "PodInitializing"
                    }
                }
            },
            {
                "image": "ghcr.io/open-traffic-generator/ixia-c-traffic-engine:1.6.0.30",
                "imageID": "",
                "lastState": {},
                "name": "otg-port-eth1-traffic-engine",
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
                "name": "init-container",
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
        "qosClass": "BestEffort",
        "startTime": "2023-03-22T15:38:13Z"
    }
}
`,
	`{
    "apiVersion": "v1",
    "kind": "Pod",
    "metadata": {
        "creationTimestamp": "2023-03-22T15:38:13Z",
        "labels": {
            "app": "otg-controller"
        },
        "name": "otg-controller",
        "namespace": "multivendor",
        "resourceVersion": "1325",
        "uid": "45154f21-3636-400d-9679-a2c23dbe519a"
    },
    "spec": {
        "containers": [
            {
                "args": [
                    "--accept-eula",
                    "--debug"
                ],
                "image": "ghcr.io/open-traffic-generator/licensed/ixia-c-controller:0.0.1-3807",
                "imagePullPolicy": "IfNotPresent",
                "name": "ixia-c",
                "resources": {},
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/home/ixia-c/controller/config",
                        "name": "config",
                        "readOnly": true
                    },
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-s6d9p",
                        "readOnly": true
                    }
                ]
            },
            {
                "args": [
                    "-http-server",
                    "https://localhost:8443",
                    "--debug"
                ],
                "image": "ghcr.io/open-traffic-generator/ixia-c-gnmi-server:1.10.14",
                "imagePullPolicy": "IfNotPresent",
                "name": "gnmi",
                "ports": [
                    {
                        "containerPort": 50051,
                        "name": "gnmi",
                        "protocol": "TCP"
                    }
                ],
                "resources": {},
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-s6d9p",
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
        "securityContext": {},
        "serviceAccount": "default",
        "serviceAccountName": "default",
        "terminationGracePeriodSeconds": 5,
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
                    "name": "controller-config"
                },
                "name": "config"
            },
            {
                "name": "kube-api-access-s6d9p",
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
                "lastTransitionTime": "2023-03-22T15:38:13Z",
                "status": "True",
                "type": "Initialized"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-22T15:38:13Z",
                "message": "containers with unready status: [ixia-c]",
                "reason": "ContainersNotReady",
                "status": "False",
                "type": "Ready"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-22T15:38:13Z",
                "message": "containers with unready status: [ixia-c]",
                "reason": "ContainersNotReady",
                "status": "False",
                "type": "ContainersReady"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-22T15:38:13Z",
                "status": "True",
                "type": "PodScheduled"
            }
        ],
        "containerStatuses": [
            {
                "containerID": "containerd://35d030d1e4d432640a83d1f7336a7493a6f5278dafa67e6b923f0a31919c1a47",
                "image": "ghcr.io/open-traffic-generator/ixia-c-gnmi-server:1.10.14",
                "imageID": "ghcr.io/open-traffic-generator/ixia-c-gnmi-server@sha256:c5ee8b62c9fe0629e431b2a6a3424355018399bed75d634601d2a4dacd3b7ede",
                "lastState": {},
                "name": "gnmi",
                "ready": true,
                "restartCount": 0,
                "started": true,
                "state": {
                    "running": {
                        "startedAt": "2023-03-22T15:38:22Z"
                    }
                }
            },
            {
                "image": "ghcr.io/open-traffic-generator/licensed/ixia-c-controller:0.0.1-3807",
                "imageID": "",
                "lastState": {},
                "name": "ixia-c",
                "ready": false,
                "restartCount": 0,
                "started": false,
                "state": {
                    "waiting": {
                        "message": "rpc error: code = Unknown desc = failed to pull and unpack image \"ghcr.io/open-traffic-generator/licensed/ixia-c-controller:0.0.1-3807\": failed to resolve reference \"ghcr.io/open-traffic-generator/licensed/ixia-c-controller:0.0.1-3807\": failed to authorize: failed to fetch anonymous token: unexpected status: 401 Unauthorized",
                        "reason": "ErrImagePull"
                    }
                }
            }
        ],
        "hostIP": "192.168.8.2",
        "phase": "Pending",
        "podIP": "10.244.0.16",
        "podIPs": [
            {
                "ip": "10.244.0.16"
            }
        ],
        "qosClass": "BestEffort",
        "startTime": "2023-03-22T15:38:13Z"
    }
}
`,
	`{
    "apiVersion": "v1",
    "kind": "Pod",
    "metadata": {
        "creationTimestamp": "2023-03-22T15:38:13Z",
        "labels": {
            "app": "otg-controller"
        },
        "name": "otg-controller",
        "namespace": "multivendor",
        "resourceVersion": "1335",
        "uid": "45154f21-3636-400d-9679-a2c23dbe519a"
    },
    "spec": {
        "containers": [
            {
                "args": [
                    "--accept-eula",
                    "--debug"
                ],
                "image": "ghcr.io/open-traffic-generator/licensed/ixia-c-controller:0.0.1-3807",
                "imagePullPolicy": "IfNotPresent",
                "name": "ixia-c",
                "resources": {},
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/home/ixia-c/controller/config",
                        "name": "config",
                        "readOnly": true
                    },
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-s6d9p",
                        "readOnly": true
                    }
                ]
            },
            {
                "args": [
                    "-http-server",
                    "https://localhost:8443",
                    "--debug"
                ],
                "image": "ghcr.io/open-traffic-generator/ixia-c-gnmi-server:1.10.14",
                "imagePullPolicy": "IfNotPresent",
                "name": "gnmi",
                "ports": [
                    {
                        "containerPort": 50051,
                        "name": "gnmi",
                        "protocol": "TCP"
                    }
                ],
                "resources": {},
                "terminationMessagePath": "/dev/termination-log",
                "terminationMessagePolicy": "File",
                "volumeMounts": [
                    {
                        "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                        "name": "kube-api-access-s6d9p",
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
        "securityContext": {},
        "serviceAccount": "default",
        "serviceAccountName": "default",
        "terminationGracePeriodSeconds": 5,
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
                    "name": "controller-config"
                },
                "name": "config"
            },
            {
                "name": "kube-api-access-s6d9p",
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
                "lastTransitionTime": "2023-03-22T15:38:13Z",
                "status": "True",
                "type": "Initialized"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-22T15:38:13Z",
                "message": "containers with unready status: [ixia-c]",
                "reason": "ContainersNotReady",
                "status": "False",
                "type": "Ready"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-22T15:38:13Z",
                "message": "containers with unready status: [ixia-c]",
                "reason": "ContainersNotReady",
                "status": "False",
                "type": "ContainersReady"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-22T15:38:13Z",
                "status": "True",
                "type": "PodScheduled"
            }
        ],
        "containerStatuses": [
            {
                "containerID": "containerd://35d030d1e4d432640a83d1f7336a7493a6f5278dafa67e6b923f0a31919c1a47",
                "image": "ghcr.io/open-traffic-generator/ixia-c-gnmi-server:1.10.14",
                "imageID": "ghcr.io/open-traffic-generator/ixia-c-gnmi-server@sha256:c5ee8b62c9fe0629e431b2a6a3424355018399bed75d634601d2a4dacd3b7ede",
                "lastState": {},
                "name": "gnmi",
                "ready": true,
                "restartCount": 0,
                "started": true,
                "state": {
                    "running": {
                        "startedAt": "2023-03-22T15:38:22Z"
                    }
                }
            },
            {
                "image": "ghcr.io/open-traffic-generator/licensed/ixia-c-controller:0.0.1-3807",
                "imageID": "",
                "lastState": {},
                "name": "ixia-c",
                "ready": false,
                "restartCount": 0,
                "started": false,
                "state": {
                    "waiting": {
                        "message": "Back-off pulling image \"ghcr.io/open-traffic-generator/licensed/ixia-c-controller:0.0.1-3807\"",
                        "reason": "ImagePullBackOff"
                    }
                }
            }
        ],
        "hostIP": "192.168.8.2",
        "phase": "Pending",
        "podIP": "10.244.0.16",
        "podIPs": [
            {
                "ip": "10.244.0.16"
            }
        ],
        "qosClass": "BestEffort",
        "startTime": "2023-03-22T15:38:13Z"
    }
}
`,
	`{
    "apiVersion": "v1",
    "kind": "Pod",
    "metadata": {
        "creationTimestamp": "2023-03-22T16:59:15Z",
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
                "uid": "5ec9c206-67b4-4bf0-a7b8-d7bd6e072013"
            }
        ],
        "resourceVersion": "2175",
        "uid": "8e7ada76-f00d-41f0-a52b-34bbe1059108"
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
                "image": "us-west1-docker.pkg.dev/gep-kne/arista/ceos:ga",
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
                        "name": "kube-api-access-kkc5c",
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
                        "name": "kube-api-access-kkc5c",
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
                "name": "kube-api-access-kkc5c",
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
                "lastTransitionTime": "2023-03-22T17:00:03Z",
                "status": "True",
                "type": "Initialized"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-22T17:02:02Z",
                "status": "True",
                "type": "Ready"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-22T17:02:02Z",
                "status": "True",
                "type": "ContainersReady"
            },
            {
                "lastProbeTime": null,
                "lastTransitionTime": "2023-03-22T16:59:15Z",
                "status": "True",
                "type": "PodScheduled"
            }
        ],
        "containerStatuses": [
            {
                "containerID": "containerd://31c18ff1a0d4f8ec8be2a25b272254fda352547e4a00a3a35a6c7e56efd8f37f",
                "image": "us-west1-docker.pkg.dev/gep-kne/arista/ceos:ga",
                "imageID": "us-west1-docker.pkg.dev/gep-kne/arista/ceos@sha256:483f662ec489b151930c590fed32e1f4a08469e5db459828e433a9a94f3cd482",
                "lastState": {},
                "name": "ceos",
                "ready": true,
                "restartCount": 0,
                "started": true,
                "state": {
                    "running": {
                        "startedAt": "2023-03-22T17:01:54Z"
                    }
                }
            }
        ],
        "hostIP": "192.168.8.2",
        "initContainerStatuses": [
            {
                "containerID": "containerd://1ca46df4a01384b73cd48dbdbeb88a799ef56f1e6d5dc072c39e8c73923f35ce",
                "image": "docker.io/networkop/init-wait:latest",
                "imageID": "docker.io/library/import-2023-03-22@sha256:b042a644d6eeaaf21576db428a7aa86927df4cf09645d63ab2bcc0ea4e9b873a",
                "lastState": {},
                "name": "init-ceos",
                "ready": true,
                "restartCount": 0,
                "state": {
                    "terminated": {
                        "containerID": "containerd://1ca46df4a01384b73cd48dbdbeb88a799ef56f1e6d5dc072c39e8c73923f35ce",
                        "exitCode": 0,
                        "finishedAt": "2023-03-22T17:00:02Z",
                        "reason": "Completed",
                        "startedAt": "2023-03-22T17:00:02Z"
                    }
                }
            }
        ],
        "phase": "Running",
        "podIP": "10.244.0.21",
        "podIPs": [
            {
                "ip": "10.244.0.21"
            }
        ],
        "qosClass": "Burstable",
        "startTime": "2023-03-22T16:59:15Z"
    }
}`,
}

var consolidated []byte

func init() {
	const (
		prefix = `
{
    "apiVersion": "v1",
    "items": [
`
		suffix = `
    ],
    "kind": "List",
    "metadata": {
        "resourceVersion": ""
    }
}`
	)

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%s", prefix)
	for _, item := range rawStream[:len(rawStream)-1] {
		fmt.Fprintf(&buf, "%s,\n", item)
	}
	fmt.Fprintf(&buf, "%s", rawStream[len(rawStream)-1])
	fmt.Fprintf(&buf, "%s", suffix)
	consolidated = buf.Bytes()
}

var wantStatus = []PodStatus{
	{
		Name:      "ceos",
		Namespace: "multivendor",
		Phase:     "Pending",
	},
	{
		Name:      "ceos",
		Namespace: "multivendor",
		Containers: []ContainerStatus{
			{
				Name:   "ceos",
				Reason: "PodInitializing",
			},
		},
		Phase: "Pending",
	},
	{
		Name:      "otg-controller",
		Namespace: "multivendor",
		Containers: []ContainerStatus{
			{
				Name:   "gnmi",
				Reason: "ContainerCreating",
			},
			{
				Name:   "ixia-c",
				Reason: "ContainerCreating",
			},
		},
		Phase: "Pending",
	},
	{
		Name:      "otg-port-eth1",
		Namespace: "multivendor",
		Containers: []ContainerStatus{
			{
				Name:   "otg-port-eth1-protocol-engine",
				Reason: "PodInitializing",
			},
			{
				Name:   "otg-port-eth1-traffic-engine",
				Reason: "PodInitializing",
			},
		},
		Phase: "Pending",
	},
	{
		Name:      "otg-controller",
		Namespace: "multivendor",
		Containers: []ContainerStatus{
			{
				Name:  "gnmi",
				Ready: true,
			},
			{
				Name:    "ixia-c",
				Reason:  "ErrImagePull",
				Message: "rpc error: code = Unknown desc = failed to pull and unpack image \"ghcr.io/open-traffic-generator/licensed/ixia-c-controller:0.0.1-3807\": failed to resolve reference \"ghcr.io/open-traffic-generator/licensed/ixia-c-controller:0.0.1-3807\": failed to authorize: failed to fetch anonymous token: unexpected status: 401 Unauthorized",
			},
		},
		Phase: "Pending",
	},
	{
		Name:      "otg-controller",
		Namespace: "multivendor",
		Containers: []ContainerStatus{
			{
				Name:  "gnmi",
				Ready: true,
			},
			{
				Name:    "ixia-c",
				Reason:  "ImagePullBackOff",
				Message: "Back-off pulling image \"ghcr.io/open-traffic-generator/licensed/ixia-c-controller:0.0.1-3807\"",
			},
		},
		Phase: "Pending",
	},
	{
		Name:      "ceos",
		Namespace: "multivendor",
		Containers: []ContainerStatus{
			{
				Name:  "ceos",
				Ready: true,
			},
		},
		Phase: "Running",
	},
}
