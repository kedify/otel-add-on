#!/bin/bash
# In order for the sidecar approach to work properly, the CertManager needs to also be installed in the k8s cluster. Otherwise, OTel operator will not create the admission webhook correctly.

# install KEDA OTel Scaler & OTel Operator
helm upgrade -i keda-otel-scaler -nkeda oci://ghcr.io/kedify/charts/otel-add-on --version=v0.1.1 -f ./otel-scaler-values.yaml -f https://raw.githubusercontent.com/kedify/otel-add-on/refs/heads/main/helmchart/otel-add-on/enable-operator-hooks-values.yaml
#helm upgrade -i keda-otel-scaler -nkeda ${DIR}/../../helmchart/otel-add-on -f ${DIR}/otel-scaler-values.yaml -f https://raw.githubusercontent.com/kedify/otel-add-on/refs/heads/main/helmchart/otel-add-on/enable-operator-hooks-values.yaml

# roll the deployments so that mutating webhooks injects the sidecars
kubectl rollout restart deploy/vllm-llama3-deployment-vllm

# create ScaledObject
kubectl delete so model-router-approach 2> /dev/null || true
kubectl apply -f ./model-so.yaml

# test
(kubectl port-forward svc/vllm-router-service 30080:80 &> /dev/null)& pf_pid=$!
(sleep $[10*60] && kill ${pf_pid})&


for x in {0..5}; do curl -s -X POST http://localhost:30080/v1/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "meta-llama/Llama-3.1-8B-Instruct",
    "prompt": "Once upon a time,",
    "max_tokens": 20
  }' | jq '.choices[].text' ; done


sleep .8 && hey -c 60 -z 60s -t 90 -m POST \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "meta-llama/Llama-3.1-8B-Instruct",
    "prompt": "Once upon a time,",
    "max_tokens": 300
  }' \
  http://localhost:30080/v1/completions

# eventually, you should be able to see more replicas of the model
sleep 20 && kubectl get hpa -A
