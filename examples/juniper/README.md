# How to: verify and use the services on cPTX

This examples shows about how to verify and use the GRPC services on the cPTX

## Get into cPTX and check the status of GRPC services

Once the KNE cluster is up, get into cPTX and Check the status of GRPC services

```bash
$ kubectl exec -it -n <NAMESPACE> cptx1 -- cli
root@cptx1>
root@cptx1> show system connections |match 50051
tcp6       0      0 :::50051                :::*                    LISTEN      XXXXX/jsd

root@cptx1>
root@cptx1> show configuration system services extension-service
request-response {
    grpc {
        ssl {
            hot-reloading;
            use-pki;
        }
    }
}
root@cptx1>
```

## Dial the services using cli tools

```bash
$ export GNMI_USER=root
$ export GNMI_PASS=Google123
$ gnmi_cli -a <NODE EXTERNAL IP>:50051 -q "/interfaces/interface/" -tls_skip_verify -with_user_pass
```
