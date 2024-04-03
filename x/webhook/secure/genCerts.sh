#!/bin/bash
#
# Generate the server certificate and key as well as the certificate authority
# to be used by the kube cluster to establish rpc connection to the mutating
# webhook.

openssl genrsa -out ca.key 2048

openssl req -new -x509 -days 365 -key ca.key \
  -subj "/C=AU/CN=kne-assembly-webhook"\
  -out ca.crt

openssl req -newkey rsa:2048 -nodes -keyout server.key \
  -subj "/C=AU/CN=kne-assembly-webhook" \
  -out server.csr

openssl x509 -req \
  -extfile <(printf "subjectAltName=DNS:kne-assembly-webhook.default.svc") \
  -days 365 \
  -in server.csr \
  -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out server.crt

echo
echo ">> Generating kube secrets..."
kubectl create secret tls kne-assembly-webhook-tls \
  --cert=server.crt \
  --key=server.key \
  --dry-run=client -o yaml \
  > manifests/tls.secret.yaml

echo
echo ">> MutatingWebhookConfiguration caBundle:"
< ca.crt base64 | fold

rm ca.crt ca.key ca.srl server.crt server.csr server.key
