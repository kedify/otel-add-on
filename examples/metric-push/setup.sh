#!/bin/bash
DIR="${DIR:-$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )}"
DEMO_APP="${DEMO_APP:-my-otel-demo}"

command -v figlet &> /dev/null && figlet Autoscaling OTel demo

# setup helm repos
helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts
helm repo add kedify https://kedify.github.io/charts
helm repo update open-telemetry kedify

set -e
# setup cluster
k3d cluster delete metric-push &> /dev/null
k3d cluster create metric-push -p "8080:31198@server:0"

# deploy stuff
helm upgrade -i my-otel-demo open-telemetry/opentelemetry-demo -f ${DIR}/opentelemetry-demo-values.yaml
helm upgrade -i kedify-otel oci://ghcr.io/kedify/charts/otel-add-on --version=v0.0.6 -f ${DIR}/scaler-only-push-values.yaml
helm upgrade -i keda kedify/keda --namespace keda --create-namespace

kubectl rollout status -n keda --timeout=300s deploy/keda-operator
kubectl rollout status -n keda --timeout=300s deploy/keda-operator-metrics-apiserver
for d in \
  ${DEMO_APP}-accountingservice \
  ${DEMO_APP}-checkoutservice \
  ${DEMO_APP}-frauddetectionservice \
  ${DEMO_APP}-frontend \
  ${DEMO_APP}-kafka \
  ${DEMO_APP}-loadgenerator \
  ${DEMO_APP}-valkey \
  ${DEMO_APP}-productcatalogservice \
  ${DEMO_APP}-otelcol \
  ${DEMO_APP}-shippingservice \
  ${DEMO_APP}-frontendproxy \
  ${DEMO_APP}-currencyservice \
  ${DEMO_APP}-adservice \
  ${DEMO_APP}-jaeger \
  ${DEMO_APP}-emailservice \
  ${DEMO_APP}-prometheus-server \
  ${DEMO_APP}-paymentservice \
  ${DEMO_APP}-recommendationservice \
  ${DEMO_APP}-imageprovider \
  ${DEMO_APP}-grafana \
  ${DEMO_APP}-cartservice \
  ${DEMO_APP}-quoteservice \
  otel-add-on-scaler ; do
    kubectl rollout status --timeout=600s deploy/${d}
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
watch -c "kubectl get deploy/${DEMO_APP}-recommendationservice deploy/${DEMO_APP}-productcatalogservice hpa/keda-hpa-recommendationservice hpa/keda-hpa-productcatalogservice"
