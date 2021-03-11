# Kubernetes based network emulation

**This is not an officially supported Google product**

## Goal

For network emulation there are many approaches using VM's for emulation of a
hardware router.  Arista, Cisco and Juniper have multiple implementations of
their network operating system and various generations of hardware emulation.
These systems are very good for most validation of vendor control plane
implementations and data plane for limited certifications.  The idea of
this project is to provide a standard "interface" so that vendors can produce
a standard container implementation which can be used to build complex topologies.

## Use Cases

### Test Development

The main use case of this infrastructure is for the development of tests to
validate control plane / configuration of network devices without needing real
hardware.

The main use case we are interested in is the ability to bring up arbitrary
topologies to represent a production topology.  This would require multiple
vendors as well as traffic generation and end hosts.

In support of the testing we need to be able to provide every tester, engineer
and continuous automated run a set of environments to validate test scenarios
used in production.  These can also be used to pre-validate hardware testing
as well.  This can reduce cycle time as there will be no contention for the
virtual testbed vs. the hardware testbed. This also allows for "unit testing"
the integration test.

### Software Development

For the development of new services or for offering a better environment to
developers for existing services, virtual testbeds would allow for better
scaling of resources and easier to use testbeds that would be customized for
a team's needs.  Specifically workflow automation struggles to have physical
representations of metros that need to be validated for workflows. A virtual
testbed would allow for the majority of workflows to be validated against any
number of production topologies.

## Building

* Install go
* Build CLI

```bash
cd kne_cli
go build 
```
