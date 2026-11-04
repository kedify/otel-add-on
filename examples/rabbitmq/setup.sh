#!/usr/bin/env bash
set -euo pipefail

DIR="${DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)}"
CLUSTER=rabbitmq
CONTEXT="k3d-${CLUSTER}"
KEDA_VERSION="${KEDA_VERSION:-v2.21.0-3}"
BUILD_IMAGES="${BUILD_IMAGES:-true}"
SCALER_IMAGE=ghcr.io/kedify/otel-add-on:promql
WORKLOAD_IMAGE=rabbitmq-demo:latest
export WORK_SECONDS="${WORK_SECONDS:-2}"
export QUEUE1_INTERVAL_SECONDS="${QUEUE1_INTERVAL_SECONDS:-5}"
export QUEUE1_TASKS_PER_BATCH="${QUEUE1_TASKS_PER_BATCH:-5}"
export QUEUE2_INTERVAL_SECONDS="${QUEUE2_INTERVAL_SECONDS:-5}"
export QUEUE2_TASKS_PER_BATCH="${QUEUE2_TASKS_PER_BATCH:-5}"

for command_name in k3d docker helm kubectl python3; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "Missing required command: ${command_name}" >&2
    exit 1
  fi
done

# Reject invalid timing/batch settings before building images or touching a cluster.
PYTHONDONTWRITEBYTECODE=1 PYTHONPATH="${DIR}/app" python3 - <<'PY'
from common import positive_integer, seconds
seconds("WORK_SECONDS", 2, allow_zero=True)
for queue in (1, 2):
    seconds(f"QUEUE{queue}_INTERVAL_SECONDS", 5)
    positive_integer(f"QUEUE{queue}_TASKS_PER_BATCH", 5)
PY
if [[ "${BUILD_IMAGES}" != true && "${BUILD_IMAGES}" != false ]]; then
  echo "BUILD_IMAGES must be true or false" >&2
  exit 1
fi

work_dir="$(mktemp -d "${TMPDIR:-/tmp}/rabbitmq-demo.XXXXXX")"
trap 'rm -rf "${work_dir}"' EXIT
docker info >/dev/null

if [[ "${BUILD_IMAGES}" == true ]]; then
  # Arithmetic functions are not in the chart's released v0.1.5 scaler image.
  git_commit="$(git -C "${DIR}/../.." rev-parse --short HEAD 2>/dev/null || printf unknown)"
  docker build --build-arg VERSION=promql --build-arg GIT_COMMIT="${git_commit}" \
    --tag "${SCALER_IMAGE}" "${DIR}/../.."
  docker build --tag "${WORKLOAD_IMAGE}" "${DIR}/app"
else
  docker image inspect "${SCALER_IMAGE}" "${WORKLOAD_IMAGE}" >/dev/null
fi

if k3d cluster list -o json | python3 -c \
  'import json,sys; sys.exit(0 if any(c["name"] == "rabbitmq" for c in json.load(sys.stdin)) else 1)'; then
  echo "Reusing cluster ${CLUSTER}"
  k3d cluster start "${CLUSTER}"
  k3d kubeconfig merge "${CLUSTER}" --kubeconfig-switch-context=false >/dev/null
else
  k3d cluster create "${CLUSTER}" --kubeconfig-switch-context=false --wait --timeout=180s
fi
k3d image import --mode direct --cluster "${CLUSTER}" "${SCALER_IMAGE}" "${WORKLOAD_IMAGE}"

kubectl() { command kubectl --context "${CONTEXT}" "$@"; }
helm() { command helm --kube-context "${CONTEXT}" "$@"; }

helm repo add kedify https://kedify.github.io/charts --force-update
helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts --force-update
helm repo update kedify open-telemetry
helm upgrade --install keda kedify/keda --namespace keda --create-namespace \
  --version "${KEDA_VERSION}" --wait --timeout=5m

kubectl create namespace rabbitmq --dry-run=client -o yaml | kubectl apply -f -
kubectl create configmap rabbitmq-config --namespace rabbitmq \
  --from-file="${DIR}/config/" --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f "${DIR}/rabbitmq.yaml"
kubectl rollout status --namespace rabbitmq deployment/rabbitmq --timeout=5m

# Resolve chart dependencies in a copy; do not modify files outside this example.
cp -R "${DIR}/../../helmchart/otel-add-on" "${work_dir}/otel-add-on"
helm dependency build "${work_dir}/otel-add-on" --skip-refresh
helm upgrade --install keda-otel-scaler "${work_dir}/otel-add-on" \
  --namespace keda --values "${DIR}/otel-scaler-values.yaml" --wait --timeout=5m

kubectl create configmap workload-config --namespace rabbitmq \
  --from-literal=WORK_SECONDS="${WORK_SECONDS}" \
  --from-literal=QUEUE1_INTERVAL_SECONDS="${QUEUE1_INTERVAL_SECONDS}" \
  --from-literal=QUEUE1_TASKS_PER_BATCH="${QUEUE1_TASKS_PER_BATCH}" \
  --from-literal=QUEUE2_INTERVAL_SECONDS="${QUEUE2_INTERVAL_SECONDS}" \
  --from-literal=QUEUE2_TASKS_PER_BATCH="${QUEUE2_TASKS_PER_BATCH}" \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f "${DIR}/workload.yaml"
# Also pick up rebuilt same-tag images and changed environment on repeat runs.
kubectl rollout restart --namespace keda deployment/keda-otel-scaler
kubectl rollout restart --namespace rabbitmq \
  deployment/rabbitmq-worker deployment/rabbitmq-producer-queue1 deployment/rabbitmq-producer-queue2
kubectl rollout status --namespace keda deployment/keda-otel-scaler --timeout=5m
for deployment in rabbitmq-worker rabbitmq-producer-queue1 rabbitmq-producer-queue2; do
  kubectl rollout status --namespace rabbitmq "deployment/${deployment}" --timeout=5m
done
kubectl apply -f "${DIR}/rabbitmq-so.yaml"
kubectl wait --namespace rabbitmq --for=condition=Ready scaledobject/rabbitmq-workers --timeout=120s

cat <<USAGE

RabbitMQ demo is ready. The current kubeconfig context was left unchanged.

# Follow task processing and worker scaling:
kubectl --context=${CONTEXT} -n rabbitmq logs -l app=rabbitmq-worker --prefix --tail=20 -f --max-log-requests=10
kubectl --context=${CONTEXT} -n rabbitmq get deployments --watch
kubectl --context=${CONTEXT} -n rabbitmq get hpa,scaledobjects

# Each additional producer independently creates its configured batch every interval:
kubectl --context=${CONTEXT} -n rabbitmq scale deployment/rabbitmq-producer-queue1 --replicas=3
kubectl --context=${CONTEXT} -n rabbitmq scale deployment/rabbitmq-producer-queue2 --replicas=2

# RabbitMQ management UI: http://localhost:15672 (demo / demo):
kubectl --context=${CONTEXT} -n rabbitmq port-forward service/rabbitmq 15672:15672

# Scaler query API (use another local port if 9090 is already occupied):
kubectl --context=${CONTEXT} -n keda port-forward service/keda-otel-scaler 19090:9090

# Cleanup:
k3d cluster delete ${CLUSTER}
USAGE
