# Kubernetes Network Emulation

[![Actions Status](https://github.com/openconfig/kne/workflows/Go/badge.svg)](https://github.com/openconfig/kne/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/openconfig/kne)](https://goreportcard.com/report/github.com/openconfig/kne)
[![GoDoc](https://godoc.org/istio.io/istio?status.svg)](https://pkg.go.dev/github.com/openconfig/kne)
[![License: BSD](https://img.shields.io/badge/license-Apache%202-blue)](https://opensource.org/licenses/Apache-2.0)
[![GitHub Super-Linter](https://github.com/openconfig/kne/workflows/Lint%20Code%20Base/badge.svg)](https://github.com/marketplace/actions/super-linter)
[![Coverage Status](https://coveralls.io/repos/github/openconfig/kne/badge.svg?branch=main)](https://coveralls.io/github/openconfig/kne?branch=main)

This is not an officially supported Google product.

## Goal

For network emulation, there are many approaches using VM's for emulation of a
hardware router. Arista, Cisco, Juniper, and Nokia have multiple implementations
of their network operating system and various generations of hardware emulation.
These systems are very good for most validation of vendor control plane
implementations and data plane for limited certifications. The idea of this
project is to provide a standard "interface" so that vendors can produce a
standard container implementation which can be used to build complex topologies.

* Have standard lifecycle management infrastructure for allowing multiple vendor
  device emulations to be present in a single "topology"
* Allow for control plane access via standard k8s networking
* Provide a common networking interface for the forwarding plane between network
  pods.
  * Data plane wires between pods
  * Control plane wires between topology manager
* Define service implementation for allowing interaction with the topology
  manager service.
  * Topology manager is the public API for allowing external users to manipulate
    the link state in the topology.
  * The topology manager will run as a service in k8s environment.
  * It will provide a gRPC interface for tests to interact with
  * It will listen to CRDs published via the network device pods for discovery
* Data plane connections for connectivity between pods must be a public
  transport mechanism
  * This can't be implemented as just exposing "x eth devices on the pod"
    because Linux doesn't understand the  associated control messages which are
    needed to make this work like a wire.
  * Transceiver state, optical characteristics, wire state, packet filtering /
    shaping / drops
  * LACP or other port aggregation protocols or APS cannot be simulated
    correctly
  * The topology manager will start a topology agent on each host for the pod to
    directly interact with.
  * The topology agent will provide the connectivity between nodes
* Define how pods boot an initial configuration
  * Ideally, this method would allow for dynamic
* Define how pods express services for use in-cluster as well as external
  services

## Use Cases

### Test Development

The main use case of this infrastructure is for the development of tests to
validate control plane / configuration of network devices without needing real
hardware.

The main use case we are interested in is the ability to bring up arbitrary
topologies to represent a production topology. This would require multiple
vendors as well as traffic generation and end hosts.

In support of the testing we need to be able to provide every tester, engineer
and continuous automated run a set of environments to validate test scenarios
used in production. These can also be used to pre-validate hardware testing as
well. This can reduce cycle time as there will be no contention for the virtual
testbed vs. the hardware testbed. This also allows for "unit testing" the
integration test.

### Software Development

For the development of new services or for offering a better environment to
developers for existing services, virtual testbeds would allow for better
scaling of resources and easier to use testbeds that would be customized for a
team's needs. Specifically, workflow automation struggles to have physical
representations of metros that need to be validated for workflows. A virtual
testbed would allow for the majority of workflows to be validated against any
number of production topologies.

## Usage

See the collection of [docs](docs/README.md) for in depth guides on how use
Kubernetes Network Emulation (KNE).

## Disclaimers

### Usage Metrics Reporting

We collect anonymous usage reporting metrics to gauge the health and
performance of various KNE operations (i.e. cluster deployment, topology
creation) on an **opt-in** basis. There is a global flag `--report_usage`
that when provided shares anonymous details about certain KNE CLI commands.
Collected data can be seen in the [event proto definition](proto/event.proto).
**Usage metrics are not shared by default.** Additionally the PubSub project and
topic the events are published to are configurable. If you want to track your
own private metrics about your KNE usage then that is supported by providing a
Cloud PubSub project/topic of your choosing. Full details about how/when usage
events are published can be found in the codebase
[here](kne/metrics/metrics.go). Usage metric reporting is appreciated to help
us develop a better KNE experience for all of our users.

## Thanks

This project is mainly based on the k8s-topo from github.com/networkop/k8s-topo
and meshnet-cni plugin from github.com/networkop/meshnet-cni.
