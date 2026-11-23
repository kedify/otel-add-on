#!/usr/bin/env bash
set -euo pipefail

usage='Usage: test-failover.sh [kubectl-context] [rounds] [namespace] [expected-replicas] [scaled-object-manifest]'
# Two rounds delete one and then two scaler Pods. Use zero for a scaling-only check.
context=${1:-otel-scaler-ha}
rounds=${2:-2}
namespace=${3:-ha-enabled}
expected_replicas=${4:-}
manifest=${5:-}
scaler=keda-otel-scaler
scaled_object=podinfo-pull-example

fail() {
  printf '%s\n' "$*" >&2
  exit 1
}

[[ $# -le 5 && $rounds =~ ^(0|[1-9][0-9]*)$ ]] || fail "$usage"
for command in kubectl jq curl hey; do
  command -v "$command" >/dev/null || fail "Required command: $command"
done

kube=(kubectl --context "$context" --namespace "$namespace" --request-timeout=10s)
tmp=$(mktemp -d)
forward_pid=
load_pid=
victims='[]'
finalizer=e2e.otel-add-on.kedify.io/hold-replacement

stop_background() {
  local pid
  for pid in "$load_pid" "$forward_pid"; do
    if [[ -n $pid ]]; then
      kill "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
    fi
  done
  load_pid=
  forward_pid=
}

release_victims() {
  local pod status=0
  while IFS= read -r pod; do
    "${kube[@]}" patch pod "$pod" --type=strategic \
      -p "{\"metadata\":{\"\$deleteFromPrimitiveList/finalizers\":[\"$finalizer\"]}}" >/dev/null || status=1
  done < <(jq -r '.[].name' <<< "$victims")
  return "$status"
}

cleanup() {
  stop_background
  release_victims || true
  rm -rf "$tmp"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

wait_for() {
  local timeout=$1 description=$2 deadline
  shift 2
  deadline=$((SECONDS + timeout))
  until "$@"; do
    if (( SECONDS >= deadline )); then
      cat "$tmp/readiness-error.log" "$tmp/metric-error.log" "$tmp/hey.log" >&2 2>/dev/null || true
      fail "Timed out waiting for $description"
    fi
    sleep 2
  done
}

ready_endpoints() {
  "${kube[@]}" get endpointslices -l "kubernetes.io/service-name=$scaler" -o json |
    jq -c '[.items[].endpoints[] | select(.conditions.ready and .targetRef.kind == "Pod") | .targetRef] | unique_by(.uid)'
}

podinfo_at_minimum() {
  "${kube[@]}" get deployment podinfo -o json |
    jq -e --argjson min "$minimum" '.spec.replicas == $min and .status.readyReplicas == $min' >/dev/null
}

all_scalers_ready() {
  local endpoints pod
  endpoints=$(ready_endpoints) || return 1
  jq -e --argjson replicas "$replicas" 'length == $replicas' <<< "$endpoints" >/dev/null || return 1
  # Operator-backed scaler Pods can pass their readiness probe before ingesting metrics.
  while IFS= read -r pod; do
    "${kube[@]}" get --raw "/api/v1/namespaces/$namespace/pods/$pod:$rest_port/proxy/readyz" \
      >/dev/null 2>"$tmp/readiness-error.log" || return 1
  done < <(jq -r '.[].name' <<< "$endpoints")
}

survivors_ready() {
  ready_endpoints | jq -e --argjson replicas "$replicas" --argjson victims "$victims" '
    length == ($replicas - ($victims | length)) and
    all(.[]; .name as $name | all($victims[]; .name != $name))
  ' >/dev/null
}

victims_stopped() {
  local pod
  while IFS= read -r pod; do
    "${kube[@]}" get pod "$pod" -o json | jq -e '
      .metadata.deletionTimestamp != null and (.status.phase == "Succeeded" or .status.phase == "Failed")
    ' >/dev/null || return 1
  done < <(jq -r '.[].name' <<< "$victims")
  survivors_ready
}

forward_ready() {
  kill -0 "$forward_pid" 2>/dev/null || fail "Port-forward failed: $(cat "$tmp/forward.log")"
  local_address=$(awk '/Forwarding from 127.0.0.1:/ {print $3}' "$tmp/forward.log")
  [[ -n $local_address ]]
}

replicas=$("${kube[@]}" get statefulset "$scaler" -o jsonpath='{.spec.replicas}')
[[ -z $expected_replicas || $replicas == "$expected_replicas" ]] ||
  fail "Expected $expected_replicas HA scaler replicas, found $replicas"
(( replicas >= 1 && (rounds == 0 || replicas >= 2) )) || fail 'Failover requires at least two HA scaler replicas'
rest_port=$("${kube[@]}" get service "$scaler" -o jsonpath='{.spec.ports[?(@.name=="rest-api")].port}')

printf 'Testing %s/%s with %s active-active replicas\n' "$context" "$namespace" "$replicas"
wait_for 180 'all HA scaler endpoints to serve metrics' all_scalers_ready
if [[ -n $manifest ]]; then
  # The scaler opens its gRPC listener on a one-second readiness poll.
  sleep 2
  "${kube[@]}" apply -f "$manifest"
fi
scaled=$("${kube[@]}" get scaledobject "$scaled_object" -o json)
minimum=$(jq -er '.spec.minReplicaCount' <<< "$scaled")
target=$(jq -er '.spec.triggers[0].metadata.targetValue | tonumber' <<< "$scaled")

find_external_metric() {
  local hpa
  hpa=$("${kube[@]}" get scaledobject "$scaled_object" -o jsonpath='{.status.hpaName}') && [[ -n $hpa ]] || return 1
  metric=$("${kube[@]}" get hpa "$hpa" -o json |
    jq -er '[.spec.metrics[] | select(.type == "External") | .external.metric.name] | select(length == 1) | .[0]')
}

wait_for 90 'KEDA to create the external metric HPA' find_external_metric
selector=$(jq -nr --arg so "$scaled_object" '"scaledobject.keda.sh/name=\($so)" | @uri')
metric_url="/apis/external.metrics.k8s.io/v1beta1/namespaces/$namespace/$metric?labelSelector=$selector"

external_metric() {
  "${kube[@]}" get --raw "$metric_url" |
    jq -er --arg metric "$metric" '.items[] | select(.metricName == $metric) | .value'
}

metric_available() {
  external_metric >/dev/null 2>"$tmp/metric-error.log"
}

above_target() {
  jq -ne --arg value "$1" --argjson target "$target" '
    ($value | if endswith("m") then (rtrimstr("m") | tonumber) / 1000 else tonumber end) > $target
  ' >/dev/null
}

podinfo_scaled_up() {
  local value
  kill -0 "$load_pid" 2>/dev/null || fail "Load generator stopped: $(cat "$tmp/hey.log")"
  # Newly scaled podinfo Pods can introduce warming series, so keep the last
  # above-target metric KEDA returned while the selected scaler Pods were down.
  if value=$(external_metric 2>"$tmp/metric-error.log") && above_target "$value"; then
    metric_value=$value
  fi
  ready=$("${kube[@]}" get deployment podinfo -o json | jq '.status.readyReplicas // 0') || return 1
  (( ready > minimum )) && [[ -n $metric_value ]] && survivors_ready
}

checks=$((rounds == 0 ? 1 : rounds))
for ((round = 1; round <= checks; round++)); do
  wait_for 180 'podinfo at minimum replicas' podinfo_at_minimum
  wait_for 180 'all HA endpoints to become ready' all_scalers_ready
  wait_for 60 'the KEDA external metric' metric_available

  "${kube[@]}" port-forward --address 127.0.0.1 service/podinfo :9898 >"$tmp/forward.log" 2>&1 &
  forward_pid=$!
  wait_for 20 'the podinfo port-forward' forward_ready
  # The first request creates a rate series. Let the Collector scrape it and
  # the scalers warm it up before starting the load.
  load_url="http://$local_address/delay/2"
  curl -fsS --max-time 10 "$load_url" >/dev/null
  sleep 6
  wait_for 120 'KEDA to report the warmed-up rate' metric_available
  wait_for 30 'podinfo at minimum replicas after warmup' podinfo_at_minimum

  if (( rounds > 0 )); then
    endpoints=$(ready_endpoints)
    count=$(jq 'length' <<< "$endpoints")
    (( count == replicas )) || fail 'Scaler endpoint count changed before deletion'
    victim_count=$((round % 2 == 0 && replicas > 2 ? 2 : 1))
    victims=$(jq -c --argjson start "$((RANDOM % count))" --argjson count "$victim_count" \
      '( .[$start:] + .[:$start] )[:$count]' <<< "$endpoints")
    victim_names=()
    # A terminating Pod keeps its StatefulSet name until the finalizer is removed.
    # Its containers still stop, so every selected replica stays down during load.
    while IFS= read -r pod; do
      victim_names+=("$pod")
      "${kube[@]}" patch pod "$pod" --type=strategic \
        -p "{\"metadata\":{\"finalizers\":[\"$finalizer\"]}}" >/dev/null
    done < <(jq -r '.[].name' <<< "$victims")
    printf 'Round %s/%s, deleting %s\n' "$round" "$rounds" "${victim_names[*]}"
    "${kube[@]}" delete pod "${victim_names[@]}" --grace-period=1 --wait=false
    wait_for 60 'the deleted scaler containers to stop and leave the Service' victims_stopped
  fi

  hey -z 120s -c 60 "$load_url" >"$tmp/hey.log" 2>&1 &
  load_pid=$!
  metric_value=
  wait_for 90 'an above-target KEDA metric and podinfo scale-up' podinfo_scaled_up
  available=$((replicas - $(jq 'length' <<< "$victims")))
  printf 'KEDA returned %s and podinfo scaled %s → %s ready replicas with %s/%s scaler replicas available\n' \
    "$metric_value" "$minimum" "$ready" "$available" "$replicas"

  stop_background
  if (( rounds > 0 )); then
    release_victims
    deleted=$victims
    victims='[]'
    wait_for 180 'the replacement scaler replicas to become ready' all_scalers_ready
    ready_endpoints | jq -e --argjson deleted "$deleted" '
      . as $ready | all($deleted[]; . as $old | any($ready[]; .name == $old.name and .uid != $old.uid))
    ' >/dev/null || fail 'Some deleted scaler Pods did not get a ready replacement'
  fi
  wait_for 180 'podinfo to scale back down' podinfo_at_minimum
  printf 'Check %s passed. All %s scaler replicas are ready and podinfo returned to %s replica(s).\n' \
    "$round" "$replicas" "$minimum"
done
