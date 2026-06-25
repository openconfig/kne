# Motivation

In K8S usually pods across nodes are connected using VxLAN/IPnIP/GRE overlay. Our objective is to connect these pods using grpc p2p overlay. One of the advantages is that there is **no reduction of MTU**. For example adding VXLAN will reduce virtual interface MTU by 20 bytes. With grpc-wire interface MTU remains 1500 (or can be made higher - not tried yet). Other advantages could be to add telemetry, generate wire up/down signal, etc.

# Introduction  
Let's take an example CRD as given in the picture below  
![CRD](./pics/crd.png)

When this crd is deployed with grpc-wire CNI, then the nodes across the K8S pods will interact over a grpc channel. Each grpc channel will provide a point to point connection between pods.  For example `POD-1:e1 <---> POD-2:e1` will be a dedicated grpc channel for POD1 & POD-2 communication via their `e1` virtual interfaces.   

![DEPLOYMENT](./pics/deployment.png)

Each node in the cluster runs a CNI daemon set. The daemon in the node is responsible to maintain the GRPC channel and send/receive packets over it. In a node, each pod is connected with the node daemon using a veth-pair. One end of the veth pair is inside the pod and the other end is with the daemon. Pods always writes to or reads from the the interface it has got. Whereas the daemon is always listening on the other end of the veth pair. As soon as the pod writes a packet, the daemon gets it and transports it over the grpc channel to the remote node. Daemon in the remote node delivers it to the destination pod.   
  
# Details

When a pod wants to send a packet to a remote pod, it writes it on the interface inside the pod. Pod is completely unaware of the grpc overlay being used.  The interface that a pod sees is one end of a veth pair. The other end of the veth pair is with meshnet daemon. The meshnet daemon receives any packet that a pod wants to send. Meshnet daemon uses the following proto to deliver the packet to the destination pod (on a different node).  

```go    
message Packet {
    int64 remot_intf_id = 1;   //remote machine interface id, to which packets to be delivered. 
    bytes frame = 2;           //raw bytes
}
```

The packet itself carries the id of the destination interface. Destination interface is an interface in the remote node and the meshnet daemon in the remote machine has access to this interface. This destination interface is one end of the veth pair and the other end of this veth pair is within the  destination pod. This is ensured during the wire creation time. So when the packet reaches the destination daemon, the demon simply writes the received packet on the interface carried by the packet itself. Since it's a veth pair the packet goes to the destination pod which is connected at the other end. It avoids any per packet lookup and packet delivery becomes an O(1) operation. 

Overall Tx/Rx mechanism is depicted in the picture below. 

![DETAIL](./pics/detail.png)

As shown it the picture above :-  
* When the pod-1 in node-1 has to send a BGP packet, it just writes it on the pod interface eth1. (*point C1 in the picture*)
* Other end of this veth-pair (`eth1-out1`) is with the daemon, where a thread is always waiting on `go channel` to read packet. (*point 1 in the picture*)
* Since the wire creation time, the daemon in node-1 knows, any packet received on `eth1-out1` has to be delivered to the daemon running on node-2 and the destination interface in node-2 is `eth2-out1` who’s id is `X2`.
* The thread in node-1 (*point 1 in the picture*) makes a GRPC service call `SendToOnce` to deliver the packet to the service handler in node-2 (*point 2  in the picture*). During the creation time node-1 and node-2 has established the GRPC connection.
* `SendToOnce` service in node-2 receives the packet and it also receives the desired destination interface id along with it. In our case it’s `X2`. It simply writes the packet on `X2`. Packet reaches the destination pod, which is at the other end of the veth pair `eth2-out1 (X2) <---> eth1` in node-2. In this case the destination pod is pod-2 and its interface `eth2`.
* In reverse direction 
  * When pod-2 in node-2 wants to send a packet to pod-1 in node-1, the same process continues.  Pod-2 writes it it’s interface `eth2`. (*point C2 in the picture*). The name `eth2` is assigned by meshnet CRD.
  * The packet goes to the other end of the veth pair `eth2-out1`
  * The daemon in node-2 receive the packet (*point 3 in the picture*) and makes a GRPC service call `SendToOnce` to deliver the packet to the service handler in node-1 (*point 4 in the picture*)
  * Since the wire creation time, the daemon in node-2 knows, any packet received on `eth2-out1` has to be delivered to the daemon running on node-1 and the destination interface in node-1 is `eth1-out1` who’s id is `X1`.
  *`SendToOnce` service in node-1 receives the packet and the desired destination interface id along with it. In our case it’s `X1`. It simply writes the packet on `X1`. Packet reaches the destination pod, which is at the other end of the veth pair `eth1-out1 (X1) <---> eth1` in node-1.




