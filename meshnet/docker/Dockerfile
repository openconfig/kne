# GRPC generation
FROM golang:1.18.2 AS proto_base

WORKDIR /src

RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
RUN go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
RUN go install github.com/bufbuild/buf/cmd/buf@v1.13.1

COPY daemon/proto .
COPY buf.gen.yaml .
COPY buf.yaml .

RUN buf lint && buf generate -v

#---------------------------------------------- Stage 2: Build Base : Build mesnet binraries (Plugin & daemon) - ---------------------------------
# Building the binaries
#FROM golang:1.17.2 AS build_base
FROM golang:1.18.2 AS build_base


WORKDIR /go/src/github.com/networkop/meshnet-cni
ENV CGO_ENABLED=0
COPY go.mod .
COPY go.sum .
RUN go mod download; \
    apt-get update -y; \
    apt-get install -y libpcap-dev; \
    apt-get install -y libsystemd-dev;
 
    # apt-get install -y libmnl; \
    # apt-get install -y libmnl-dev; \
    # go get github.com/chamaken/cgolmnl

FROM --platform=${BUILDPLATFORM:-linux/amd64} build_base AS build

ARG LDFLAGS
ARG TARGETOS
ARG TARGETARCH

COPY daemon/ daemon/
COPY api/ api/
COPY plugin/ plugin/
COPY utils/ utils/
COPY --from=proto_base /src/ .


RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o meshnet plugin/meshnet.go plugin/grpcwires-plugin.go 
RUN GOOS=${TARGETOS} CGO_ENABLED=1 GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o meshnetd daemon/main.go; 


#----------------------------------------------------- Final Conatiner ---------------------------------
# FROM alpine:latest
# RUN apk update; \
#     apk add --no-cache jq

FROM ubuntu:20.04
RUN apt-get update; \ 
    apt-get install -y jq; \
    apt-get install -y libpcap-dev

# apt-get install -y libpcap-dev; \  

COPY --from=build /go/src/github.com/networkop/meshnet-cni/meshnet /
COPY --from=build /go/src/github.com/networkop/meshnet-cni/meshnetd /
#COPY etc/cni/net.d/meshnet.conf /
COPY docker/new-entrypoint.sh /entrypoint.sh
COPY LICENSE /
RUN chmod +x ./entrypoint.sh
RUN chmod +x /meshnetd
#RUN mkdir /lib64 && ln -s /lib/libc.musl-x86_64.so.1 /lib64/ld-linux-x86-64.so.2

ENTRYPOINT ./entrypoint.sh
