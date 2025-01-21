#!/bin/bash
set -e

OTEL_SCALER_VERSION=v0.0.4
KEDA_VERSION=v2.16.0-1

k3d cluster delete dapr-demo
k3d cluster create dapr-demo -p "8080:31222@server:0"

# setup dapr
# arch -arm64 brew install dapr/tap/dapr-cli
dapr init -k --dev
dapr status -k

# our tweaked version, until https://github.com/dapr/dapr/issues/7225 is done
kubectl set env deployments.apps -n dapr-system dapr-sidecar-injector SIDECAR_IMAGE=docker.io/jkremser/dapr:test SIDECAR_IMAGE_PULL_POLICY=Always
kubectl set image deploy/dapr-sidecar-injector -n dapr-system dapr-sidecar-injector=jkremser/dapr-injector:test
kubectl rollout status -n dapr-system deploy/dapr-sidecar-injector


# deploy otel scaler
cat <<VALUES | helm upgrade -i kedify-otel oci://ghcr.io/kedify/charts/otel-add-on --version=${OTEL_SCALER_VERSION} -f -
opentelemetry-collector:
  alternateConfig:
    processors:
      filter/ottl:
        error_mode: ignore
        metrics:
          metric: # drop all other metrics that are not whitelisted here
            - |
              name != "runtime/service_invocation/req_recv_total"
              and instrumentation_scope.attributes["app_id"] != "nodeapp"
              and instrumentation_scope.attributes["src_app_id"] != "pythonapp"
    service:
      pipelines:
        metrics:
          processors: [filter/ottl]
VALUES

# deploy test apps
kubectl apply -f https://raw.githubusercontent.com/dapr/quickstarts/refs/tags/v1.14.0/tutorials/hello-kubernetes/deploy/node.yaml
kubectl apply -f https://raw.githubusercontent.com/dapr/quickstarts/refs/tags/v1.14.0/tutorials/hello-kubernetes/deploy/python.yaml
kubectl patch svc nodeapp --type=merge -p '{"spec":{"type": "NodePort","ports":[{"nodePort": 31222, "port":80, "targetPort":3000}]}}'
kubectl patch deployments.apps pythonapp nodeapp --type=merge -p '{"spec":{"template": {"metadata":{"annotations": {
  "dapr.io/enable-metrics":"true",
  "dapr.io/metrics-port": "9090",
  "dapr.io/metrics-push-enable":"true",
  "dapr.io/metrics-push-endpoint":"otelcol:55678"
  }}}}}'


# deploy KEDA
helm repo add kedify https://kedify.github.io/charts
helm repo update kedify
helm upgrade -i keda kedify/keda --namespace keda --create-namespace  --version ${KEDA_VERSION}


# wait for it..
for d in nodeapp pythonapp otelcol otel-add-on-scaler ; do
  kubectl rollout status --timeout=300s deploy/${d}
done
for d in keda-admission-webhooks keda-operator keda-operator-metrics-apiserver ; do
  kubectl rollout status --timeout=300s deploy/${d} -nkeda
done


# create ScaledObject CR
cat <<SO | kubectl apply -f -
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: dapr-nodeapp
spec:
  scaleTargetRef:
    name: nodeapp
  triggers:
    - type: kedify-otel
      metadata:
        scalerAddress: 'keda-otel-scaler.keda.svc:4318'
        metricQuery: 'sum(runtime_service_invocation_req_recv_total{app_id="nodeapp",src_app_id="pythonapp"})'
        operationOverTime: 'rate'
        targetValue: '1'
        clampMax: '10'
  minReplicaCount: 1
SO

# test auto-scaling:

# scale the caller micro-service to 3 replicas and observe the node app
# kubectl scale deployment pythonapp --replicas=3

# create 100 request from pythonapp
# kubectl debug -it $(kubectl get po -ldapr.io/app-id=pythonapp -ojsonpath="{.items[0].metadata.name}") --image=nicolaka/netshoot -- sh -c 'for x in $(seq 100); do curl http://localhost:3500/v1.0/invoke/nodeapp/method/order/ ;done'
