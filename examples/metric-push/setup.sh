#!/bin/bash
DIR="${DIR:-$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )}"

command -v figlet &> /dev/null && figlet -w105 Autoscaling OTel demo

# setup helm repos
helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts
helm repo add kedify https://kedify.github.io/charts
helm repo update open-telemetry kedify

set -e
# setup cluster
k3d cluster delete metric-push &> /dev/null
k3d cluster create metric-push -p "8080:31198@server:0"

# deploy stuff
KEDA_VERSION=$(curl -s https://api.github.com/repos/kedify/charts/releases | jq -r '[.[].tag_name | select(. | startswith("keda/")) | sub("^keda/"; "")] | first')
KEDA_VERSION=${KEDA_VERSION:-v2.17.1-0}
helm upgrade -i keda kedify/keda --namespace keda --create-namespace --version ${KEDA_VERSION}
helm upgrade -i my-otel-demo open-telemetry/opentelemetry-demo -f ${DIR}/opentelemetry-demo-values.yaml --version=0.37.1
helm upgrade -i keda-otel-scaler -nkeda oci://ghcr.io/kedify/charts/otel-add-on --version=v0.0.13 -f ${DIR}/scaler-only-push-values.yaml
#helm upgrade -i keda-otel-scaler -nkeda ${DIR}/../../helmchart/otel-add-on -f ${DIR}/scaler-only-push-values.yaml

kubectl rollout status -n keda --timeout=300s deploy/keda-operator
kubectl rollout status -n keda --timeout=300s deploy/keda-otel-scaler
kubectl rollout status -n keda --timeout=300s deploy/keda-operator-metrics-apiserver
for d in \
  accounting \
  checkout \
  fraud-detection \
  frontend \
  kafka \
  load-generator \
  valkey-cart \
  product-catalog \
  otel-collector \
  shipping \
  frontend-proxy \
  currency \
  ad \
  jaeger \
  email \
  prometheus \
  payment \
  recommendation \
  image-provider \
  grafana \
  cart \
  quote ; do
    kubectl rollout status --timeout=900s deploy/${d}
  done

# create scaled objects
kubectl apply -f ${DIR}/sos.yaml

# run against scaler running outside of k8s (debug)
#SO_NAME=recommendationservice
#LOCAL_ENDPOINT=192.168.84.98
#helm upgrade --reuse-values \
# 		my-otel-demo open-telemetry/opentelemetry-demo \
# 		--set opentelemetry-collector.config.exporters.otlp/keda.endpoint=${LOCAL_ENDPOINT}:4317
#kubectl patch so ${SO_NAME} --type=json -p '[{"op":"replace","path":"/spec/triggers/0/metadata/scalerAddress","value":"'${LOCAL_ENDPOINT}':4318"}]'


# watch deployments being scaled
echo "now deployments should be autoscaled.."
sleep 5
watch -c "kubectl get deploy/recommendation deploy/product-catalog hpa/keda-hpa-recommendationservice hpa/keda-hpa-productcatalogservice"
