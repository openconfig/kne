#!/bin/bash

# exit when a command fails
set -e

echo "Beginning webhook build"
go build /kne/webhook:dsdn_webhook
echo "webhook build complete."

WEBHOOK_BINARY="$(cat "$(blaze info master-log)" | grep output_file | awk '{print $3}')"

# Create temporary directory and copy in binary.
mkdir build-out
cp "$WEBHOOK_BINARY" ./build-out/

docker build -t kne_webhook -f Dockerfile.webhook .

# Remove the temporary build directory.
rm -rf ./build-out/

echo "docker build complete. Have a nice day."
