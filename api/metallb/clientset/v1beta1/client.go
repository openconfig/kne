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
package v1beta1

import (
	"context"

	metallbv1 "go.universe.tf/metallb/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

// IPAddressPoolInterface provides access to the IPAddressPool CRD.
type IPAddressPoolInterface interface {
	List(ctx context.Context, opts metav1.ListOptions) (*metallbv1.IPAddressPoolList, error)
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*metallbv1.IPAddressPool, error)
	Create(ctx context.Context, pool *metallbv1.IPAddressPool, opts metav1.CreateOptions) (*metallbv1.IPAddressPool, error)
	Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
	Unstructured(ctx context.Context, name string, opts metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error)
	Update(ctx context.Context, obj *unstructured.Unstructured, opts metav1.UpdateOptions) (*metallbv1.IPAddressPool, error)
}

// L2AdvertisementInterface provides access to the L2Advertisement CRD.
type L2AdvertisementInterface interface {
	List(ctx context.Context, opts metav1.ListOptions) (*metallbv1.L2AdvertisementList, error)
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*metallbv1.L2Advertisement, error)
	Create(ctx context.Context, pool *metallbv1.L2Advertisement, opts metav1.CreateOptions) (*metallbv1.L2Advertisement, error)
	Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
	Unstructured(ctx context.Context, name string, opts metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error)
	Update(ctx context.Context, obj *unstructured.Unstructured, opts metav1.UpdateOptions) (*metallbv1.L2Advertisement, error)
}

// Interface is the clientset interface for Metallb.
type Interface interface {
	IPAddressPool(namespace string) IPAddressPoolInterface
	L2Advertisement(namespace string) L2AdvertisementInterface
}

// Clientset is a client for the metallb crds.
type Clientset struct {
	dClient dynamic.Interface
}

func (c *Clientset) Metallb() Interface {
	return c
}

var (
	gvrIPAddressPool = schema.GroupVersionResource{
		Group:    groupVersion.Group,
		Version:  groupVersion.Version,
		Resource: "ipaddresspools",
	}

	gvrL2Advertisment = schema.GroupVersionResource{
		Group:    groupVersion.Group,
		Version:  groupVersion.Version,
		Resource: "l2advertisements",
	}
)

func IPAddressPoolGVR() schema.GroupVersionResource {
	return gvrIPAddressPool
}

func L2AdvertisementGVR() schema.GroupVersionResource {
	return gvrL2Advertisment
}

var (
	groupVersion = metallbv1.GroupVersion
	Scheme       = runtime.NewScheme()
)

func init() {
	metallbv1.AddToScheme(Scheme)

	metav1.AddToGroupVersion(Scheme, groupVersion)
	metav1.AddMetaToScheme(Scheme)
}

func GV() *schema.GroupVersion {
	return &groupVersion
}

// NewForConfig returns a new Clientset based on c.
func NewForConfig(c *rest.Config) (*Clientset, error) {
	config := *c
	config.ContentConfig.GroupVersion = &groupVersion
	config.APIPath = "/apis"
	config.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	config.UserAgent = rest.DefaultKubernetesUserAgent()
	dClient, err := dynamic.NewForConfig(c)
	if err != nil {
		return nil, err
	}
	return &Clientset{
		dClient: dClient,
	}, nil
}

// SetDynamicClient is only exposed for integration testing.
func (c *Clientset) SetDynamicClient(d dynamic.Interface) {
	c.dClient = d
}

func (c *Clientset) IPAddressPool(namespace string) IPAddressPoolInterface {
	return &addressPoolClient{
		dInterface: c.dClient.Resource(gvrIPAddressPool),
		ns:         namespace,
	}
}

func (c *Clientset) L2Advertisement(namespace string) L2AdvertisementInterface {
	return &l2AdvertisementClient{
		dInterface: c.dClient.Resource(gvrL2Advertisment),
		ns:         namespace,
	}
}
