# Use-case: push metrics

This use-case demonstrates how OTel collector can be used as a scraper of another metric endpoints and
then forwarding the filtered metrics into OTLP receiver in our scaler.

Prepare helm chart repos:

```bash
helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts
helm repo add kedify https://kedify.github.io/charts
helm repo update open-telemetry kedify
```

Any Kubernetes cluster will do:
```bash
k3d cluster create metric-push -p "8080:31198@server:0"
```

Install demo app
- architecture: https://opentelemetry.io/docs/demo/architecture/
- helm chart: https://github.com/open-telemetry/opentelemetry-helm-charts/tree/main/charts/opentelemetry-demo

```bash
helm upgrade -i my-otel-demo open-telemetry/opentelemetry-demo -f opentelemetry-demo-values.yaml
# check if the app is running
open http://localhost:8080
```

Install this addon:
```bash
helm upgrade -i keda-otel-scaler oci://ghcr.io/kedify/charts/otel-add-on --version=v0.1.3 --create-namespace -f scaler-only-push-values.yaml
```

In this scenario, we don't install OTel collector using the `kedify-otel/otel-add-on` helm chart, because
the `opentelemetry-demo` already creates one and it was configured to forward all metrics to our scaler.
If we wanted to filter the metrics, we would need to deploy another OTel collector and configure the processor
there so that it would look like this:

```bash
  ┌────────────┐     ┌────────────┐     ┌─────────────┐
  │            │     │            │     │             │
  │ OTel col 1 ├────►│ OTel col 2 ├────►│ this scaler │
  │            │     │ (filtering)│     │             │
  └────────────┘     └────────────┘     └─────────────┘
   
instead we go w/ simple (w/o filtering):
  ┌────────────┐     ┌─────────────┐
  │            │     │             │
  │ OTel col 1 ├────►│ this scaler │
  │            │     │             │
  └────────────┘     └─────────────┘
```

Install KEDA by Kedify.io:
```bash
helm upgrade -i keda kedify/keda --namespace keda --version vX.Y.Z-n
```

 > [!TIP]
 > Find out the latest version of Kedify KEDA (the "`X.Y.Z-n`") at [releases](https://github.com/kedify/charts/releases) page or by running:
 > ```bash
 > curl -s https://api.github.com/repos/kedify/charts/releases | jq -r '[.[].tag_name | select(. | startswith("keda/")) | sub("^keda/"; "")] | first'
 > ```

We will be scaling two microservices for this application, first let's check what metrics are there in shipped 
[grafana](http://localhost:8080/grafana/explore?schemaVersion=1&panes=%7B%222n3%22:%7B%22datasource%22:%22webstore-metrics%22,%22queries%22:%5B%7B%22refId%22:%22A%22,%22expr%22:%22app_frontend_requests_total%22,%22range%22:true,%22instant%22:true,%22datasource%22:%7B%22type%22:%22prometheus%22,%22uid%22:%22webstore-metrics%22%7D,%22editorMode%22:%22code%22,%22legendFormat%22:%22__auto%22,%22useBackend%22:false,%22disableTextWrap%22:false,%22fullMetaSearch%22:false,%22includeNullMetadata%22:true%7D%5D,%22range%22:%7B%22from%22:%22now-1h%22,%22to%22:%22now%22%7D%7D%7D&orgId=1).

We will use following two metrics for scaling microservices `recommendationservice` and `productcatalogservice`.
```bash
...
app_frontend_requests_total{instance="0b38958c-f169-4a83-9adb-cf2c2830d61e", job="opentelemetry-demo/frontend", method="GET", status="200", target="/api/recommendations"}
1824
app_frontend_requests_total{instance="0b38958c-f169-4a83-9adb-cf2c2830d61e", job="opentelemetry-demo/frontend", method="GET", status="200", target="/api/products"}
1027
...
```

Create `ScaledObject`s:
```bash
kubectl apply -f sos.yaml
```

The demo application contains a load generator that can be further tweaked on http://localhost:8080/loadgen/ endpoint and by
default, creates a lot of traffic in the eshop. So there is no need to create further load from our side and we can just
observe the effects of autoscaling:

```bash
watch kubectl get deploy my-otel-demo-recommendationservice my-otel-demo-productcatalogservice
```

If the load is still too low, you can help each microservice individually by:
```bash
hey http://localhost:8080/api/recommendations

hey http://localhost:8080/api/products

#k port-forward svc/my-otel-demo-imageprovider 8282:8081
#hey -z 50s http://localhost:8282/Banner.png
```

Once finished, clean the cluster:
```bash
k3d cluster delete metric-push
```

> [!TIP]
> In general, when instrumenting the application using OTel sdk, you can call the 
> [ForceFlush](https://opentelemetry.io/docs/specs/otel/metrics/sdk/#forceflush-1)
> to further decrease the delay between a metric change and scaling trigger.
