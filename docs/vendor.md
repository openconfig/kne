# Vendor Image Requirements

A Vendor Image is a docker container that can be used with KNE to emulate a
vendor's devices.  A Vendor Image might also be a fully virtual device with
no physical version, such as openconfig/lemming.

Without vendor images KNE is just an empty virtual machine rack that does
nothing.  Vendor supplied images are what the user of KNE sees and is interested
in.  This document describes the requirements and expectations of vendor images
and the associated code that is included in the KNE repository

A vendor image requires a corresponding node implementation in topo/node/vendor
and should have working examples in examples/vendor.  A single node
implementation may support multiple vendor images (e.g., cisco-xrd and cisco-8000e).  A maintainer is the person or organization that maintains the vendor
specific node implementation and examples.

In this document a vendor is considered the person or organization that makes
image containers available for use by others.

## KNE Uses

KNE was built to enable testing the functionality of networks without physical
hardware.  Due to the obvious limitations of emulation, KNE is not designed to
test bandwidth and latency of connections.  KNE is designed to enable testing of
the control protocols and interaction between devices.  There are several
different types of testing.

### Testing new Topologies

KNE is used to test changes in network topology.  Changes in network topology
can impact various protocols use in the network (e.g. BGP).

### Testing Changes in Protocol or Configuration

KNE is used to test protocol changes or other configuration changes.

### Testing Device Functionality

KNE is used to test changes to a device's Network Operating System (NOS).  This
is a crucial step in validating a devices usability for a particular purpose
when a new NOS is released.

## Fidelity

A network device in KNE can be viewed as two main components, the control plane
and the data plane (the ASIC).

KNE is used to test the control plane of the NOS.  This requires the control
software in the virtual device behave the same as in the hardware.  It is
expected that the control software used in an image is the same as the
software used on the physical device and that it is configured and reacts in the
same way as the hardware.

KNE is not designed to test the data plane or ASIC.  The emulated data plane
must support routing and packet forwarding.  ASIC specific commands and features
do not need to be supported as long as the data plane provides basic
functionality.

All of these use cases require that the vendor images to behave functionally as
if it were the hardware.  The image is expected to be built from the same source
code base as the NOS used in the hardware.  Faithful emulation of the ASIC is
not a requirement.  The emulated ASIC (data plane) must correctly handle routing
changes and packet forwarding.

### Deviations

The vendor should supply a document that describes what series of devices the
image emulates as well as known limits and deviations.  These include

* Protocols not supported
* Protocols that deviate from the hardware (and how)
* OpenConfig paths only supported by hardware
* OpenConfig paths that report different results compared to the hardware.
* Known limitations of the emulated device
* Supported port configurations (e.g, number of ports, line cards, etc).

The listed OpenConfig paths need not be leaf nodes.  Wildcards may be used in
the path where applicable.

## Testing

Vendor images must be tested prior to publication.  A standard set of tests is
found at <under development>.  At a minimum, a KNE node using that image should
start and not cause the KNE emulation to hang.  It should work in both a single
Kubernetes Worker Node environment as well as a multi-worker node environment.

It is expected that released images undergo repeated testing to identify
non-deterministic errors.

## Support

Vendors are responsible for support of their images.  The maintainer (typically
the person or organization that provides the associated container images) is
responsible for the support of the vendor image specific node implementation in
[kne/topo/node](https://github.com/openconfig/kne/tree/main/topo/node)
as well as the vendor specific examples in
[kne/examples](https://github.com/openconfig/kne/tree/main/examples).  The
maintainer should be responsive to community contributions.  In the event the
maintainer of a particular node implementation is unresponsive a new maintainer
may take over that implementation.
