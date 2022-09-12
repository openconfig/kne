// Copyright 2022 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package fake

import (
	metallbv1client "github.com/openconfig/kne/api/metallb/clientset/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	dfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/rest"
)

func NewSimpleClientset(objects ...runtime.Object) (*metallbv1client.Clientset, error) {
	cs, err := metallbv1client.NewAddressPoolForConfig(&rest.Config{})
	if err != nil {
		return nil, err
	}
	c := dfake.NewSimpleDynamicClient(metallbv1client.Scheme, objects...)
	cs.SetDynamicClient(c.Resource(metallbv1client.IPAddressPoolGVR()))
	return cs, nil
}
