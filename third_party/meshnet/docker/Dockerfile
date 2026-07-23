# GRPC generation
FROM golang:1.26.5 AS proto_base

WORKDIR /src

RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11 && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1 && \
    go install github.com/bufbuild/buf/cmd/buf@v1.13.1

COPY daemon/proto .
COPY buf.gen.yaml .
COPY buf.yaml .

RUN buf lint && buf generate -v

#---------------------------------------------- Stage 2: Build Base : Build meshnet binaries (Plugin & daemon) - ---------------------------------
# Building the binaries
FROM golang:1.26.5 AS build_base


WORKDIR /go/src/github.com/openconfig/kne/third_party/meshnet
ENV CGO_ENABLED=0
COPY go.mod .
COPY go.sum .
# hadolint ignore=DL3008
RUN go mod download && \
    apt-get update -y && \
    apt-get install -y --no-install-recommends \
        libpcap-dev \
        libsystemd-dev && \
    rm -rf /var/lib/apt/lists/*

FROM --platform=${BUILDPLATFORM:-linux/amd64} build_base AS build

ARG LDFLAGS
ARG TARGETOS
ARG TARGETARCH

COPY daemon/ daemon/
COPY api/ api/
COPY plugin/ plugin/
COPY utils/ utils/
COPY --from=proto_base /src/ .


RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o meshnet plugin/meshnet.go plugin/grpcwires-plugin.go && \
    GOOS=${TARGETOS} CGO_ENABLED=1 GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o meshnetd daemon/main.go


#----------------------------------------------------- Final Container ---------------------------------
# checkov:skip=CKV_DOCKER_2: CNI daemon image does not require healthcheck
# checkov:skip=CKV_DOCKER_3: CNI daemon must run as root to configure host network namespaces
FROM debian:13-slim
# hadolint ignore=DL3008
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        jq \
        libpcap-dev && \
    rm -rf /var/lib/apt/lists/*

COPY --from=build /go/src/github.com/openconfig/kne/third_party/meshnet/meshnet /
COPY --from=build /go/src/github.com/openconfig/kne/third_party/meshnet/meshnetd /
COPY docker/new-entrypoint.sh /entrypoint.sh
COPY LICENSE /
RUN chmod +x ./entrypoint.sh /meshnetd

ENTRYPOINT ["./entrypoint.sh"]
