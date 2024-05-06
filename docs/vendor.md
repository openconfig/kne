# Vendor Image Requirements

A Vendor Image is a docker container that can be used with KNE to emulate a
vendor's devices.

Without vendor images KNE is just an empty virtual machine rack that does
nothing.  Vendor supplied images are what the user of KNE sees and is interested
in.  This document describes the requirements and expectations of vendor images
to be used with KNE.

This document describes the image requirements and expectations in order to be
considered *KNE Qualified*.

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

The vendor should supply a document that describes what series of devices this
image emulates as well as known limits and deviations.  These include

* Protocols not supported
* Protocols that deviate from the hardware (and how)
* OpenConfig paths only supported by hardware
* OpenConfig paths that report different results compared to the hardware.
* Known limitations of the emulated device

The listed OpenConfig paths need not be leaf nodes.  Wildcards may be used in
the path where applicable.

## Testing

Vendor images must be tested prior to publication.  A standard set of tests is
found at <INSERT LOCATION HERE>.  It is expected that the image undergoes
repeated testing to identify non-deterministic errors.

## Support

Vendors are responsible for support of their images.  This includes the node
implementation in
[kne/topo/node](https://github.com/openconfig/kne/tree/main/topo/node) as well
as the vendor specific examples in
[kne/examples](https://github.com/openconfig/kne/tree/main/examples).
The vendors should review and provide approval for any community contributions
to the code they are responsible for.
