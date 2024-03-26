go_library(
    name = "mutations",
    srcs = [
        "dsdn.go",
        "mutate.go",
    ],
    visibility = ["//net/sdn/decentralized:__subpackages__"],
    deps = [
        "//third_party/golang/github_com/wI2L/jsondiff/v/v0:jsondiff",
        "//third_party/golang/k8s_io/api/v/v0_23/core/v1",
        "//third_party/golang/k8s_io/apimachinery/v/v0_23/pkg/api/resource",
        "//third_party/golang/klog_v2:klog",
        "//third_party/golang/protobuf/v2/proto",
    ],
)

go_test(
    name = "mutations_test",
    srcs = [
        "dsdn_test.go",
        "mutate_test.go",
    ],
    data = [":testdata"],
    library = ":mutations",
    deps = [
        "//testing/gobase:runfilestest",
        "//third_party/golang/cmp",
        "//third_party/golang/k8s_io/api/v/v0_23/core/v1",
        "//third_party/golang/k8s_io/apimachinery/v/v0_23/pkg/apis/meta/v1",
    ],
)
