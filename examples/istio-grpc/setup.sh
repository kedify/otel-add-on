#!/usr/bin/env bash
set -euo pipefail

DIR="${DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)}"
CLUSTER=istio-grpc
CONTEXT="k3d-${CLUSTER}"
K3S_IMAGE="${K3S_IMAGE:-rancher/k3s:v1.37.1-k3s1}"
ISTIO_VERSION="${ISTIO_VERSION:-1.31.1}"
export MESSAGE_INTERVAL_SECONDS="${MESSAGE_INTERVAL_SECONDS:-5±1}"
export MESSAGE_COUNT="${MESSAGE_COUNT:-30±5}"
export CONSUMER_START_DELAY_SECONDS="${CONSUMER_START_DELAY_SECONDS:-5±2}"
export PRODUCER_CAPACITY="${PRODUCER_CAPACITY:-3}"
export PI_INITIAL_DIGITS="${PI_INITIAL_DIGITS:-100}"
export PI_DIGITS_STEP="${PI_DIGITS_STEP:-100}"
export PRODUCER_ADDRESS="${PRODUCER_ADDRESS:-producer:50051}"

for command_name in k3d docker helm kubectl curl python3; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "Missing required command: ${command_name}" >&2
    exit 1
  fi
done

# Validate configuration before creating or changing any cluster resources.
scale_out_consumers="$(PYTHONDONTWRITEBYTECODE=1 PYTHONPATH="${DIR}/app" python3 - <<'PY'
from common import ProducerConfig, consumer_delay
config = ProducerConfig.from_env()
consumer_delay()
print(config.capacity + 1)
PY
)"

work_dir="$(mktemp -d "${TMPDIR:-/tmp}/istio-grpc.XXXXXX")"
trap 'rm -rf "${work_dir}"' EXIT

k3d version
docker info >/dev/null

if k3d cluster list -o json | python3 -c \
  'import json,sys; sys.exit(0 if any(c["name"] == "istio-grpc" for c in json.load(sys.stdin)) else 1)'; then
  echo "Reusing cluster ${CLUSTER}"
  k3d kubeconfig merge "${CLUSTER}" --kubeconfig-switch-context=false >/dev/null
else
  k3d cluster create "${CLUSTER}" --image="${K3S_IMAGE}" \
    --kubeconfig-switch-context=false --wait --timeout=180s \
    --k3s-arg '--disable=traefik@server:0'
fi

kubectl() { command kubectl --context "${CONTEXT}" "$@"; }
helm() { command helm --kube-context "${CONTEXT}" "$@"; }

# Reusing an older demo must not silently bypass the selected Kubernetes version.
actual_version="$(kubectl version -o json | python3 -c \
  'import json,sys; print(json.load(sys.stdin)["serverVersion"]["gitVersion"])')"
expected_version="${K3S_IMAGE##*:}"
if [[ "${actual_version//+/-}" != "${expected_version//+/-}" ]]; then
  echo "Cluster ${CLUSTER} runs ${actual_version}; requested ${expected_version}." >&2
  echo "Delete it explicitly with: k3d cluster delete ${CLUSTER}, then rerun setup." >&2
  exit 1
fi

helm repo add istio https://blob.istio.io/istio-release/charts --force-update
helm repo add kedify https://kedify.github.io/charts --force-update
helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts --force-update
helm repo update istio kedify open-telemetry
helm upgrade --install istio-base istio/base --namespace istio-system \
  --create-namespace --version "${ISTIO_VERSION}" --wait --timeout=5m
helm upgrade --install istiod istio/istiod --namespace istio-system \
  --version "${ISTIO_VERSION}" --wait --timeout=5m

if [[ -z "${KEDA_VERSION:-}" ]]; then
  KEDA_VERSION="$(curl --fail --silent --show-error --location --retry 3 \
    https://api.github.com/repos/kedify/charts/releases | python3 -c \
    'import json,sys; print(next((r["tag_name"][5:] for r in json.load(sys.stdin) if not r["draft"] and not r["prerelease"] and r["tag_name"].startswith("keda/")), ""))')"
fi
KEDA_VERSION="${KEDA_VERSION:-v2.17.1-0}"
helm upgrade --install keda kedify/keda --namespace keda --create-namespace \
  --version "${KEDA_VERSION}" --wait --timeout=5m

# Build a temporary chart copy so dependency downloads do not alter the checkout.
cp -R "${DIR}/../../helmchart/otel-add-on" "${work_dir}/otel-add-on"
helm dependency build "${work_dir}/otel-add-on" --skip-refresh
helm upgrade --install keda-otel-scaler "${work_dir}/otel-add-on" \
  --namespace keda --values "${DIR}/otel-scaler-values.yaml" --wait --timeout=5m

kubectl create namespace istio-grpc --dry-run=client -o yaml | kubectl apply -f -
kubectl label namespace istio-grpc istio-injection=enabled --overwrite
kubectl create configmap grpc-app --namespace istio-grpc \
  --from-file="${DIR}/app/" --dry-run=client -o yaml | kubectl apply -f -
kubectl create configmap workload-config --namespace istio-grpc \
  --from-literal=MESSAGE_INTERVAL_SECONDS="${MESSAGE_INTERVAL_SECONDS}" \
  --from-literal=MESSAGE_COUNT="${MESSAGE_COUNT}" \
  --from-literal=CONSUMER_START_DELAY_SECONDS="${CONSUMER_START_DELAY_SECONDS}" \
  --from-literal=PRODUCER_CAPACITY="${PRODUCER_CAPACITY}" \
  --from-literal=PI_INITIAL_DIGITS="${PI_INITIAL_DIGITS}" \
  --from-literal=PI_DIGITS_STEP="${PI_DIGITS_STEP}" \
  --from-literal=PRODUCER_ADDRESS="${PRODUCER_ADDRESS}" \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f "${DIR}/istio.yaml" -f "${DIR}/workload.yaml"
kubectl rollout status --namespace istio-grpc deployment/producer --timeout=5m
kubectl rollout status --namespace istio-grpc deployment/consumer --timeout=5m
sed "s/\${PRODUCER_CAPACITY}/${PRODUCER_CAPACITY}/g" "${DIR}/scaledobject.yaml" | kubectl apply -f -
kubectl wait --namespace istio-grpc --for=condition=Ready scaledobject/producer --timeout=120s

cat <<USAGE

Demo ready: Kubernetes ${actual_version}, Istio ${ISTIO_VERSION}, capacity ${PRODUCER_CAPACITY}.
The current kubeconfig context was left unchanged. Use --context=${CONTEXT}:

# ${scale_out_consumers} active consumers with capacity=${PRODUCER_CAPACITY} should trigger a second producer.
kubectl --context=${CONTEXT} -n istio-grpc scale deployment/consumer --replicas=${scale_out_consumers}
kubectl --context=${CONTEXT} -n istio-grpc get deployments --watch
kubectl --context=${CONTEXT} -n istio-grpc get hpa,scaledobjects

# More load (ten active streams with the default capacity=3 request four producers).
kubectl --context=${CONTEXT} -n istio-grpc scale deployment/consumer --replicas=10
kubectl --context=${CONTEXT} -n istio-grpc logs -l app=consumer -c consumer --prefix --tail=10
kubectl --context=${CONTEXT} -n istio-grpc logs -l app=producer -c producer --prefix --tail=10

# Inspect the metric via the scaler's REST API (see README for the query).
kubectl --context=${CONTEXT} -n keda port-forward service/keda-otel-scaler 9090:9090

# Remove load; producers eventually return to one replica.
kubectl --context=${CONTEXT} -n istio-grpc scale deployment/consumer --replicas=0

# Cleanup:
k3d cluster delete ${CLUSTER}
USAGE
