// Copyright 2023 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This is required to generate CRD using controller-gen
// +groupName=networkop.co.uk

package v1beta1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

//go:generate controller-gen object paths=$GOFILE

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +optional
type GWireKNodeSpec struct {
	metav1.TypeMeta `json:",inline"`
	// unique link id
	// +optional
	UIDs []int `json:"uids"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +optional
type GWireKNodeStatus struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	GWireKItems []GWireStatus `json:"grpcWireItems"`
}

type GWireStatus struct {
	// +optional
	// Name of the node holding the wire end
	LocalNodeName string `json:"node_name"`
	// +optional
	// Unique link id as assigned by meshnet
	LinkId int64 `json:"link_id"`
	// +optional
	// The topology namespace.
	TopoNamespace string `json:"topo_namespace"`
	// +optional
	//Network namespace of the local pod holding the wire end
	LocalPodNetNs string `json:"local_pod_net_ns"`
	// +optional
	// Local pod name as specified in topology CR
	LocalPodName string `json:"local_pod_name"`
	// +optional
	// Local pod ip as specified in topology CR
	LocalPodIp string `json:"local_pod_ip"`
	// +optional
	// Local pod interface name that is specified in topology CR and is created by meshnet
	LocalPodIfaceName string `json:"local_pod_iface_name"`
	// +optional
	// The interface(name) in the local node and is connected with local pod
	WireIfaceNameOnLocalNode string `json:"wire_iface_name_on_local_node"`
	// +optional
	// The interface id, in the peer node adn is connected with remote pod.
	// This is used for de-multiplexing received packet from grpcwire
	WireIfaceIdOnPeerNode int64 `json:"wire_iface_id_on_peer_node"`
	// +optional
	// peer node IP address
	GWirePeerNodeIp string `json:"gwire_peer_node_ip"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type GWireKObj struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Status GWireKNodeStatus `json:"status"`
	// +optional
	Spec GWireKNodeSpec `json:"spec"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type GWireKObjList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []GWireKObj `json:"items"`
}
