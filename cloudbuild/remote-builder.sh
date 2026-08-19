#!/bin/bash -xe
# Copyright 2017-2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Configurable parameters
[ -z "$COMMAND" ] && echo "Need to set COMMAND" && exit 1

USERNAME=${USERNAME:-admin}
REMOTE_WORKSPACE=${REMOTE_WORKSPACE:-/home/${USERNAME}/workspace/}
INSTANCE_NAME=${INSTANCE_NAME:-builder-$(cat /proc/sys/kernel/random/uuid)}
REGION=${REGION:-us-central1}
INSTANCE_ARGS=${INSTANCE_ARGS:---preemptible}
SSH_ARGS=${SSH_ARGS:-}
GCLOUD=${GCLOUD:-gcloud}
RETRIES=${RETRIES:-10}

CREATED_ZONE=""

# Always delete instance after attempting build
cleanup() {
	if [ -n "${CREATED_ZONE}" ]; then
		"${GCLOUD}" compute instances delete "${INSTANCE_NAME}" --zone="${CREATED_ZONE}" --quiet || true
	fi
}
trap cleanup EXIT

# Run command on the instance via ssh
ssh() {
	# shellcheck disable=SC2086
	"${GCLOUD}" compute ssh ${SSH_ARGS} --zone="${CREATED_ZONE}" --ssh-key-file="${KEYNAME}" \
		"${USERNAME}"@"${INSTANCE_NAME}" -- "$1"
}

KEYNAME=builder-key
# TODO Need to be able to detect whether a ssh key was already created
ssh-keygen -t rsa -N "" -f "${KEYNAME}" -C "${USERNAME}" || true
chmod 400 "${KEYNAME}"*

cat >ssh-keys <<EOF
${USERNAME}:$(cat "${KEYNAME}.pub")
EOF

# Determine candidate zones
if [ -n "${ZONE}" ]; then
	CANDIDATE_ZONES="${ZONE}"
else
	CANDIDATE_ZONES=$("${GCLOUD}" compute zones list --filter="region:(${REGION}) AND status:UP" --format="value(name)" | shuf)
fi

if [ -z "${CANDIDATE_ZONES}" ]; then
	echo "Error: No UP zones found for region '${REGION:-${ZONE}}'."
	exit 1
fi

echo "Candidate zone evaluation order:"
echo "${CANDIDATE_ZONES}"

for z in ${CANDIDATE_ZONES}; do
	echo "Attempting to create instance '${INSTANCE_NAME}' in zone '${z}'..."
	set +e
	# shellcheck disable=SC2086
	CREATE_OUT=$("${GCLOUD}" compute instances create \
		${INSTANCE_ARGS} "${INSTANCE_NAME}" \
		--zone="${z}" \
		--metadata block-project-ssh-keys=TRUE \
		--metadata-from-file ssh-keys=ssh-keys 2>&1)
	CREATE_STATUS=$?
	set -e

	if [ ${CREATE_STATUS} -eq 0 ]; then
		echo "Successfully created instance '${INSTANCE_NAME}' in zone '${z}'."
		CREATED_ZONE="${z}"
		break
	fi

	echo "Failed to create instance in zone '${z}':"
	echo "${CREATE_OUT}"

	# Check if failure is due to capacity stockout
	if echo "${CREATE_OUT}" | grep -Eq "ZONE_RESOURCE_POOL_EXHAUSTED|state:STOCKOUT|does not have enough resources|resource pool exhausted"; then
		echo "Stockout detected in zone '${z}'. Retrying next zone..."
	else
		echo "Non-stockout failure encountered in zone '${z}'. Aborting."
		exit ${CREATE_STATUS}
	fi
done

if [ -z "${CREATED_ZONE}" ]; then
	echo "Error: All candidate zones exhausted due to stockouts for instance '${INSTANCE_NAME}'."
	exit 1
fi

"${GCLOUD}" config set compute/zone "${CREATED_ZONE}"

RETRY_COUNT=1
while [ "$(ssh 'printf pass')" != "pass" ]; do
	echo "[Try $RETRY_COUNT of $RETRIES] Waiting for instance to start accepting SSH connections..."
	if [ "$RETRY_COUNT" == "$RETRIES" ]; then
		echo "Retry limit reached, giving up!"
		exit 1
	fi
	sleep 10
	RETRY_COUNT=$((RETRY_COUNT + 1))
done

# shellcheck disable=SC2086
"${GCLOUD}" compute scp ${SSH_ARGS} --compress --recurse --zone="${CREATED_ZONE}" \
	"$(pwd)" "${USERNAME}"@"${INSTANCE_NAME}":"${REMOTE_WORKSPACE}" \
	--ssh-key-file="${KEYNAME}"

ssh "${COMMAND}"

# shellcheck disable=SC2086
"${GCLOUD}" compute scp ${SSH_ARGS} --compress --recurse --zone="${CREATED_ZONE}" \
	"${USERNAME}"@"${INSTANCE_NAME}":"${REMOTE_WORKSPACE}"* "$(pwd)" \
	--ssh-key-file="${KEYNAME}"
