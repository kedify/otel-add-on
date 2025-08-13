#!/bin/bash
DIR="${DIR:-$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )}"

#export HF_TOKEN=

command -v figlet &> /dev/null && figlet -w155 OTel Operator + vLLM stack
[ -z "${HF_TOKEN}" ] && echo "Set HF_TOKEN env variable (https://huggingface.co/docs/hub/en/security-tokens)" && exit 1

# make sure your k8s cluster supports GPUs and have at least one accelerator on a node
# following pod should run successfully
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: cuda-vectoradd
spec:
  restartPolicy: OnFailure
  containers:
  - name: cuda-vectoradd
    image: "nvcr.io/nvidia/k8s/cuda-sample:vectoradd-cuda11.7.1-ubuntu20.04"
    resources:
      limits:
        nvidia.com/gpu: 1
EOF


# setup helm repos
helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts
helm repo add kedify https://kedify.github.io/charts
helm repo add vllm https://vllm-project.github.io/production-stack
helm repo update open-telemetry kedify vllm

set -e

# deploy KEDA
KEDA_VERSION=$(curl -s https://api.github.com/repos/kedify/charts/releases | jq -r '[.[].tag_name | select(. | startswith("keda/")) | sub("^keda/"; "")] | first')
KEDA_VERSION=${KEDA_VERSION:-v2.17.1-0}
helm upgrade -i keda kedify/keda --namespace keda --create-namespace --version ${KEDA_VERSION}

# deploy vLLM Stack
helm upgrade -i vllm vllm/vllm-stack --version 0.1.5 -f ${DIR}/vllm-stack-values.yaml --set "servingEngineSpec.modelSpec[0].hf_token=${HF_TOKEN}"

# wait for components
for d in \
  keda-operator \
  keda-operator-metrics-apiserver \
  otel-operator \
  keda-otel-scaler \
  otel-add-on-collector ; do
    kubectl rollout status -n keda --timeout=600s deploy/${d}
  done

echo "Continue either with ./sidecar/setup.sh, ./scraping-router/setup.sh or ./dcgm/setup.sh"
