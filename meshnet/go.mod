module github.com/networkop/meshnet-cni

go 1.19

require (
	github.com/containernetworking/cni v0.8.1
	github.com/containernetworking/plugins v0.9.1
	github.com/davecgh/go-spew v1.1.1
	github.com/google/go-cmp v0.5.9
	github.com/google/gopacket v1.1.19
	github.com/grpc-ecosystem/go-grpc-middleware v1.3.0
	github.com/h-fam/errdiff v1.0.2
	github.com/openconfig/gnmi v0.0.0-20220920173703-480bf53a74d2
	github.com/redhat-nfvpe/koko v0.0.0-20210415181932-a18aa44814ea
	github.com/safchain/ethtool v0.0.0-20190326074333-42ed695e3de8
	github.com/sirupsen/logrus v1.8.1
	github.com/vishvananda/netlink v1.1.1-0.20201029203352-d40f9887b852
	google.golang.org/grpc v1.47.0
	google.golang.org/protobuf v1.30.0
	k8s.io/apimachinery v0.26.1
	k8s.io/client-go v0.26.1
	sigs.k8s.io/controller-runtime v0.14.5
)

require google.golang.org/grpc/cmd/protoc-gen-go-grpc v1.3.0 // indirect

require (
	github.com/Microsoft/go-winio v0.4.11 // indirect
	github.com/docker/distribution v2.8.0+incompatible // indirect
	github.com/docker/docker v0.0.0-20181024220401-bc4c1c238b55 // indirect
	github.com/docker/go-connections v0.0.0-20180228141015-7395e3f8aa16 // indirect
	github.com/docker/go-units v0.4.0 // indirect
	github.com/emicklei/go-restful/v3 v3.9.0 // indirect
	github.com/evanphx/json-patch v4.12.0+incompatible // indirect
	github.com/go-errors/errors v1.4.2
	github.com/go-logr/logr v1.2.3 // indirect
	github.com/go-openapi/jsonpointer v0.19.5 // indirect
	github.com/go-openapi/jsonreference v0.20.0 // indirect
	github.com/go-openapi/swag v0.19.14 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/google/gnostic v0.5.7-v3refs // indirect
	github.com/google/gofuzz v1.1.0 // indirect
	github.com/imdario/mergo v0.3.6 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/mailru/easyjson v0.7.6 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/opencontainers/go-digest v0.0.0-20170607195333-279bed98673d // indirect
	github.com/opencontainers/image-spec v0.0.0-20171030174740-d60099175f88 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/vishvananda/netns v0.0.0-20200728191858-db3c7e526aae // indirect
	golang.org/x/net v0.7.0 // indirect
	golang.org/x/oauth2 v0.0.0-20220223155221-ee480838109b // indirect
	golang.org/x/sys v0.5.0 // indirect
	golang.org/x/term v0.5.0 // indirect
	golang.org/x/text v0.7.0 // indirect
	golang.org/x/time v0.3.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto v0.0.0-20220714211235-042d03aeabc9 // indirect
	gopkg.in/check.v1 v1.0.0-20200902074654-038fdea0a05b // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/api v0.26.1 // indirect
	k8s.io/cri-api v0.0.0-20191204094248-a6f63f369f6d // indirect
	k8s.io/klog v1.0.0 // indirect
	k8s.io/klog/v2 v2.80.1 // indirect
	k8s.io/kube-openapi v0.0.0-20221012153701-172d655c2280 // indirect
	k8s.io/kubernetes v1.14.6 // indirect
	k8s.io/utils v0.0.0-20221128185143-99ec85e7a448 // indirect
	sigs.k8s.io/json v0.0.0-20220713155537-f223a00ba0e2 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.2.3 // indirect
	sigs.k8s.io/yaml v1.3.0 // indirect
)

replace github.com/networkop/meshnet-cni => ./
