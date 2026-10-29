#!/usr/bin/env bash
# Chaos test of the shared sessions with TWO REAL Vinaire pods (Roadmap Phase 111).
#
# It installs the chart with two replicas into a scratch namespace of the cluster kubectl points at, then:
#   1. a login on pod 1 is accepted by pod 2;
#   2. pod 1 is deleted: the session still works on pod 2 and on the pod that replaces pod 1 — nobody signs in again;
#   3. pod 2 is cut off from the Kubernetes API (an iptables rule on the k3d node drops what it sends to the API server's
#      port, connections that already exist included): within SESSION_STALE_LIMIT it keeps working, past it every session is refused (fail closed);
#      a session revoked on pod 1 during the cut is NOT honoured by pod 2 once the limit has passed;
#   4. the cut is lifted: pod 2 admits the surviving session again without a new login, the revoked one stays dead.
#
# Run it only against a disposable cluster (k3d). It never touches anything outside its namespace, a ClusterRoleBinding
# named chaos-ha-* and the cluster-scoped objects of the chart release (all removed at the end).
#
#   KUBECONFIG=... IMAGE_REPO=vinaire IMAGE_TAG=chaos scripts/chaos-ha.sh
#
# Needs: kubectl, helm, curl, sha256sum. The image must already be on the cluster's nodes (k3d image import).
set -uo pipefail

NS="${CHAOS_NS:-chaos-ha}"
REL=chaos
IMAGE_REPO="${IMAGE_REPO:-vinaire}"
IMAGE_TAG="${IMAGE_TAG:-chaos}"
LIMIT_SECONDS=20          # SESSION_STALE_LIMIT used by the test
P1=18081 P2=18082 P3=18083
PIDS=()
fails=0

say()  { printf '\n== %s\n' "$*"; }
ok()   { printf '   ok   %s\n' "$*"; }
bad()  { printf '   FAIL %s\n' "$*"; fails=$((fails+1)); }
check() { # check "description" expected actual
  if [ "$2" = "$3" ]; then ok "$1 ($3)"; else bad "$1 — wanted $2, got $3"; fi
}

cleanup() {
  for p in "${PIDS[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null; done
  kubectl delete clusterrolebinding chaos-ha-admin chaos-ha-viewer --ignore-not-found > /dev/null 2>&1
  helm uninstall "$REL" -n "$NS" > /dev/null 2>&1
  kubectl delete namespace "$NS" --ignore-not-found --wait=false > /dev/null 2>&1
}
trap cleanup EXIT

pf() { # pf pod port — forwards a port to ONE pod, remembers the process
  kubectl -n "$NS" port-forward "pod/$1" "$2:8080" > /dev/null 2>&1 &
  PIDS+=("$!")
  for _ in $(seq 1 40); do curl -sf "http://localhost:$2/healthz" > /dev/null && return 0; sleep 0.5; done
  echo "port-forward to $1 did not come up"; return 1
}

pods() { kubectl -n "$NS" get pods -l app.kubernetes.io/instance="$REL" -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}'; }

me() { # me port cookie → HTTP status of an authenticated read
  curl -s -o /dev/null -w '%{http_code}' --max-time 10 -H "Cookie: vinaire_session=$2" "http://localhost:$1/api/v1/auth/me"
}

login() { # login port token → prints the session cookie value
  curl -si --max-time 20 -X POST -H 'Content-Type: application/json' -d "{\"token\":\"$2\"}" "http://localhost:$1/api/v1/auth/login" \
    | tr -d '\r' | sed -n 's/^[Ss]et-[Cc]ookie: vinaire_session=\([^;]*\).*/\1/p' | head -1
}

until_code() { # until_code port cookie want seconds → waits for a status, prints the seconds it took (or "timeout")
  local start=$SECONDS
  while [ $((SECONDS-start)) -lt "$4" ]; do
    [ "$(me "$1" "$2")" = "$3" ] && { echo $((SECONDS-start)); return 0; }
    sleep 1
  done
  echo timeout; return 1
}

say "install two replicas into $NS"
kubectl create namespace "$NS" > /dev/null 2>&1 || true
helm upgrade --install "$REL" "$(dirname "$0")/../charts/vinaire" -n "$NS" \
  --set image.repository="$IMAGE_REPO" --set image.tag="$IMAGE_TAG" --set image.pullPolicy=Never \
  --set replicaCount=2 --set sessions.store=secret --set sessions.staleLimit="${LIMIT_SECONDS}s" \
  --set authMode=token --set cookie.secure=false \
  --wait --timeout 240s || { echo "install failed"; exit 1; }

# an administrator (may revoke) and a plain viewer, as ServiceAccounts
kubectl -n "$NS" create serviceaccount chaos-admin > /dev/null
kubectl -n "$NS" create serviceaccount chaos-viewer > /dev/null
kubectl create clusterrolebinding chaos-ha-admin --clusterrole=cluster-admin --serviceaccount="$NS:chaos-admin" > /dev/null
kubectl create clusterrolebinding chaos-ha-viewer --clusterrole=view --serviceaccount="$NS:chaos-viewer" > /dev/null
ADMIN_TOKEN=$(kubectl -n "$NS" create token chaos-admin --duration=40m)
VIEWER_TOKEN=$(kubectl -n "$NS" create token chaos-viewer --duration=40m)

read -r POD1 POD2 <<< "$(pods | tr '\n' ' ')"
say "pods: $POD1 and $POD2"
pf "$POD1" $P1 || exit 1
pf "$POD2" $P2 || exit 1

say "1. a login on pod 1 is accepted by pod 2"
ADMIN=$(login $P1 "$ADMIN_TOKEN")
VICTIM=$(login $P1 "$VIEWER_TOKEN")
KEEP=$(login $P1 "$VIEWER_TOKEN")
[ -n "$ADMIN" ] && [ -n "$VICTIM" ] && [ -n "$KEEP" ] || { echo "login failed"; exit 1; }
check "admin session on pod 1" 200 "$(me $P1 "$ADMIN")"
t=$(until_code $P2 "$VICTIM" 200 15); check "the viewer session reaches pod 2 (seconds)" "$t" "$t"
[ "$t" = timeout ] && bad "pod 2 never learned the session"

say "2. pod 1 is deleted: nobody signs in again"
kubectl -n "$NS" delete pod "$POD1" --wait=true > /dev/null
kubectl -n "$NS" rollout status deployment/"$REL-vinaire" --timeout=180s > /dev/null 2>&1 || kubectl -n "$NS" rollout status deployment --timeout=180s > /dev/null
check "pod 2 still admits the session" 200 "$(me $P2 "$VICTIM")"
NEW=$(pods | grep -v -x "$POD2" | grep -v -x "$POD1" | head -1)
if [ -z "$NEW" ]; then bad "no replacement pod found"; else
  pf "$NEW" $P3 || exit 1
  t=$(until_code $P3 "$VICTIM" 200 20); [ "$t" = timeout ] && bad "the replacement pod did not admit the session" || ok "the replacement pod admits the same cookie (after ${t}s, no new login)"
  check "the admin session works on the replacement" 200 "$(me $P3 "$ADMIN")"
fi

say "3. pod 2 is cut off from the Kubernetes API"
# The cut is an iptables rule on the (k3d) node that drops everything pod 2 sends to the API server's port. It does not depend
# on the CNI enforcing NetworkPolicy or on when it gets round to it (a NetworkPolicy also spares connections that already exist,
# which is the one Vinaire holds), and an explicit DROP at the top of INPUT stops established connections as well. On k3d the
# node is a container named like the node. The drill does not go on until the pod itself says it can't read the sessions.
NODE=$(kubectl -n "$NS" get pod "$POD2" -o jsonpath='{.spec.nodeName}')
POD2_IP=$(kubectl -n "$NS" get pod "$POD2" -o jsonpath='{.status.podIP}')
RULE=(-s "$POD2_IP" -p tcp --dport 6443 -j DROP)
docker exec "$NODE" iptables -I INPUT 1 "${RULE[@]}" || { echo "could not add the iptables rule on $NODE"; exit 1; }
trap 'docker exec "$NODE" iptables -D INPUT "${RULE[@]}" > /dev/null 2>&1; cleanup' EXIT
cut_effective=no
for _ in $(seq 1 40); do
  me $P2 "$KEEP" > /dev/null
  if kubectl -n "$NS" logs "$POD2" 2>/dev/null | grep -q "couldn't read it"; then cut_effective=yes; break; fi
  sleep 2
done
check "the cut took effect (pod 2 reports it cannot read the sessions)" yes "$cut_effective"
cut=$SECONDS
# the session is revoked on a healthy pod while pod 2 cannot see it
REF=$(printf %s "$VICTIM" | sha256sum | cut -c1-16)
code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 20 -X DELETE -H "Cookie: vinaire_session=$ADMIN" "http://localhost:$P3/api/v1/auth/sessions/$REF")
check "the revoke succeeds on a healthy pod" 200 "$code"
check "the healthy pod refuses the revoked session" 401 "$(me $P3 "$VICTIM")"

# how long until the cut pod refuses it: within the limit it may still say 200 (availability), past it must say 401
t=timeout; trace=""
while [ $((SECONDS-cut)) -lt $((LIMIT_SECONDS + 40)) ]; do
  c=$(me $P2 "$VICTIM"); trace="$trace $((SECONDS-cut))s:$c"
  [ "$c" = 401 ] && { t=ok; break; }
  sleep 1
done
echo "   timeline of the cut pod:$trace"
since=$((SECONDS-cut))
if [ "$t" = timeout ]; then bad "the cut pod still honours the revoked session after $((LIMIT_SECONDS + 40))s"; else
  if [ "$since" -le $((LIMIT_SECONDS + 15)) ]; then ok "the cut pod refused the revoked session ${since}s after the cut (limit ${LIMIT_SECONDS}s)"
  else bad "the cut pod needed ${since}s to refuse it (limit ${LIMIT_SECONDS}s)"; fi
fi
check "past the limit the cut pod refuses even the surviving session (fail closed)" 401 "$(me $P2 "$KEEP")"

say "4. the cut is lifted"
docker exec "$NODE" iptables -D INPUT "${RULE[@]}"
t=$(until_code $P2 "$KEEP" 200 40)
[ "$t" = timeout ] && bad "the surviving session did not come back on pod 2" || ok "the surviving session works on pod 2 again after ${t}s, without a new login"
check "the revoked session stays refused on pod 2" 401 "$(me $P2 "$VICTIM")"
check "the admin session still works on pod 2" 200 "$(me $P2 "$ADMIN")"

say "result"
if [ "$fails" -eq 0 ]; then echo "all scenarios passed"; else echo "$fails check(s) failed"; fi
exit "$fails"
