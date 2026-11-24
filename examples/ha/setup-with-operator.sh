#!/usr/bin/env bash
set -euo pipefail

# KUBE_CONTEXT defaults to the current context. Set START_K3D=true to use k3d-$CLUSTER.
# OTEL_SCALER_VERSION selects a registry tag. A k3d run builds locally when it is unset.
dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
root=$(cd "$dir/../.." && pwd)
start_k3d=${START_K3D:-false}
cluster=${CLUSTER:-otel-scaler-ha}
context=${KUBE_CONTEXT:-}
namespace=ha-operator
release=keda-otel-scaler
collector=otel-add-on
image_repository=ghcr.io/kedify/otel-add-on
image_tag=${OTEL_SCALER_VERSION:-}
pull_policy=Always

commands=(helm kubectl jq)
case "$start_k3d" in
  true) commands+=(docker k3d) ;;
  false) ;;
  *) printf 'START_K3D must be true or false\n' >&2; exit 1 ;;
esac
for command_name in "${commands[@]}"; do
  if ! command -v "$command_name" >/dev/null; then
    printf 'Missing required command: %s\n' "$command_name" >&2
    exit 1
  fi
done

work_dir=$(mktemp -d)
trap 'rm -rf "$work_dir"' EXIT

if [[ $start_k3d == true ]]; then
  if [[ -n $context && $context != "k3d-$cluster" ]]; then
    printf 'START_K3D=true requires KUBE_CONTEXT to match k3d-%s\n' "$cluster" >&2
    exit 1
  fi
  context="k3d-$cluster"
  if [[ -z $image_tag ]]; then
    commit=$(git -C "$root" rev-parse --short HEAD 2>/dev/null || printf unknown)
    docker build --build-arg VERSION=ha-local --build-arg "GIT_COMMIT=$commit" \
      --iidfile "$work_dir/image-id" "$root"
    image_id=$(cat "$work_dir/image-id")
    image_repository=otel-add-on
    image_tag="ha-local-${image_id#sha256:}"
    pull_policy=Never
    docker image tag "$image_id" "$image_repository:$image_tag"
  fi
  if k3d cluster list -o json | jq -e --arg name "$cluster" 'any(.[]?; .name == $name)' >/dev/null; then
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
else
  context=${context:-$(kubectl config current-context)}
  image_tag=${image_tag:-ha}
fi

printf 'Setting up %s in context %s\n' "$namespace" "$context"
kube=(kubectl --context "$context")
deploy=(helm upgrade --install --kube-context "$context" --wait --timeout 5m --hide-notes)

keda_crd=$("${kube[@]}" get crd scaledobjects.keda.sh --ignore-not-found -o name)
if [[ -z $keda_crd ]]; then
  "${deploy[@]}" keda keda --repo https://kedify.github.io/charts \
    --namespace keda --create-namespace --version v2.17.1-0 --set webhooks.enabled=false
fi
"${kube[@]}" wait --for=condition=Established --timeout=60s crd/scaledobjects.keda.sh
"${kube[@]}" wait --for=condition=Available --timeout=90s apiservice/v1beta1.external.metrics.k8s.io

# Reuse a shared Operator when one already manages Collectors on this cluster.
operators=$("${kube[@]}" get deployments -A \
  -l app.kubernetes.io/part-of=opentelemetry-operator,app.kubernetes.io/component=controller-manager -o json)
operator_enabled=true
if jq -e --arg ns "$namespace" --arg release "$release" 'any(.items[];
  .metadata.namespace != $ns or .metadata.annotations["meta.helm.sh/release-name"] != $release
)' <<< "$operators" >/dev/null; then
  operator_enabled=false
  printf 'Reusing the cluster OpenTelemetry Operator\n'
  "${kube[@]}" wait --for=condition=Established --timeout=60s crd/opentelemetrycollectors.opentelemetry.io
fi

helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts --force-update
cp -R "$root/helmchart/otel-add-on" "$work_dir/otel-add-on"
helm dependency build "$work_dir/otel-add-on" --skip-refresh

"${deploy[@]}" podinfo podinfo --repo https://stefanprodan.github.io/podinfo \
  --namespace "$namespace" --create-namespace --version 6.9.0 \
  -f "$dir/../metric-pull/podinfo-values.yaml" --set service.type=ClusterIP,podAnnotations=null

# The chart's hook creates the Collector CR and gives each HA scaler its own OTLP exporter.
"${deploy[@]}" "$release" "$work_dir/otel-add-on" --namespace "$namespace" \
  -f "$root/e2e-tests/testdata/scaler-ha-values.yaml" \
  --set "otelOperator.enabled=$operator_enabled" \
  --set "otelOperatorCrs[0].enabled=true,otelOperatorCrs[0].name=$collector" \
  --set-string "image.repository=$image_repository,image.tag=$image_tag,image.pullPolicy=$pull_policy"
deadline=$((SECONDS + 120))
while :; do
  collector_deployment=$("${kube[@]}" --namespace "$namespace" get "deployment/$collector-collector" --ignore-not-found -o name)
  [[ -z $collector_deployment ]] || break
  if (( SECONDS >= deadline )); then
    printf 'Timed out waiting for the Operator to create deployment/%s-collector\n' "$collector" >&2
    exit 1
  fi
  sleep 2
done
"${kube[@]}" --namespace "$namespace" rollout status "deployment/$collector-collector" --timeout=180s

# The bootstrap probe can pass before a replica has warmed up its metrics.
replicas=$("${kube[@]}" --namespace "$namespace" get "statefulset/$release" -o jsonpath='{.spec.replicas}')
deadline=$((SECONDS + 180))
for ((ordinal = 0; ordinal < replicas; ordinal++)); do
  pod="$release-$ordinal"
  probe="/api/v1/namespaces/$namespace/pods/$pod:9090/proxy/readyz"
  until response=$("${kube[@]}" --request-timeout=8s get --raw "$probe" 2>&1); do
    if (( SECONDS >= deadline )); then
      printf 'Timed out waiting for %s readiness\n%s\n' "$pod" "$response" >&2
      exit 1
    fi
    sleep 2
  done
done
# The KEDA listener polls readiness once per second.
sleep 2

sed "s/keda-otel-scaler\.keda\.svc/$release.$namespace.svc/" "$dir/../metric-pull/podinfo-so.yaml" |
  "${kube[@]}" --namespace "$namespace" apply -f -
"${kube[@]}" --namespace "$namespace" wait --for=condition=Ready \
  --timeout=180s scaledobject/podinfo-pull-example

hpa=$("${kube[@]}" --namespace "$namespace" get scaledobject podinfo-pull-example -o jsonpath='{.status.hpaName}')
"${kube[@]}" --namespace "$namespace" wait --timeout=60s \
  --for='jsonpath={.spec.metrics[?(@.type=="External")].type}=External' "hpa/$hpa"

printf '\nRun the failover test with\n%q %q 2 %q\n' "$dir/test-failover.sh" "$context" "$namespace"
