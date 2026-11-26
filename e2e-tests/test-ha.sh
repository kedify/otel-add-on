#!/usr/bin/env bash
set -euo pipefail

: "${OTEL_SCALER_VERSION:?Set OTEL_SCALER_VERSION to the scaler image tag to test}"

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
chart="$root/helmchart/otel-add-on"
example="$root/examples/metric-pull"
release=keda-otel-scaler
tmp=$(mktemp -d)
cluster="otel-ha-e2e-$(tr '[:upper:]' '[:lower:]' <<< "${tmp##*.}")"
context="k3d-$cluster"
export KUBECONFIG="$tmp/kubeconfig"
export HELM_CONFIG_HOME="$tmp/helm/config" HELM_CACHE_HOME="$tmp/helm/cache"

cleanup() {
  local status=$?
  if (( status != 0 )) && [[ -s $KUBECONFIG ]]; then
    kubectl --request-timeout=10s get pods,scaledobjects,hpa -A -o wide || true
    for namespace in keda ha ha-operator; do
      kubectl --request-timeout=10s -n "$namespace" get events --sort-by=.lastTimestamp || true
    done
  fi
  k3d cluster delete "$cluster" || true
  rm -rf "$tmp"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# Use KEDA as the cluster's metrics source for autoscaling.
k3d cluster create "$cluster" --no-lb --timeout 3m \
  --kubeconfig-update-default=false --kubeconfig-switch-context=false \
  --k3s-arg '--disable=traefik,servicelb,metrics-server@server:*'
scaler_image_args=(--set-string "image.tag=$OTEL_SCALER_VERSION")
if [[ -n ${OTEL_SCALER_ARCHIVE:-} ]]; then
  k3d image import "$OTEL_SCALER_ARCHIVE" --cluster "$cluster" --mode direct
  scaler_image_args+=(--set image.pullPolicy=Never)
fi
k3d kubeconfig get "$cluster" >"$KUBECONFIG"

helm repo add kedify https://kedify.github.io/charts
helm repo add podinfo https://stefanprodan.github.io/podinfo
helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts
helm dependency build "$chart"

helm install keda kedify/keda --namespace keda --create-namespace --version v2.17.1-0 \
  --set webhooks.enabled=false --wait --timeout 5m --hide-notes

install_podinfo() {
  local namespace=$1
  helm install podinfo podinfo/podinfo --namespace "$namespace" --create-namespace --version 6.9.0 \
    -f "$example/podinfo-values.yaml" --set service.type=ClusterIP --wait --timeout 5m --hide-notes
}

test_failover() {
  local namespace=$1
  local manifest="$tmp/podinfo-so.yaml"
  shift
  helm install "$release" "$chart" --namespace "$namespace" \
    -f "$root/e2e-tests/testdata/scaler-ha-values.yaml" \
    "${scaler_image_args[@]}" --wait --timeout 5m --hide-notes "$@"
  sed "s/keda-otel-scaler\.keda\.svc/$release.$namespace.svc/" "$example/podinfo-so.yaml" >"$manifest"
  bash "$root/examples/ha/test-failover.sh" "$context" 2 "$namespace" 3 "$manifest"
}

install_podinfo ha
test_failover ha --set otelCollector.enabled=true \
  --set otelCollector.alternateConfig.exporters.otlp.endpoint=keda-otel-scaler.ha.svc:4317

install_podinfo ha-operator
test_failover ha-operator --set otelOperator.enabled=true \
  --set 'otelOperatorCrs[0].enabled=true,otelOperatorCrs[0].name=otel-add-on'

for replicas in 1 4; do
  helm upgrade "$release" "$chart" --namespace ha-operator --reuse-values \
    --set "settings.ha.replicas=$replicas" --wait --timeout 5m --hide-notes
  bash "$root/examples/ha/test-failover.sh" "$context" 0 ha-operator "$replicas"
done
