#!/usr/bin/env bash
# Pull the managed-agent-sandbox image used by local e2b-local / OMA E2E.
# OrbStack/macOS hosts need linux/amd64; the registry currently has no arm64 manifest.
set -euo pipefail

REGISTRY_IMAGE="${SANDBOX_IMAGE:-registry.gz.cvte.cn/oma/managed-agent-sandbox:latest}"
LOCAL_TAG="${SANDBOX_LOCAL_TAG:-managed-agent-sandbox:latest}"
PLATFORM="${SANDBOX_PLATFORM:-linux/amd64}"

echo "pulling ${REGISTRY_IMAGE} (platform=${PLATFORM})"
docker pull --platform "${PLATFORM}" "${REGISTRY_IMAGE}"
docker tag "${REGISTRY_IMAGE}" "${LOCAL_TAG}"

echo "aligned tags:"
docker image inspect "${LOCAL_TAG}" --format 'Id={{.Id}} Created={{.Created}} RepoTags={{json .RepoTags}} Digests={{json .RepoDigests}}'
