#!/usr/bin/env bash
# Waits until the cluster `kubectl` points at answers steadily (Roadmap Phase 97's companion).
#
# `k3d cluster create --wait` returns when the node is up, not when the API server has settled: for a while after
# that Helm and kubectl fail with "the server is currently unable to handle the request" — from /version, or from
# listing the API groups while an aggregated API (metrics-server) is still starting — and one good answer is not
# proof, because the API server can still answer once and then restart a component. So: the cluster must be ready,
# report its version and list every API group, five times in a row.
set -uo pipefail

need=5
ok=0
for _ in $(seq 1 90); do
  if kubectl get --raw=/readyz >/dev/null 2>&1 \
    && kubectl get --raw=/version >/dev/null 2>&1 \
    && kubectl api-resources >/dev/null 2>&1; then
    ok=$((ok + 1))
    if [ "$ok" -ge "$need" ]; then
      kubectl wait --for=condition=Ready nodes --all --timeout=120s
      exit 0
    fi
  else
    ok=0
  fi
  sleep 2
done

kubectl get apiservices 2>&1 | grep -v True || true
echo "::error::the cluster's API did not answer steadily within three minutes"
exit 1
