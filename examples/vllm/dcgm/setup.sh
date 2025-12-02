#!/bin/bash
# scaling the model using the DCGM metrics. DCGM exporter needs to be enabled and it is assumed in the namespace gpu-operator
# namely service nvidia-dcgm-exporter.gpu-operator.svc

# install KEDA OTel Scaler & OTel Operator
helm upgrade -i keda-otel-scaler -nkeda oci://ghcr.io/kedify/charts/otel-add-on --version=v0.1.3 -f ./dcgm-values.yaml
#helm upgrade -i keda-otel-scaler -nkeda ${DIR}/../../helmchart/otel-add-on -f ${DIR}/dcgm-values.yaml

# roll the deployments so that mutating webhooks (un)injects the sidecars (if the sidecar setup was run before)
kubectl rollout restart deploy/vllm-llama3-deployment-vllm

# create ScaledObject
kubectl delete so model-sidecar-approach model-router-approach 2> /dev/null || true
kubectl apply -f ./model-so.yaml

# test
(kubectl port-forward svc/vllm-router-service 30080:80 &> /dev/null)& pf_pid=$!
(sleep $[10*60] && kill ${pf_pid})&

sleep .8 && hey -c 300 -z 60s -t 90 -m POST http://localhost:30080/v1/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "meta-llama/Llama-3.1-8B-Instruct",
    "prompt": "Once upon a time,",
    "max_tokens": 3000
  }'

# eventually, you should be able to see more replicas of the model
sleep 20 && kubectl get hpa -A
