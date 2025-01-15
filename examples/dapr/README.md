# Use-case: Dapr 
## Autoscaling Based on Number of Service Invocations

In this example we will set up a microservice architecture using Dapr middleware. There will be two microservices:
one written in Node.js called `nodeapp` and one written in Python called `pythonapp`. These services are based on an
upstream [example](https://docs.dapr.io/getting-started/quickstarts/serviceinvocation-quickstart/),
where the Python app calls the Node app using the service invocation pattern.

Both workloads run `daprd` in a sidecar container, which also exposes metrics. We have modified the `daprd` and its
mutating webhook (`dapr-sidecar-injector`) to push metrics to our OTEL collector. These metrics use OpenCensus,
so we need to configure the OTEL collector to accept metrics through the `opencensus` receiver.

## Setup

Any Kubernetes cluster will work for this setup:
```bash
k3d cluster create dapr-demo -p "8080:31222@server:0"
```

Setup Dapr on the Kubernetes cluster (`dapr` cli is needed):
```bash
arch -arm64 brew install dapr/tap/dapr-cli
dapr init -k --dev
dapr status -k
```

Apply the patch so that our version of Dapr is used:
```bash
# our tweaked version, until https://github.com/dapr/dapr/issues/7225 is done
kubectl set env deployments.apps -n dapr-system dapr-sidecar-injector SIDECAR_IMAGE=docker.io/jkremser/dapr:test SIDECAR_IMAGE_PULL_POLICY=Always
kubectl set image deploy/dapr-sidecar-injector -n dapr-system dapr-sidecar-injector=jkremser/dapr-injector:test
kubectl rollout status -n dapr-system deploy/dapr-sidecar-injector
```

Deploy this scaler and OTEL collector that forwards one whitelisted metric:
```bash
cat <<VALUES | helm upgrade -i kedify-otel oci://ghcr.io/kedify/charts/otel-add-on --version=v0.0.5 -f -
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
```

Deploy two demo apps and patch them so that they are able to push the metrics to collector:
```bash
kubectl apply -f https://raw.githubusercontent.com/dapr/quickstarts/refs/tags/v1.14.0/tutorials/hello-kubernetes/deploy/node.yaml
kubectl apply -f https://raw.githubusercontent.com/dapr/quickstarts/refs/tags/v1.14.0/tutorials/hello-kubernetes/deploy/python.yaml
kubectl patch svc nodeapp --type=merge -p '{"spec":{"type": "NodePort","ports":[{"nodePort": 31222, "port":80, "targetPort":3000}]}}'
kubectl patch deployments.apps pythonapp nodeapp --type=merge -p '{"spec":{"template": {"metadata":{"annotations": {
  "dapr.io/enable-metrics":"true",
  "dapr.io/metrics-port": "9090",
  "dapr.io/metrics-push-enable":"true",
  "dapr.io/metrics-push-endpoint":"otelcol:55678"
  }}}}}'
```

Deploy Kedify KEDA:
```bash
helm repo add kedify https://kedify.github.io/charts
helm repo update kedify
helm upgrade -i keda kedify/keda --namespace keda --create-namespace  --version v2.16.0-1
```

Wait for all the deployment to become ready
```bash
for d in nodeapp pythonapp otelcol otel-add-on-scaler ; do
kubectl rollout status --timeout=300s deploy/${d}
done
for d in keda-admission-webhooks keda-operator keda-operator-metrics-apiserver ; do
kubectl rollout status --timeout=300s deploy/${d} -nkeda
done
```

```bash
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
        scalerAddress: 'keda-otel-scaler.default.svc:4318'
        metricQuery: 'sum(runtime_service_invocation_req_recv_total{app_id="nodeapp",src_app_id="pythonapp"})'
        operationOverTime: 'rate'
        targetValue: '1'
        clampMax: '10'
  minReplicaCount: 1
SO
```

## Scaling Behavior

Each replica of the pythonapp microservice makes a call to the nodeapp microservice every second. Check the following
part of the ScaledObject configuration:

```yaml
metricQuery: 'sum(runtime_service_invocation_req_recv_total{app_id="nodeapp",src_app_id="pythonapp"})'
operationOverTime: 'rate'
```

- The runtime_service_invocation_req_recv_total metric increments each time the `pythonapp` calls `nodeapp`.
- One of the metric dimensions is the pod identity, meaning each pod exposes these metrics with its label attached.
- Similar to PromQL, if not all dimensions are specified, multiple metric series will be returned.
- The OTEL scaler calculates the rate over a one-minute window (default). This should be `1`, as we are calling the API 
  every second, so the counter increments by one each second.
- If multiple metric series are present, the sum is applied to aggregate the values. For example, if there are three
  producer pods, the total will be `3`.
- The `targetValue` was set to `1`, indicating that one replica of nodeapp can handle this value. This ensures replica
  parity between the two services.
- If `targetValue` was set to `2`, it would indicate that if we scale pythonapp (the producer) to `N` replicas,
  it would result in `nodeapp` (the consumer) being scaled to `N/2` replicas.

Scale the caller microservice to `3` replicas and observe the node app:
```bash
kubectl scale deployment pythonapp --replicas=3
```

This should lead to `nodeapp` being scaled also to `3` replicas.

Create `100` request from `pythonapp`
```bash
_podName=$(kubectl get po -ldapr.io/app-id=pythonapp -ojsonpath="{.items[0].metadata.name}")
kubectl debug -it ${_podName} --image=nicolaka/netshoot -- sh -c 'for x in $(seq 100); do curl http://localhost:3500/v1.0/invoke/nodeapp/method/order/ ;done'
```

Eventually, the node app should be scaled back to `pythonapp`'s number of replicas.

Check the logs:

```bash
kubectl logs -lapp.kubernetes.io/name=otel-add-on --tail=-1 --follow
```

Once finished, clean the cluster:
```bash
k3d cluster delete dapr-demo
```
