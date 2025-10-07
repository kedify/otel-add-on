#!/bin/bash

DIR="${DIR:-$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )}"
#export SETUP_ONLY=true

command -v figlet &> /dev/null && figlet -w95 Autoscaling podinfo

# setup helm repos
helm repo add podinfo https://stefanprodan.github.io/podinfo
helm repo add kedify https://kedify.github.io/charts
helm repo update podinfo kedify
set -e

# setup cluster
k3d cluster delete metric-pull &> /dev/null
k3d cluster create metric-pull -p "8181:31198@server:0"

# deploy stuff
helm upgrade -i podinfo podinfo/podinfo -f ${DIR}/podinfo-values.yaml
KEDA_VERSION=$(curl -s https://api.github.com/repos/kedify/charts/releases | jq -r '[.[].tag_name | select(. | startswith("keda/")) | sub("^keda/"; "")] | first')
KEDA_VERSION=${KEDA_VERSION:-v2.17.1-0}
helm upgrade -i keda kedify/keda --namespace keda --create-namespace --version ${KEDA_VERSION}
#helm upgrade -i keda-otel-scaler -nkeda oci://ghcr.io/kedify/charts/otel-add-on --version=v0.1.2 -f ${DIR}/scaler-with-collector-pull-values.yaml
helm upgrade -i keda-otel-scaler -nkeda ${DIR}/../../helmchart/otel-add-on -f ${DIR}/scaler-with-collector-pull-values.yaml

[ "x${SETUP_ONLY}" = "xtrue" ] && exit 0

kubectl rollout status -n keda --timeout=300s \
 deploy/keda-operator \
 deploy/keda-operator-metrics-apiserver \
 deploy/keda-otel-scaler
kubectl rollout status --timeout=300s deploy/podinfo

# create scaled objects
kubectl apply -f ${DIR}/podinfo-so.yaml

# create some traffic
(hey -n 7000 -z 180s http://localhost:8181/delay/2 &> /dev/null)&

# watch deployments being scaled
echo -e "\nhey is running in background, now deployments should be autoscaled.."
sleep 5
watch -c "kubectl get deploy/podinfo"

echo -e "\nDon't forget to delete the cluster:\n - k3d cluster delete metric-pull\n\n🚀"
