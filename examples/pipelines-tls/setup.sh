#!/bin/bash
DIR="${DIR:-$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )}"

command -v figlet &> /dev/null && {
  __wid=$(/usr/bin/tput cols) && _wid=$(( __wid < 155 ? __wid : 155 ))
  figlet -w${_wid} OTel Operator + multiple collectors
}
echo "Architecture (all communication goes via TLS):"
cat architecture.ascii
set -e

# setup helm repos
helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts
helm repo add kedify https://kedify.github.io/charts
helm repo add prometheus https://prometheus-community.github.io/helm-charts
helm repo add grafana https://grafana.github.io/helm-charts
helm repo add jetstack https://charts.jetstack.io
helm repo update open-telemetry kedify prometheus grafana jetstack

# setup cluster
k3d cluster delete pipelines-tls &> /dev/null
k3d cluster create pipelines-tls -p "8080:31197@server:0" -p "8081:31196@server:0" -p "8082:31195@server:0"

# deploy stuff
kubectl create ns app
kubectl create ns observability
kubectl create ns keda
# cert-manager & trust-manager
helm upgrade -i --create-namespace -n cert-manager cert-manager oci://quay.io/jetstack/charts/cert-manager --version v1.18.2 --set crds.enabled=true
# https://github.com/cert-manager/trust-manager/blob/main/deploy/charts/trust-manager/values.yaml
helm upgrade -i --create-namespace -n cert-manager trust-manager jetstack/trust-manager \
 --set crds.enabled=true \
 --set secretTargets.enabled=true \
 --set secretTargets.authorizedSecretsAll=true \
 --wait --timeout=10m

# certs
kubectl apply -f ${DIR}/certs.yaml

# KEDA
KEDA_VERSION=$(curl -s https://api.github.com/repos/kedify/charts/releases | jq -r '[.[].tag_name | select(. | startswith("keda/")) | sub("^keda/"; "")] | first')
KEDA_VERSION=${KEDA_VERSION:-v2.17.1-0}
helm upgrade -i keda kedify/keda --namespace keda --create-namespace --version ${KEDA_VERSION}
# prometheus
helm upgrade -i --create-namespace -n observability prometheus prometheus-community/prometheus -f ${DIR}/prometheus-values.yaml
# grafana
helm upgrade -i --create-namespace -n observability grafana grafana/grafana -f ${DIR}/grafana-values.yaml

#helm upgrade -i keda-otel-scaler -nkeda oci://ghcr.io/kedify/charts/otel-add-on --version=v0.0.13 -f ${DIR}/scaler-pipelines-tls-values.yaml
# KEDA Scaler & OTel collectors
helm upgrade -i keda-otel-scaler -nkeda ${DIR}/../../helmchart/otel-add-on -f ${DIR}/scaler-pipelines-tls-values.yaml


[ "x${SETUP_ONLY}" = "xtrue" ] && exit 0
# wait for components
for d in \
  keda-operator \
  keda-operator-metrics-apiserver \
  otel-operator \
  keda-otel-scaler ; do
    kubectl rollout status -n keda --timeout=600s deploy/${d}
  done
kubectl rollout status -n observability --timeout=600s \
  deploy/router-collector \
  deploy/grafana
kubectl create cm nginx-dashboard -nobservability --from-file=${DIR}/grafana-dashboard.json
kubectl label -nobservability cm nginx-dashboard --overwrite grafana_dashboard=true

# prometheus & grafana helm chart do not allow setting fixed node port
kubectl patch service prometheus-server \
  -n observability --type='json' \
  -p='[{"op":"replace","path":"/spec/ports/0/nodePort","value":31196}]'
kubectl patch service grafana \
  -n observability --type='json' \
  -p='[{"op":"replace","path":"/spec/ports/0/nodePort","value":31195}]'

# nginx workload
kubectl apply -n app -f ${DIR}/workload.yaml
kubectl rollout status -n observability --timeout=600s deploy/prometheus-server
kubectl apply -f ${DIR}/so.yaml

cat <<USAGE
🚀
Continue with:
# nginx app
open http://localhost:8080

# prometheus
open http://localhost:8081

# grafana
open http://localhost:8082/dashboards

# check collectors
k get otelcol -A

# check certs
k get cert -A -owide

# create traffic
(hey -z 60s http://localhost:8080 &> /dev/null)&

# check how it scales out
k get hpa -A && k get so -A

# verify SSL works
kubectl debug -it -n observability router-collector-59cfdd7967-p8bjq --image=dockersec/tcpdump --target otc-container -- tcpdump -i any 'port 4317 and (tcp[tcpflags] & (tcp-syn|tcp-ack) == tcp-syn)'
and you should be able to observe beginnings of SSL handshakes (kill nginx pod to force one)
15:40:34.519246 eth0  In  IP 10.42.0.25.47070 > router-collector-59cfdd7967-p8bjq.4317: Flags [S], seq 3108166900, win 64860, options [mss 1410,sackOK,TS val 1743122799 ecr 0,nop,wscale 7], length 0
also tcpdump -n port 4317 -nn -A should return some arbitrary encrypted data
USAGE

# k3d cluster delete pipelines-tls
