# How to configure Ciena simulators in KNE

## Interface naming
- `eth0` - Node management interface

### For saos
- `1` - First dataplane interface
- `X` - Subsequent dataplane interfaces will count onwards from 1. For example, the third dataplane interface will be `3`

### For waverouter
Waverouter port numbering format is: `<housing>/<slot>/<port>`. In the example below, 1/5/1, this is housing 1, slot 5, port 1
- `1/5/1` - First dataplane interface
- `1/5/X` - Subsequent dataplane interfaces will count onwards from 1. For example, the third dataplane interface will be `1/5/3`

### Notes:
- You can also use interface aliases of `ethX` (count onwards from 1) for both saos and waverouter
- We only support one waverouter interface box (using the wr-qbox type in JSON) at this time

### [wr13_example.json](./wr13_example.json)
```json
{
    "WR1": {
        "1": {
            "type": "wr13",
            "7": {
                "type": "wr-ctm"
            },
            "5": {
                "type": "wr-qbox"
            }
        }
    }
}
```

## [saos.pbtxt topology](./saos.pbtxt)
This topology includes 2 saos which has 2 connections, saos-1 1----1 saos-2, saos-1 2----2 saos-2
```yaml
name: "saos-example"
nodes: {
    name: "saos-1"
    vendor: CIENA
    model: "5132"
    # when `image` is not specified under `config`, the "artifactory.ciena.com/psa/saos-containerlab:latest" container image is used by default
}
nodes: {
    name: "saos-2"
    vendor: CIENA
    model: "5132"
    # when `image` is not specified under `config`, the "artifactory.ciena.com/psa/saos-containerlab:latest" container image is used by default
}
links: {
    a_node: "saos-1"
    a_int: "1"
    z_node: "saos-2"
    z_int: "1"
}
links: {
    a_node: "saos-1"
    a_int: "2"
    z_node: "saos-2"
    z_int: "2"
}
```

## [waverouter.pbtxt topology](./waverouter.pbtxt)
This topology includes one saos and one waverouter which has 2 connections, saos-1 1----1/5/1 wr-1, saos-1 2----1/5/2 wr-1
* Waverouter model requires an additional file (wr13_example.json) to describe the internal system topology
```yaml
name: "waverouter-saos-example"
nodes: {
    name: "wr-1"
    vendor: CIENA
    model: "waverouter"
    config: {
        # when `image` is not specified under `config`, the "artifactory.ciena.com/psa/rw-containerlab:latest" container image is used by default
        vendor_data {
            [type.googleapis.com/ciena.CienaConfig] {
                system_equipment: {
                    equipment_json: "wr13_example.json"
                }
            }
        }
    }
}
nodes: {
    name: "saos-1"
    vendor: CIENA
    model: "5132"
    # when `image` is not specified under `config`, the "artifactory.ciena.com/psa/saos-containerlab:latest" container image is used by default
}
links: {
    a_node: "saos-1"
    a_int: "1"
    z_node: "wr-1"
    z_int: "1/5/1"
}
links: {
    a_node: "saos-1"
    a_int: "2"
    z_node: "wr-1"
    z_int: "1/5/2"
}
```