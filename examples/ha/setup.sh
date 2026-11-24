#!/usr/bin/env bash
set -euo pipefail

dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
root=$(cd "$dir/../.." && pwd)
cluster=${CLUSTER:-otel-scaler-ha}
context="k3d-$cluster"
namespace=ha-enabled
release=keda-otel-scaler
image_tag=${OTEL_SCALER_VERSION:-}
image_repository=ghcr.io/kedify/otel-add-on
pull_policy=Always

for command_name in docker k3d helm kubectl jq curl hey; do
  if ! command -v "$command_name" >/dev/null; then
    printf 'Missing required command: %s\n' "$command_name" >&2
    exit 1
  fi
done
docker info >/dev/null

work_dir=$(mktemp -d)
trap 'rm -rf "$work_dir"' EXIT

if [[ -z $image_tag ]]; then
  # Build for the local Docker architecture and use a new tag when the image changes.
  commit=$(git -C "$root" rev-parse --short HEAD 2>/dev/null || printf unknown)
  docker build --build-arg VERSION=ha-local --build-arg "GIT_COMMIT=$commit" \
    --iidfile "$work_dir/image-id" "$root"
  image_id=$(cat "$work_dir/image-id")
  image_repository=otel-add-on
  image_tag="ha-local-${image_id#sha256:}"
  pull_policy=Never
  docker image tag "$image_id" "$image_repository:$image_tag"
fi

helm repo add kedify https://kedify.github.io/charts --force-update
helm repo add podinfo https://stefanprodan.github.io/podinfo --force-update
helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts --force-update
cp -R "$root/helmchart/otel-add-on" "$work_dir/otel-add-on"
helm dependency build "$work_dir/otel-add-on" --skip-refresh

if k3d cluster list -o json | jq -e --arg name "$cluster" 'any(.[]; .name == $name)' >/dev/null; then
  printf 'Reusing cluster %s\n' "$cluster"
  k3d cluster start "$cluster" --timeout 3m
else
  # KEDA serves the external metrics used by this demo.
  k3d cluster create "$cluster" --no-lb --timeout 3m \
    --kubeconfig-update-default=false --kubeconfig-switch-context=false \
    --k3s-arg '--disable=traefik,servicelb,metrics-server@server:*'
fi
k3d kubeconfig merge "$cluster" --kubeconfig-merge-default --kubeconfig-switch-context=false >/dev/null
if [[ $pull_policy == Never ]]; then
  k3d image import --mode direct --cluster "$cluster" "$image_repository:$image_tag"
fi

helm upgrade --install keda kedify/keda --kube-context "$context" \
  --namespace keda --create-namespace --version v2.17.1-0 \
  --set webhooks.enabled=false --wait --timeout 5m --hide-notes
kubectl --context "$context" wait --for=condition=Established --timeout=60s crd/scaledobjects.keda.sh
kubectl --context "$context" wait --for=condition=Available --timeout=90s apiservice/v1beta1.external.metrics.k8s.io

helm upgrade --install podinfo podinfo/podinfo --kube-context "$context" \
  --namespace "$namespace" --create-namespace --version 6.9.0 \
  -f "$dir/../metric-pull/podinfo-values.yaml" --set service.type=ClusterIP,podAnnotations=null \
  --wait --timeout 5m --hide-notes
helm upgrade --install "$release" "$work_dir/otel-add-on" --kube-context "$context" \
  --namespace "$namespace" -f "$root/e2e-tests/testdata/scaler-ha-values.yaml" \
  --set otelCollector.enabled=true \
  --set-string "otelCollector.alternateConfig.exporters.otlp.endpoint=$release.$namespace.svc:4317" \
  --set-string "image.repository=$image_repository,image.tag=$image_tag,image.pullPolicy=$pull_policy" \
  --wait --timeout 5m --hide-notes

# The scaler opens its KEDA listener on a one-second poll after metric history is ready.
sleep 2
sed "s/keda-otel-scaler\.keda\.svc/$release.$namespace.svc/" "$dir/../metric-pull/podinfo-so.yaml" |
  kubectl --context "$context" --namespace "$namespace" apply -f -
kubectl --context "$context" --namespace "$namespace" wait --for=condition=Ready \
  --timeout=120s scaledobject/podinfo-pull-example

printf '\nRun the failover test with\n%q %q\n' "$dir/test-failover.sh" "$context"
