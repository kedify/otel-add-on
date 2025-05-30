#!/bin/bash
DIR="${DIR:-$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )}"

export PR_BRANCH=
export GH_PAT=

command -v figlet &> /dev/null && figlet OTel Operator + GitHub receiver
[ -z "${PR_BRANCH}" ] && echo "Set BRANCH env variable to a branch name from which a PR is opened against kedify/otel-add-on repo" && exit 1
[ -z "${GH_PAT}" ] && echo "Set GH_PAT env variable to a PAT token that has read permissions for content on kedify/otel-add-on repo repo"  && exit 1

# setup helm repos
helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts
helm repo add kedify https://kedify.github.io/charts
helm repo update open-telemetry kedify

set -e
# setup cluster
k3d cluster delete otel-operator &> /dev/null
k3d cluster create otel-operator --no-lb --k3s-arg "--disable=traefik,servicelb@server:*"

# deploy stuff
KEDA_VERSION=$(curl -s https://api.github.com/repos/kedify/charts/releases | jq -r '[.[].tag_name | select(. | startswith("keda/")) | sub("^keda/"; "")] | first')
KEDA_VERSION=${KEDA_VERSION:-v2.17.1-0}
helm upgrade -i keda kedify/keda --namespace keda --create-namespace --version ${KEDA_VERSION}

kubectl create secret -nkeda generic gh-token --from-literal=GH_PAT=${GH_PAT}
helm upgrade -i kedify-otel oci://ghcr.io/kedify/charts/otel-add-on --version=v0.0.9 -f ${DIR}/scaler-with-operator-with-collector-values.yaml
#helm upgrade -i kedify-otel -nkeda ${DIR}/../../helmchart/otel-add-on -f ${DIR}/scaler-with-operator-with-collector-values.yaml

# wait for components
for d in \
  keda-operator \
  keda-operator-metrics-apiserver \
  otel-operator \
  otel-add-on-scaler \
  otel-add-on-otc-collector ; do
    kubectl rollout status -n keda --timeout=600s deploy/${d}
  done

# create ScaledObject
kubectl apply -f <(cat ${DIR}/so.yaml | envsubst)

# there should be only one replica of OTel Operator (target deployment of our SO)
sleep 5
kubectl get hpa -A

# eventually, you shoul be able to see 3 replicas of OTel operator deployment
sleep 40
kubectl get hpa -A

# k3d cluster delete otel-operator
