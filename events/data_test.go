package events

import (
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
)

// Variables are named
//
//   name#data    Raw json from kubectl
//   name#event     Data converted into a corev1.Event
//   name#status  PodStatus version
//   name#string  PodStatus as a string

func json2event(j string) *corev1.Event {
	var event corev1.Event
	if err := json.Unmarshal(([]byte)(j), &event); err != nil {
		panic(err)
	}
	return &event
}

var (
	normal1event  = json2event(normal1eventdata)
	warning1event = json2event(warning1eventdata)
	warning2event = json2event(warning2eventdata)
)

// We only use 3 events when checking EventStatus.String as these cover all the cases.
var (
	normal1eventstring  = `{Name: "service-r2.1772fc5571f425ff", UID: "a756a650-bf61-4da6-8d69-d5ae40bd943c", Namespace: "ceos2"}`
	warning1eventstring = `{Name: "r1.177351c307a38f38", UID: "a0450cc2-26e5-46e6-8ef8-be9dec02cd35", Namespace: "ceos1"}`
)

var (
	normal1eventdata = `
	{
		"metadata": {
			"name": "service-r2.1772fc5571f425ff",
			"namespace": "ceos2",
			"uid": "a756a650-bf61-4da6-8d69-d5ae40bd943c",
			"resourceVersion": "3243",
			"creationTimestamp": "2023-07-18T14:24:14Z",
			"managedFields": [
				{
					"manager": "speaker",
					"operation": "Update",
					"apiVersion": "v1",
					"time": "2023-07-18T14:24:14Z",
					"fieldsType": "FieldsV1",
					"fieldsV1": {
						"f:count": {},
						"f:firstTimestamp": {},
						"f:involvedObject": {},
						"f:lastTimestamp": {},
						"f:message": {},
						"f:reason": {},
						"f:source": {
							"f:component": {}
						},
						"f:type": {}
					}
				}
			]
		},
		"involvedObject": {
			"kind": "Service",
			"namespace": "ceos2",
			"name": "service-r2",
			"uid": "2459a3c5-c353-4a50-aa70-dc897ffb051e",
			"apiVersion": "v1",
			"resourceVersion": "3076"
		},
		"reason": "nodeAssigned",
		"message": "announcing from node \"kne-control-plane\" with protocol \"layer2\"",
		"source": {
			"component": "metallb-speaker"
		},
		"firstTimestamp": "2023-07-18T14:24:14Z",
		"lastTimestamp": "2023-07-18T14:24:14Z",
		"count": 1,
		"type": "Normal",
		"eventTime": null,
		"reportingComponent": "",
		"reportingInstance": ""
	}
	`

	normal1eventstatus = EventStatus{
		"Name": "service-r2.1772fc5571f425ff",
		"UID": "a756a650-bf61-4da6-8d69-d5ae40bd943c",
		"Namespace": "ceos2",
		"Message": "announcing from node \"kne-control-plane\" with protocol \"layer2\"",
		"Type": "Normal",
		"Event": {
			"metadata": {
				"name": "service-r2.1772fc5571f425ff",
				"namespace": "ceos2",
				"uid": "a756a650-bf61-4da6-8d69-d5ae40bd943c",
				"resourceVersion": "3243",
				"creationTimestamp": "2023-07-18T14:24:14Z",
				"managedFields": [
					{
						"manager": "speaker",
						"operation": "Update",
						"apiVersion": "v1",
						"time": "2023-07-18T14:24:14Z",
						"fieldsType": "FieldsV1",
						"fieldsV1": {
							"f:count": {},
							"f:firstTimestamp": {},
							"f:involvedObject": {},
							"f:lastTimestamp": {},
							"f:message": {},
							"f:reason": {},
							"f:source": {
								"f:component": {}
							},
							"f:type": {}
						}
					}
				]
			},
			"involvedObject": {
				"kind": "Service",
				"namespace": "ceos2",
				"name": "service-r2",
				"uid": "2459a3c5-c353-4a50-aa70-dc897ffb051e",
				"apiVersion": "v1",
				"resourceVersion": "3076"
			},
			"reason": "nodeAssigned",
			"message": "announcing from node \"kne-control-plane\" with protocol \"layer2\"",
			"source": {
				"component": "metallb-speaker"
			},
			"firstTimestamp": "2023-07-18T14:24:14Z",
			"lastTimestamp": "2023-07-18T14:24:14Z",
			"count": 1,
			"type": "Normal",
			"eventTime": null,
			"reportingComponent": "",
			"reportingInstance": ""
		}
	}
	
	warning1eventstatus = EventStatus{
		"Name": "r1.177351c307a38f38",
		"UID": "a0450cc2-26e5-46e6-8ef8-be9dec02cd35",
		"Namespace": "ceos1",
		"Message": "0/1 nodes are available: 1 Insufficient cpu. preemption: 0/1 nodes are available: 1 No preemption victims found for incoming pod..",
		"Type": "Warning",
		"Event": {
			"metadata": {
				"name": "r1.177351c307a38f38",
				"namespace": "ceos1",
				"uid": "a0450cc2-26e5-46e6-8ef8-be9dec02cd35",
				"resourceVersion": "325871",
				"creationTimestamp": "2023-07-19T16:29:43Z",
				"managedFields": [
					{
						"manager": "kube-scheduler",
						"operation": "Update",
						"apiVersion": "v1",
						"time": "2023-07-19T16:39:50Z",
						"fieldsType": "FieldsV1",
						"fieldsV1": {
							"f:count": {},
							"f:firstTimestamp": {},
							"f:involvedObject": {},
							"f:lastTimestamp": {},
							"f:message": {},
							"f:reason": {},
							"f:source": {
								"f:component": {}
							},
							"f:type": {}
						}
					}
				]
			},
			"involvedObject": {
				"kind": "Pod",
				"namespace": "ceos1",
				"name": "r1",
				"uid": "b69a3fd5-2f25-42aa-91c4-146c40e53053",
				"apiVersion": "v1",
				"resourceVersion": "323358"
			},
			"reason": "FailedScheduling",
			"message": "0/1 nodes are available: 1 Insufficient cpu. preemption: 0/1 nodes are available: 1 No preemption victims found for incoming pod..",
			"source": {
				"component": "default-scheduler"
			},
			"firstTimestamp": "2023-07-19T16:29:43Z",
			"lastTimestamp": "2023-07-19T16:39:50Z",
			"count": 3,
			"type": "Warning",
			"eventTime": null,
			"reportingComponent": "",
			"reportingInstance": ""
		}
	}
)