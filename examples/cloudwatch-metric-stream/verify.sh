#!/usr/bin/env bash

set -euo pipefail

NAMESPACE="cloudwatch-metric-stream"
REQUEST_COUNT="${REQUEST_COUNT:-200}"
CONCURRENCY="${CONCURRENCY:-20}"
STREAM_READY_TIMEOUT_SECONDS="${STREAM_READY_TIMEOUT_SECONDS:-900}"
VERIFY_TIMEOUT_SECONDS="${VERIFY_TIMEOUT_SECONDS:-900}"
BASELINE_TIMEOUT_SECONDS="${BASELINE_TIMEOUT_SECONDS:-600}"
REST_PORT="${REST_PORT:-19090}"
SCALED_OBJECT_NAME="cloudwatch-metric-demo"
HPA_NAME="keda-hpa-cloudwatch-metric-demo"

for command_name in kubectl curl jq base64 awk xargs seq date; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "Missing required command: ${command_name}" >&2
    exit 1
  fi
done

KUBE_CONTEXT="${KUBE_CONTEXT:-$(command kubectl config current-context)}"
kubectl() {
  command kubectl --context "${KUBE_CONTEXT}" "$@"
}

workload_hostname="$(
  kubectl get service cloudwatch-metric-demo -n "${NAMESPACE}" \
    -o jsonpath='{.status.loadBalancer.ingress[0].hostname}'
)"
load_balancer_dimension="$(
  kubectl get secret cloudwatch-metric-stream -n "${NAMESPACE}" \
    -o jsonpath='{.data.workload-load-balancer-dimension}' | base64 --decode
)"

if [[ -z "${workload_hostname}" || -z "${load_balancer_dimension}" ]]; then
  echo "The example is not set up completely; run setup.sh first." >&2
  exit 1
fi

echo "Waiting for http://${workload_hostname} to accept traffic."
ready=false
for attempt in $(seq 1 60); do
  if curl --connect-timeout 5 --max-time 10 -fsS -o /dev/null \
    "http://${workload_hostname}/" 2>/dev/null; then
    ready=true
    break
  fi
  sleep 5
done
if [[ "${ready}" != "true" ]]; then
  echo "The workload NLB did not become ready." >&2
  exit 1
fi

port_forward_log="$(mktemp)"
kubectl port-forward --address 127.0.0.1 \
  -n "${NAMESPACE}" service/cloudwatch-otel-scaler \
  "${REST_PORT}:9090" >"${port_forward_log}" 2>&1 &
port_forward_pid=$!
scaling_cap_active=false
original_max_replica_count=""

restore_scaling_cap() {
  local patch_payload

  if [[ "${scaling_cap_active}" != "true" ]]; then
    return 0
  fi

  patch_payload="$(
    jq -cn --argjson max_replicas "${original_max_replica_count}" \
      '{spec: {maxReplicaCount: $max_replicas}}'
  )"
  if kubectl patch scaledobject "${SCALED_OBJECT_NAME}" -n "${NAMESPACE}" \
    --type=merge --patch "${patch_payload}" >/dev/null; then
    scaling_cap_active=false
    return 0
  fi
  return 1
}

cleanup_verification() {
  local status=$?

  if [[ "${scaling_cap_active}" == "true" ]]; then
    echo "Restoring the ScaledObject's original maximum replica count." >&2
    if ! restore_scaling_cap; then
      echo "Warning: failed to restore maxReplicaCount=${original_max_replica_count}." >&2
    fi
  fi
  kill "${port_forward_pid}" >/dev/null 2>&1 || true
  rm -f "${port_forward_log}"
  trap - EXIT
  exit "${status}"
}
trap cleanup_verification EXIT

port_forward_ready=false
for attempt in $(seq 1 30); do
  if curl -fsS -o /dev/null \
    "http://127.0.0.1:${REST_PORT}/swagger/index.html" 2>/dev/null; then
    port_forward_ready=true
    break
  fi
  if ! kill -0 "${port_forward_pid}" >/dev/null 2>&1; then
    cat "${port_forward_log}" >&2
    exit 1
  fi
  sleep 1
done
if [[ "${port_forward_ready}" != "true" ]]; then
  echo "Timed out waiting for the scaler REST API port-forward." >&2
  cat "${port_forward_log}" >&2
  exit 1
fi

metric_name="amazonaws_com_AWS_NetworkELB_NewFlowCount_sum"
metric_query="${metric_name}{LoadBalancer=${load_balancer_dimension}}"
query_payload="$(
  jq -n --arg query "${metric_query}" \
    '{operationOverTime: "last_one", query: $query}'
)"

get_current_fresh_datapoint() {
  local metric_dump

  metric_dump="$(
    curl -fsS "http://127.0.0.1:${REST_PORT}/memstore/data" 2>/dev/null || true
  )"
  jq -c \
    --arg metric_name "${metric_name}" \
    --arg load_balancer "${load_balancer_dimension}" \
    --argjson traffic_window_start "${traffic_window_start}" \
    --argjson scale_target_value "${scale_target_value}" \
    '[.[$metric_name][]?
      | select(.labels.LoadBalancer == $load_balancer)
      | .data[-1]?][0]?
     | select(.time > $traffic_window_start and .value > $scale_target_value)' \
    <<<"${metric_dump}" 2>/dev/null || true
}

echo "Waiting up to ${STREAM_READY_TIMEOUT_SECONDS}s for the metric stream's first datapoint."
deadline=$((SECONDS + STREAM_READY_TIMEOUT_SECONDS))
stream_ready=false
while ((SECONDS < deadline)); do
  response="$(
    curl -fsS -X POST "http://127.0.0.1:${REST_PORT}/memstore/query" \
      -H 'accept: application/json' \
      -H 'Content-Type: application/json' \
      --data "${query_payload}" 2>/dev/null || true
  )"
  if jq -e '.ok == true and ((.error // "") == "")' <<<"${response}" >/dev/null 2>&1; then
    initial_metric_value="$(jq -r '.value' <<<"${response}")"
    echo "Metric stream is delivering; initial NewFlowCount_sum=${initial_metric_value}."
    stream_ready=true
    break
  fi
  sleep 10
done

if [[ "${stream_ready}" != "true" ]]; then
  echo "No CloudWatch datapoint reached the scaler before the stream-ready timeout." >&2
  kubectl logs -n "${NAMESPACE}" deployment/cloudwatch-firehose-receiver --tail=100 >&2 || true
  exit 1
fi

min_replica_count="$(
  kubectl get scaledobject "${SCALED_OBJECT_NAME}" -n "${NAMESPACE}" \
    -o jsonpath='{.spec.minReplicaCount}'
)"
original_max_replica_count="$(
  kubectl get scaledobject "${SCALED_OBJECT_NAME}" -n "${NAMESPACE}" \
    -o jsonpath='{.spec.maxReplicaCount}'
)"
scale_target_value="$(
  kubectl get scaledobject "${SCALED_OBJECT_NAME}" -n "${NAMESPACE}" \
    -o jsonpath='{.spec.triggers[0].metadata.targetValue}'
)"

if [[ ! "${min_replica_count}" =~ ^[0-9]+$ || \
  ! "${original_max_replica_count}" =~ ^[0-9]+$ || \
  "${original_max_replica_count}" -le "${min_replica_count}" ]]; then
  echo "Verification requires numeric replica bounds with maxReplicaCount greater than minReplicaCount." >&2
  exit 1
fi

# Keep delayed data from an earlier run from scaling the Deployment while this
# run waits for and identifies its own source-timestamped datapoint. The EXIT
# trap restores this value if verification is interrupted.
cap_payload="$(
  jq -cn --argjson min_replicas "${min_replica_count}" \
    '{spec: {maxReplicaCount: $min_replicas}}'
)"
scaling_cap_active=true
kubectl patch scaledobject "${SCALED_OBJECT_NAME}" -n "${NAMESPACE}" \
  --type=merge --patch "${cap_payload}" >/dev/null

# Remove the warm-up datapoint so KEDA can return to its minimum replica
# baseline before this run generates traffic.
curl -fsS -X POST "http://127.0.0.1:${REST_PORT}/memstore/reset" >/dev/null

echo "Waiting up to ${BASELINE_TIMEOUT_SECONDS}s for capped desired replicas to return to ${min_replica_count}."
deadline=$((SECONDS + BASELINE_TIMEOUT_SECONDS))
baseline_ready=false
baseline_replicas=""
hpa_max_replicas=""
while ((SECONDS < deadline)); do
  baseline_replicas="$(
    kubectl get deployment cloudwatch-metric-demo -n "${NAMESPACE}" \
      -o jsonpath='{.spec.replicas}'
  )"
  hpa_max_replicas="$(
    kubectl get hpa "${HPA_NAME}" -n "${NAMESPACE}" \
      -o jsonpath='{.spec.maxReplicas}' 2>/dev/null || true
  )"
  if [[ "${hpa_max_replicas}" == "${min_replica_count}" ]] && \
    ((baseline_replicas == min_replica_count)); then
    baseline_ready=true
    break
  fi
  sleep 5
done
if [[ "${baseline_ready}" != "true" ]]; then
  echo "The Deployment and HPA did not settle at the capped minimum replica baseline." >&2
  kubectl get scaledobject,hpa,deployment -n "${NAMESPACE}" >&2 || true
  exit 1
fi

# Start in a new CloudWatch one-minute interval. This gives the traffic a
# source-time watermark later than every datapoint that could have been
# produced by a previous run, even if Firehose delivers an old batch late.
now_epoch="$(date +%s)"
traffic_window_start=$((((now_epoch / 60) + 1) * 60))
echo "Waiting for a fresh CloudWatch interval beginning at ${traffic_window_start}."
while (( $(date +%s) < traffic_window_start )); do
  sleep 1
done

# Clear anything that arrived while KEDA was returning to baseline. Scaling
# remains capped until a datapoint from the new interval is current in the
# scaler, so delayed data cannot create the transition asserted below.
curl -fsS -X POST "http://127.0.0.1:${REST_PORT}/memstore/reset" >/dev/null

echo "Opening ${REQUEST_COUNT} connections with concurrency ${CONCURRENCY}."
seq 1 "${REQUEST_COUNT}" | \
  xargs -P "${CONCURRENCY}" -I '{}' \
    curl --no-keepalive --connect-timeout 5 --max-time 15 -fsS -o /dev/null \
      -H 'Connection: close' "http://${workload_hostname}/?request={}"

echo "Waiting up to ${VERIFY_TIMEOUT_SECONDS}s for ${metric_query} to exceed ${scale_target_value}."
deadline=$((SECONDS + VERIFY_TIMEOUT_SECONDS))
metric_value=""
metric_timestamp=""
while ((SECONDS < deadline)); do
  curl -fsS -X POST "http://127.0.0.1:${REST_PORT}/memstore/query" \
    -H 'accept: application/json' \
    -H 'Content-Type: application/json' \
    --data "${query_payload}" >/dev/null 2>&1 || true
  # The query keeps the metric subscribed in lazy-store configurations. Read
  # the current last_one source datapoint as well. CloudWatch puts the interval
  # end in timeUnixNano, so a strict comparison excludes the preceding interval
  # whose end is exactly traffic_window_start.
  fresh_datapoint="$(get_current_fresh_datapoint)"
  metric_value="$(jq -r '.value // empty' <<<"${fresh_datapoint}" 2>/dev/null || true)"
  metric_timestamp="$(jq -r '.time // empty' <<<"${fresh_datapoint}" 2>/dev/null || true)"
  if [[ -n "${metric_value}" ]] && awk "BEGIN { exit !(${metric_value} > ${scale_target_value}) }"; then
    echo "Scaler received fresh NewFlowCount_sum=${metric_value} at source timestamp ${metric_timestamp}."
    break
  fi
  sleep 10
done

if [[ -z "${metric_value}" ]] || ! awk "BEGIN { exit !(${metric_value} > ${scale_target_value}) }"; then
  echo "No CloudWatch metric from this run's source interval exceeded the scaling target before the timeout." >&2
  kubectl logs -n "${NAMESPACE}" deployment/cloudwatch-firehose-receiver --tail=100 >&2 || true
  exit 1
fi

# The current scaler value is now provably from this run while scaling is still
# capped. Restore the original bound; KEDA can only create the asserted
# transition after this point.
restore_scaling_cap

echo "Waiting for KEDA to scale the workload."
deadline=$((SECONDS + 180))
while ((SECONDS < deadline)); do
  replicas="$(
    kubectl get deployment cloudwatch-metric-demo -n "${NAMESPACE}" \
      -o jsonpath='{.spec.replicas}'
  )"
  if ((replicas > baseline_replicas)); then
    current_datapoint="$(get_current_fresh_datapoint)"
    if [[ -z "${current_datapoint}" ]]; then
      echo "Desired replicas increased, but a delayed datapoint replaced this run's scaler value." >&2
      kubectl get scaledobject,hpa,deployment -n "${NAMESPACE}" >&2 || true
      exit 1
    fi
    kubectl get scaledobject,hpa,deployment,pod -n "${NAMESPACE}"
    echo "End-to-end verification passed: desired replicas=${replicas}."
    exit 0
  fi
  sleep 5
done

echo "The metric arrived, but desired replicas did not increase above ${baseline_replicas}." >&2
kubectl get scaledobject,hpa,deployment -n "${NAMESPACE}" >&2 || true
exit 1
