// Copyright 2021 Google LLC
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

// Package v1beta1 defines the API types for meshnet CRDs.
package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GroupName and other constants define the API group, version, and resource names for meshnet.
const (
	// GroupName is the group name used in this package.
	GroupName = "networkop.co.uk"
	// GroupVersion is the group version used in this package.
	GroupVersion = "v1beta1"
	// GWireResNamePlural is the plural resource name for GWire objects.
	GWireResNamePlural = "gwirekobjs"
)

var (
	// SchemeGroupVersion is the group version used to register these objects.
	SchemeGroupVersion = schema.GroupVersion{Group: GroupName, Version: GroupVersion}
	// Scheme is the runtime scheme for meshnet API types.
	Scheme = runtime.NewScheme()
)

func init() {
	Scheme.AddKnownTypes(SchemeGroupVersion,
		&Topology{},
		&TopologyList{},
	)
	metav1.AddToGroupVersion(Scheme, SchemeGroupVersion)
	metav1.AddMetaToScheme(Scheme)
}
