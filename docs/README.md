# KNE Documentation

KNE documentation can be found in this directory. There are multiple resources
found here, each is described below.

## User How To

KNE is a Google initiative to develop tooling for quickly setting up topologies
of containers running various device OSes.

This document is meant to serve as a How-To guide for various KNE usage. The
guide is broken up into multiple sections spanning multiple documents.

* [Setup](setup.md): A guide to first time setup for KNE.
* [Create a topology](create_topology.md): A guide to deploying a KNE cluster
  and creating a topology.
* [Interact with a topology](interact_topology.md): A guide to interacting with
  a KNE topology after creation.
* [Troubleshooting](troubleshoot.md): A troubleshooting guide if anything goes
  wrong along the way.

They are recommended to be done in order.

## KNE with a Multi Node Cluster

[KNE with a Mult Node Cluster](multinode.md)

KNE can easily be scaled to run large topologies utilizing its Kubernetes
backbone. This guide describes how to set up a k8s multi worker node cluster
and get a 150 node KNE topology up and running.

## Kubernetes Reference

[Kubernetes Reference](kubernetes_reference.md)

KNE utilizes many aspects of Kubernetes. This document is a primer on k8s
concepts and how they are used in KNE by running through an example topology
creation.
