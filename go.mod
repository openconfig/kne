module github.com/openconfig/kne

go 1.20

require (
	cloud.google.com/go/pubsub v1.30.0
	github.com/aristanetworks/arista-ceoslab-operator/v2 v2.0.2
	github.com/docker/docker v20.10.24+incompatible
	github.com/ghodss/yaml v1.0.0
	github.com/golang/glog v1.1.0
	github.com/golang/mock v1.6.0
	github.com/google/go-cmp v0.5.9
	github.com/h-fam/errdiff v1.0.2
	github.com/kr/pretty v0.3.1
	github.com/networkop/meshnet-cni v0.3.1-0.20230525201116-d7c306c635cf
	github.com/open-traffic-generator/ixia-c-operator v0.3.6
	github.com/open-traffic-generator/snappi/gosnappi v0.11.14
	github.com/openconfig/gnmi v0.10.0
	github.com/openconfig/gribi v1.0.0
	github.com/openconfig/lemming/operator v0.2.0
	github.com/openconfig/ondatra v0.2.8
	github.com/p4lang/p4runtime v1.3.0
	github.com/pborman/uuid v1.2.1
	github.com/pkg/errors v0.9.1
	github.com/scrapli/scrapligo v1.1.11
	github.com/scrapli/scrapligocfg v1.0.0
	github.com/spf13/cobra v1.7.0
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.15.0
	github.com/srl-labs/srl-controller v0.6.0
	github.com/srl-labs/srlinux-scrapli v0.6.0
	go.universe.tf/metallb v0.13.5
	golang.org/x/oauth2 v0.7.0
	google.golang.org/api v0.114.0
	google.golang.org/grpc v1.58.0-dev
	google.golang.org/protobuf v1.31.0
	gopkg.in/yaml.v3 v3.0.1
	k8s.io/api v0.26.3
	k8s.io/apimachinery v0.26.3
	k8s.io/client-go v0.26.3
	k8s.io/klog/v2 v2.90.1
	k8s.io/utils v0.0.0-20230313181309-38a27ef9d749
	sigs.k8s.io/controller-runtime v0.14.5
	sigs.k8s.io/yaml v1.3.0
)

require (
	cloud.google.com/go v0.110.0 // indirect
	cloud.google.com/go/compute v1.19.1 // indirect
	cloud.google.com/go/compute/metadata v0.2.3 // indirect
	cloud.google.com/go/iam v0.13.0 // indirect
	github.com/Masterminds/semver/v3 v3.2.1 // indirect
	github.com/Microsoft/go-winio v0.5.2 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/carlmontanari/difflibgo v0.0.0-20210718194309-31b9e131c298 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/creack/pty v1.1.18 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/docker/distribution v2.8.2+incompatible // indirect
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/docker/go-units v0.4.0 // indirect
	github.com/emicklei/go-restful/v3 v3.10.2 // indirect
	github.com/evanphx/json-patch v5.6.0+incompatible // indirect
	github.com/evanphx/json-patch/v5 v5.6.0 // indirect
	github.com/fsnotify/fsnotify v1.6.0 // indirect
	github.com/go-kit/log v0.2.1 // indirect
	github.com/go-logfmt/logfmt v0.5.1 // indirect
	github.com/go-logr/logr v1.2.3 // indirect
	github.com/go-openapi/jsonpointer v0.19.6 // indirect
	github.com/go-openapi/jsonreference v0.20.2 // indirect
	github.com/go-openapi/swag v0.22.3 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/google/gnostic v0.6.9 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.2.3 // indirect
	github.com/googleapis/gax-go/v2 v2.7.1 // indirect
	github.com/hashicorp/hcl v1.0.0 // indirect
	github.com/imdario/mergo v0.3.14 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/jstemmer/go-junit-report/v2 v2.0.1-0.20220823220451-7b10b4285462 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/magiconair/properties v1.8.7 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.4 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/moby/spdystream v0.2.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/morikuni/aec v1.0.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/openconfig/gnoi v0.0.0-20230221223856-1727ed932554 // indirect
	github.com/openconfig/gnsi v1.2.3 // indirect
	github.com/openconfig/gocloser v0.0.0-20220310182203-c6c950ed3b0b // indirect
	github.com/openconfig/goyang v1.4.0 // indirect
	github.com/openconfig/ygnmi v0.8.7 // indirect
	github.com/openconfig/ygot v0.29.9 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.0.2 // indirect
	github.com/patrickmn/go-cache v2.1.0+incompatible // indirect
	github.com/pelletier/go-toml/v2 v2.0.8 // indirect
	github.com/prometheus/client_golang v1.14.0 // indirect
	github.com/prometheus/client_model v0.3.0 // indirect
	github.com/prometheus/common v0.42.0 // indirect
	github.com/prometheus/procfs v0.9.0 // indirect
	github.com/rogpeppe/go-internal v1.9.0 // indirect
	github.com/sirikothe/gotextfsm v1.0.1-0.20200816110946-6aa2cfd355e4 // indirect
	github.com/sirupsen/logrus v1.9.0 // indirect
	github.com/spf13/afero v1.9.5 // indirect
	github.com/spf13/cast v1.5.1 // indirect
	github.com/spf13/jwalterweatherman v1.1.0 // indirect
	github.com/subosito/gotenv v1.4.2 // indirect
	go.opencensus.io v0.24.0 // indirect
	golang.org/x/crypto v0.6.0 // indirect
	golang.org/x/exp v0.0.0-20230725093048-515e97ebf090 // indirect
	golang.org/x/net v0.10.0 // indirect
	golang.org/x/sync v0.1.0 // indirect
	golang.org/x/sys v0.8.0 // indirect
	golang.org/x/term v0.8.0 // indirect
	golang.org/x/text v0.9.0 // indirect
	golang.org/x/time v0.3.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.2.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto v0.0.0-20230526161137-0005af68ea54 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20230525234035-dd9d682886f9 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20230525234030-28d5490b6b19 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/ini.v1 v1.67.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	k8s.io/apiextensions-apiserver v0.26.3 // indirect
	k8s.io/component-base v0.26.3 // indirect
	k8s.io/kube-openapi v0.0.0-20230308215209-15aac26d736a // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.2.3 // indirect
)
