package load_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	cmddeploy "github.com/openconfig/kne/cmd/deploy"
	"github.com/openconfig/kne/deploy"
	"github.com/openconfig/kne/load"
)

func TestNewConfig(t *testing.T) {
	var want = &deploy.Deployment{
		Cluster: &deploy.KindSpec{
			Name:                "kne",
			Recycle:             true,
			Version:             "v0.17.0",
			Image:               "kindest/node:v1.26.0",
			KindConfigFile:      ".*/testdata/kind/kind-no-cni.yaml",
			AdditionalManifests: []string{".*/testdata/manifests/kind/kind-bridge.yaml"},
		},
		Ingress: &deploy.MetalLBSpec{
			IPCount:  100,
			Manifest: ".*/testdata/manifests/metallb/manifest.yaml",
		},
		CNI: &deploy.MeshnetSpec{
			Manifest: ".*/testdata/manifests/meshnet/grpc/manifest.yaml",
		},
		Controllers: []deploy.Controller{
			&deploy.IxiaTGSpec{
				Operator:  ".*/testdata/manifests/keysight/ixiatg-operator.yaml",
				ConfigMap: ".*/testdata/manifests/keysight/ixiatg-configmap.yaml",
			},
			&deploy.SRLinuxSpec{
				Operator: ".*/testdata/manifests/controllers/srlinux/manifest.yaml",
			},
			&deploy.CEOSLabSpec{
				Operator: ".*/testdata/manifests/controllers/ceoslab/manifest.yaml",
			},
			&deploy.LemmingSpec{
				Operator: ".*/testdata/manifests/controllers/lemming/manifest.yaml",
			},
		},
	}

	c, err := load.NewConfig("testdata/deploy/kne/kind-bridge.yaml", &cmddeploy.DeploymentConfig{})
	if err != nil {
		t.Fatalf("%v", err)
	}
	if err := c.Decode(&deploy.Deployment{}); err != nil {
		t.Fatalf("%v", err)
	}

	structs := map[string]interface{}{}

	fixStrings(reflect.ValueOf(c.Deployment), reflect.ValueOf(want), structs)

	var ignore []interface{}
	for _, i := range structs {
		ignore = append(ignore, i)
	}

	if s := cmp.Diff(c.Deployment, want, cmpopts.IgnoreUnexported(ignore...)); s != "" {
		t.Errorf("%s", s)
	}
}

// fixStrings does two things for us.  First, it fixes up absolute pathnames so
// we can compare them.  Second, it records every type of struct we see so we
// can pass them to cmpopts.IngoreUnexported.  There appears to be no way to
// call cmp.Diff and have it ignore unexported fields in all the sub structures.
func fixStrings(x, y reflect.Value, structs map[string]interface{}) {
	if !x.IsValid() || !y.IsValid() {
		return
	}

	switch x.Kind() {
	case reflect.String:
		// Our test is going to fill in full pathnames for the various
		// strings.  So if got is "/a/b/c/d" and want is ".*/c/d" then
		// reset the value of got to equal want.
		got := x.String()

		want := y.String()
		if strings.HasPrefix(want, ".*/") {
			if strings.HasSuffix(got, want[2:]) {
				x.Set(y)
			}
		}
	case reflect.Array:
		for i := 0; i < x.Len(); i++ {
			fixStrings(x.Index(i), y.Index(i), structs)
		}
	case reflect.Slice:
		if x.IsNil() != y.IsNil() || x.Len() != y.Len() {
			return
		}
		for i := 0; i < x.Len(); i++ {
			fixStrings(x.Index(i), y.Index(i), structs)
		}
	case reflect.Interface:
		if x.IsNil() || y.IsNil() {
			return
		}
		fixStrings(x.Elem(), y.Elem(), structs)
	case reflect.Pointer:
		fixStrings(x.Elem(), y.Elem(), structs)
	case reflect.Struct:
		structs[x.Type().String()] = reflect.New(x.Type()).Elem().Interface()
		for i, n := 0, x.NumField(); i < n; i++ {
			fixStrings(x.Field(i), y.Field(i), structs)
		}
	case reflect.Map:
		if x.Len() != y.Len() {
			return
		}
		for _, k := range x.MapKeys() {
			val1 := x.MapIndex(k)
			val2 := y.MapIndex(k)
			fixStrings(val1, val2, structs)
		}
	default:
		// These are all scalars
	}
}
