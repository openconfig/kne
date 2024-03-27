#!/bin/bash

# exit when a command fails
set -e

echo "Beginning webhook build"
go build
echo "webhook build complete."

docker build -t webhook -f Dockerfile.webhook .

echo "docker build complete. Have a nice day."
