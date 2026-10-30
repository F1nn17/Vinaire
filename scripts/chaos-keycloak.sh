#!/usr/bin/env bash
# Renewal of a Keycloak session with TWO REAL Vinaire pods and a REAL Keycloak, while one pod is cut off from the Kubernetes
# API (Roadmap Phase 113) — the part of the shared-sessions story that unit tests cannot show:
#
#   a person signs in through Keycloak (no browser: the sign-in form is walked with curl) -> the credential lives 10 minutes
#   (SESSION_TOKEN_TTL's minimum) and is renewed when a third is left -> at that moment pod 2 is cut off from the API:
#     * a request that reaches the cut pod does NOT rotate the refresh token (it can't take the renewal lease, which lives
#       in the cluster) and the session carries on with the credential it has;
#     * a request on the healthy pod renews it (Keycloak rotates the refresh token — the realm is set to do so);
#   -> the cut is lifted: pod 2 admits the same cookie without a new sign-in, and — after the ORIGINAL credential has run
#   out — a call as the person still works on pod 2, which can only be the renewed credential.
#
# Takes about 13 minutes (the credential can't be made shorter than 10 minutes). Disposable k3d cluster only; the script
# starts a Keycloak container on the cluster's docker network and removes it, its namespace and its bindings at the end.
#
#   IMAGE_REPO=vinaire IMAGE_TAG=chaos scripts/chaos-keycloak.sh
#
# Needs: kubectl, helm, curl, docker. The Vinaire image must already be on the cluster's node (k3d image import).
set -uo pipefail

NS="${CHAOS_NS:-chaos-kc}"
REL=chaos
IMAGE_REPO="${IMAGE_REPO:-vinaire}"
IMAGE_TAG="${IMAGE_TAG:-chaos}"
KC_IMAGE="${KC_IMAGE:-quay.io/keycloak/keycloak:26.0}"
CURL_IMAGE="${CURL_IMAGE:-curlimages/curl:8.12.1}"
KC_NAME=chaos-keycloak
KC_ADMIN_PASSWORD="chaos-keycloak-admin-pw"
REALM=vinaire-chaos
PERSON=chaos-person
PERSON_PASSWORD="Chaos-person-pw-1"
TOKEN_TTL=600             # SESSION_TOKEN_TTL=10m: the shortest there is
LIMIT_SECONDS=300         # SESSION_STALE_LIMIT: longer than the cut lasts, so what is tested is the renewal, not fail-closed
P1=18091 P2=18092
PIDS=()
fails=0

say()  { printf '\n== %s\n' "$*"; }
ok()   { printf '   ok   %s\n' "$*"; }
bad()  { printf '   FAIL %s\n' "$*"; fails=$((fails+1)); }
check() { if [ "$2" = "$3" ]; then ok "$1 ($3)"; else bad "$1 — wanted $2, got $3"; fi; }

NODE=""; NET=""; POD2_IP=""; IPT=""; API_IP=""; NODE_IP=""
cut_off() { [ -n "$NODE" ] && [ -n "$POD2_IP" ] && for c in $IPT; do for d in $API_IP $NODE_IP; do docker exec "$NODE" $c -t raw -D PREROUTING -s "$POD2_IP" -d "$d" -j DROP > /dev/null 2>&1; done; done; }
cleanup() {
  cut_off
  for f in "$PFDIR"/*; do [ -f "$f" ] && kill "$(cat "$f")" 2>/dev/null; done
  kubectl delete clusterrolebinding chaos-kc-admin chaos-kc-reader --ignore-not-found > /dev/null 2>&1
  helm uninstall "$REL" -n "$NS" > /dev/null 2>&1
  kubectl delete namespace "$NS" --ignore-not-found --wait=false > /dev/null 2>&1
  docker rm -f "$KC_NAME" > /dev/null 2>&1
}
trap cleanup EXIT

# A curl that runs on the cluster's docker network and can reach this machine as host.docker.internal: the sign-in is a
# browser's walk Keycloak -> Vinaire, and both ends must be reachable from where curl is (Docker Desktop and Linux alike).
dcurl() { docker run --rm --network "$NET" --add-host=host.docker.internal:host-gateway "$CURL_IMAGE" "$@"; }

PFDIR=$(mktemp -d)   # one file per forwarded port holding its process id (the forwards are restarted inside $(...))
declare -A FWD_POD FWD_ADDR
pf() { # pf pod port address — forwards a port to ONE pod
  FWD_POD[$2]=$1; FWD_ADDR[$2]=$3
  kubectl -n "$NS" port-forward --address "$3" "pod/$1" "$2:8080" > /dev/null 2>&1 &
  echo $! > "$PFDIR/$2"
  for _ in $(seq 1 40); do curl -sf "http://localhost:$2/healthz" > /dev/null && return 0; sleep 0.5; done
  echo "port-forward to $1 did not come up"; return 1
}
pods() { kubectl -n "$NS" get pods -l app.kubernetes.io/instance="$REL" -o go-template='{{range .items}}{{if not .metadata.deletionTimestamp}}{{.metadata.name}}{{"\n"}}{{end}}{{end}}'; }
# kubectl port-forward is a long-lived process and is known to end on its own (an idle stream, a dropped connection) — that is
# the harness, not Vinaire, so a refused connection brings it up again once and the request is repeated.
repf() { kill "$(cat "$PFDIR/$1")" 2>/dev/null; pf "${FWD_POD[$1]}" "$1" "${FWD_ADDR[$1]}" > /dev/null; }
code() { # code port cookie path [max-seconds] → HTTP status
  local out rc
  out=$(curl -s -o /dev/null -w '%{http_code}' --max-time "${4:-15}" -H "Cookie: vinaire_session=$2" "http://localhost:$1$3"); rc=$?
  if [ "$rc" = 7 ] || [ "$rc" = 52 ] || [ "$rc" = 56 ]; then
    repf "$1"
    out=$(curl -s -o /dev/null -w '%{http_code}' --max-time "${4:-15}" -H "Cookie: vinaire_session=$2" "http://localhost:$1$3")
  fi
  printf %s "$out"
}
diag2() { # what pod 2 and the port-forward to it look like right now
  echo "   --- diagnostics ($1): port-forward to pod 2 alive: $(kill -0 "$(cat "$PFDIR/$P2")" 2>/dev/null && echo yes || echo no); healthz: $(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "http://localhost:$P2/healthz")"
  pod_log "$POD2" | tail -12 | cut -c1-260
}
pod_log() { for p in "$@"; do kubectl -n "$NS" logs "$p" 2>/dev/null; done; }

say "start Keycloak next to the cluster"
NODE=$(kubectl get nodes -o jsonpath='{.items[0].metadata.name}')
NET=$(docker inspect -f '{{range $k,$v := .NetworkSettings.Networks}}{{$k}} {{end}}' "$NODE" | awk '{print $1}')
docker run -d --name "$KC_NAME" --network "$NET" -e KEYCLOAK_ADMIN=admin -e KEYCLOAK_ADMIN_PASSWORD="$KC_ADMIN_PASSWORD" \
  "$KC_IMAGE" start-dev > /dev/null || { echo "could not start Keycloak"; exit 1; }
KC_IP=$(docker inspect -f "{{(index .NetworkSettings.Networks \"$NET\").IPAddress}}" "$KC_NAME")
KC="http://$KC_IP:8080"
echo "   Keycloak at $KC (network $NET)"
for _ in $(seq 1 100); do dcurl -sf "$KC/realms/master" > /dev/null 2>&1 && break; sleep 3; done
dcurl -sf "$KC/realms/master" > /dev/null || { docker logs "$KC_NAME" | tail -30; echo "Keycloak did not come up"; exit 1; }

say "install two replicas into $NS"
kubectl create namespace "$NS" > /dev/null 2>&1 || true
helm upgrade --install "$REL" "$(dirname "$0")/../charts/vinaire" -n "$NS" \
  --set image.repository="$IMAGE_REPO" --set image.tag="$IMAGE_TAG" --set image.pullPolicy=Never \
  --set replicaCount=2 --set sessions.store=secret --set sessions.staleLimit="${LIMIT_SECONDS}s" \
  --set authMode=token --set cookie.secure=false --set keycloak.allowHttpPrivateNetwork=true \
  --set sessionTokenTTL=10m --set sessionMaxTTL=2h \
  --wait --timeout 240s > /dev/null || { echo "install failed"; exit 1; }

kubectl -n "$NS" create serviceaccount chaos-admin > /dev/null
kubectl create clusterrolebinding chaos-kc-admin --clusterrole=cluster-admin --serviceaccount="$NS:chaos-admin" > /dev/null
ADMIN_TOKEN=$(kubectl -n "$NS" create token chaos-admin --duration=60m)

read -r POD1 POD2 <<< "$(pods | tr '\n' ' ')"
pf "$POD1" $P1 0.0.0.0 || exit 1
BASE="http://localhost:$P1"   # what Keycloak sends the browser back to; the administrator's setup is loopback to loopback

say "set Keycloak sign-in up from Vinaire (as an administrator), then restart so BOTH pods restore it"
ADMIN=$(curl -si --max-time 20 -X POST -H 'Content-Type: application/json' -d "{\"token\":\"$ADMIN_TOKEN\"}" "http://localhost:$P1/api/v1/auth/login" \
  | tr -d '\r' | sed -n 's/^[Ss]et-[Cc]ookie: vinaire_session=\([^;]*\).*/\1/p' | head -1)
[ -n "$ADMIN" ] || { echo "administrator login failed"; exit 1; }
body=$(printf '{"keycloakUrl":"%s","adminUser":"admin","adminPassword":"%s","realm":"%s","user":"%s","userEmail":"%s@example.com","userPassword":"%s","userChangePassword":false,"allowTokenLogin":true,"identityMode":"serviceaccount"}' \
  "$KC" "$KC_ADMIN_PASSWORD" "$REALM" "$PERSON" "$PERSON" "$PERSON_PASSWORD")
resp=$(curl -s --max-time 120 -w '\n%{http_code}' -X POST -H 'Content-Type: application/json' -H "Origin: $BASE" -H "Cookie: vinaire_session=$ADMIN" -d "$body" "$BASE/api/v1/auth/keycloak/setup")
check "the Keycloak setup" 200 "$(echo "$resp" | tail -1)"
[ "$(echo "$resp" | tail -1)" = 200 ] || { echo "$resp" | head -c 600; exit 1; }

# the realm rotates refresh tokens (a used one is dead), and the person has a name so Keycloak asks for nothing more
akt=$(dcurl -s -d "client_id=admin-cli&grant_type=password&username=admin&password=$KC_ADMIN_PASSWORD" "$KC/realms/master/protocol/openid-connect/token" | sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p')
[ -n "$akt" ] || { echo "no Keycloak admin token"; exit 1; }
check "the realm rotates refresh tokens" 204 "$(dcurl -s -o /dev/null -w '%{http_code}' -X PUT -H "Authorization: Bearer $akt" -H 'Content-Type: application/json' \
  -d '{"revokeRefreshToken":true,"refreshTokenMaxReuse":0}' "$KC/admin/realms/$REALM")"
uid=$(dcurl -s -H "Authorization: Bearer $akt" "$KC/admin/realms/$REALM/users?username=$PERSON&exact=true" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p' | head -1)
check "the person is complete (name set)" 204 "$(dcurl -s -o /dev/null -w '%{http_code}' -X PUT -H "Authorization: Bearer $akt" -H 'Content-Type: application/json' \
  -d "{\"firstName\":\"Chaos\",\"lastName\":\"Person\",\"email\":\"$PERSON@example.com\",\"emailVerified\":true,\"requiredActions\":[]}" "$KC/admin/realms/$REALM/users/$uid")"

for f in "$PFDIR"/*; do [ -f "$f" ] && kill "$(cat "$f")" 2>/dev/null; done; rm -f "$PFDIR"/*
kubectl -n "$NS" rollout restart deployment/"$REL-vinaire" > /dev/null
kubectl -n "$NS" rollout status deployment/"$REL-vinaire" --timeout=240s > /dev/null || { echo "restart failed"; exit 1; }
read -r POD1 POD2 <<< "$(pods | tr '\n' ' ')"
say "pods: $POD1 and $POD2"
pf "$POD1" $P1 0.0.0.0 || exit 1
pf "$POD2" $P2 127.0.0.1 || exit 1

say "1. the person signs in through Keycloak (curl walks the sign-in form)"
LOGIN='
jar=/tmp/jar; base="$1"; port="$4"
# the redirect names localhost, which inside this container is not the machine the port-forward runs on
c() { curl -s --connect-to "localhost:$port:host.docker.internal:$port" -c $jar -b $jar -L "$@"; }
c -o /tmp/page.html "$base/api/v1/auth/oidc/login"
action=$(grep -o "action=\"[^\"]*\"" /tmp/page.html | head -1 | sed "s/^action=\"//; s/\"\$//; s/&amp;/\&/g")
[ -n "$action" ] || { echo "NOFORM"; head -c 400 /tmp/page.html >&2; exit 1; }
c -o /dev/null --data-urlencode "username=$2" --data-urlencode "password=$3" "$action"
grep vinaire_session $jar | awk "{print \$NF}"'
COOKIE=$(docker run --rm --network "$NET" --add-host=host.docker.internal:host-gateway --entrypoint sh "$CURL_IMAGE" -c "$LOGIN" sh "$BASE" "$PERSON" "$PERSON_PASSWORD" "$P1")
T0=$SECONDS
[ -n "$COOKIE" ] && [ "$COOKIE" != NOFORM ] || { echo "sign-in through Keycloak failed: $COOKIE"; exit 1; }
ME=$(curl -s --max-time 10 -H "Cookie: vinaire_session=$COOKIE" "http://localhost:$P1/api/v1/auth/me")
echo "   $ME"
KUSER=$(printf %s "$ME" | sed -n 's/.*"kubernetesUser":"\([^"]*\)".*/\1/p')
case "$KUSER" in system:serviceaccount:*:oidc-*) ok "the cluster knows the person as $KUSER";; *) bad "kubernetesUser is '$KUSER'";; esac
USERNS=$(printf %s "$KUSER" | cut -d: -f3); ACCOUNT=$(printf %s "$KUSER" | cut -d: -f4)
kubectl create clusterrolebinding chaos-kc-reader --clusterrole="$REL-vinaire-reader" --serviceaccount="$USERNS:$ACCOUNT" > /dev/null
for _ in $(seq 1 30); do [ "$(code $P1 "$COOKIE" /api/v1/pods)" = 200 ] && break; sleep 1; done
check "a call as the person works on pod 1" 200 "$(code $P1 "$COOKIE" /api/v1/pods)"
check "the same cookie works on pod 2" 200 "$(code $P2 "$COOKIE" /api/v1/pods)"

DUE=$((TOKEN_TTL * 2 / 3))   # a credential is renewed when less than a third of its life is left
say "2. wait until the credential is almost due (${DUE}s after the sign-in)"
while [ $((SECONDS-T0)) -lt $((DUE - 25)) ]; do sleep 5; done
echo "   $((SECONDS-T0))s after the sign-in; no renewal yet: $(pod_log "$POD1" "$POD2" | grep -c oidc-token-renewed)"

say "3. pod 2 is cut off from the Kubernetes API just before the renewal is due"
# The stale limit counts from the last SUCCESSFUL read of the sessions, and a replica that has had no request for longer than the
# limit has made none — so, cut off, it would (rightly) refuse everything. Real traffic keeps a replica warm; the drill does so too.
check "pod 2 is warm: it has just read the sessions" 200 "$(code $P2 "$COOKIE" /api/v1/auth/me)"
POD2_IP=$(kubectl -n "$NS" get pod "$POD2" -o jsonpath='{.status.podIP}')
# What is dropped is what the pod sends to the Kubernetes API, in the raw table's PREROUTING chain — the first place a packet is
# looked at, before conntrack, NAT and kube-router's chains (a DROP in INPUT saw 0 packets on the CI runner): to the ClusterIP
# of the `kubernetes` Service (before NAT) and to the node's own address (after a DNAT, or a client that goes there directly).
# NOT everything the pod sends: its replies to the kubelet's probes come from its address too, and dropping them makes the
# liveness probe fail and the container restart — and a container that starts while the API is cut can't start at all.
# Added with every iptables front-end the node has (the one that doesn't work on its kernel just fails).
API_IP=$(kubectl get svc kubernetes -n default -o jsonpath='{.spec.clusterIP}')
NODE_IP=$(kubectl get node "$NODE" -o jsonpath='{.status.addresses[?(@.type=="InternalIP")].address}')
IPT=$(docker exec "$NODE" sh -c 'for c in iptables iptables-legacy iptables-nft; do command -v $c > /dev/null 2>&1 && echo $c; done')
n=0; for c in $IPT; do for d in $API_IP $NODE_IP; do docker exec "$NODE" $c -t raw -I PREROUTING 1 -s "$POD2_IP" -d "$d" -j DROP 2> /dev/null && n=$((n+1)); done; done
[ "$n" -gt 0 ] || { echo "could not cut pod 2 off"; exit 1; }
cut_effective=no
for _ in $(seq 1 20); do
  code $P2 "$COOKIE" /api/v1/auth/me > /dev/null
  if pod_log "$POD2" | grep -q "couldn't read it"; then cut_effective=yes; break; fi
  sleep 2
done
check "the cut took effect (pod 2 reports it cannot read the sessions)" yes "$cut_effective"
while [ $((SECONDS-T0)) -lt $((DUE + 10)) ]; do sleep 2; done
echo "   $((SECONDS-T0))s after the sign-in: the credential is due"

# a request that reaches the cut pod: it must not spend the refresh token, and the session must go on
c=$(code $P2 "$COOKIE" /api/v1/auth/me 60)
check "the cut pod keeps the session on the credential it has (due, no renewal possible)" 200 "$c"
[ "$c" = 200 ] || diag2 "the due request on the cut pod"
check "the cut pod did not renew (it could not take the lease)" 0 "$(pod_log "$POD2" | grep -c oidc-token-renewed)"
echo "   pod 2 says: $(pod_log "$POD2" | grep -m1 "couldn't coordinate" | sed 's/^.*msg=//' | head -c 200)"

say "4. the healthy pod renews the credential (Keycloak rotates the refresh token)"
check "a request on pod 1" 200 "$(code $P1 "$COOKIE" /api/v1/auth/me 60)"
renewed=no
for _ in $(seq 1 30); do pod_log "$POD1" | grep -q oidc-token-renewed && { renewed=yes; break; }; sleep 1; done
check "pod 1 renewed it (audit oidc-token-renewed)" yes "$renewed"
check "no pod ended the session" 0 "$(pod_log "$POD1" "$POD2" | grep -c 'session ended at renewal')"

say "5. the cut is lifted"
cut_off
t=timeout
start=$SECONDS
while [ $((SECONDS-start)) -lt 60 ]; do [ "$(code $P2 "$COOKIE" /api/v1/auth/me)" = 200 ] && { t=$((SECONDS-start)); break; }; sleep 1; done
[ "$t" = timeout ] && { bad "pod 2 did not admit the session after the cut"; diag2 "after the cut was lifted"; } || ok "pod 2 admits the same cookie again after ${t}s, without a new sign-in"

say "6. the ORIGINAL credential runs out — only the renewed one can still work"
while [ $((SECONDS-T0)) -lt $((TOKEN_TTL + 30)) ]; do sleep 5; done
echo "   $((SECONDS-T0))s after the sign-in"
ok_on_2=no
for _ in $(seq 1 20); do [ "$(code $P2 "$COOKIE" /api/v1/pods)" = 200 ] && { ok_on_2=yes; break; }; sleep 3; done
check "a call as the person works on pod 2 (the renewed credential came through the shared store)" yes "$ok_on_2"
check "a call as the person works on pod 1" 200 "$(code $P1 "$COOKIE" /api/v1/pods)"
check "the credential was renewed once, not once per pod" 1 "$(( $(pod_log "$POD1" | grep -c oidc-token-renewed) + $(pod_log "$POD2" | grep -c oidc-token-renewed) ))"

say "result"
if [ "$fails" -eq 0 ]; then echo "all scenarios passed"; else echo "$fails check(s) failed"; fi
exit "$fails"
