# Use-case: pull metrics

This use-case demonstrates how OTEL collector can be used as a scraper of another metric endpoints and
then forwarding the filtered metrics into OTLP receiver in our scaler.

Prepare helm chart repos:

```bash
helm repo add podinfo https://stefanprodan.github.io/podinfo
helm repo add kedify https://kedify.github.io/charts
helm repo add kedify-otel https://kedify.github.io/otel-add-on
helm repo update
```

Any Kubernetes cluster will do:
```bash
k3d cluster create metric-pull -p "8181:31198@server:0"
```

Install demo webapp:

```bash
helm upgrade -i podinfo podinfo/podinfo -f podinfo-values.yaml
# check if the app is running
open http://localhost:8181
open http://localhost:8181/metrics
```

Install this addon:
```bash
helm upgrade -i kedify-otel kedify-otel/otel-add-on --version=v0.0.1-0 -f scaler-with-collector-pull-values.yaml
```

Note the following section in the helm chart values that configures the OTEL collector to scrape targets:

```yaml
...
  config:
    receivers:
      # https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/receiver/prometheusreceiver/README.md
      prometheus:
        config:
          scrape_configs:
            - job_name: 'otelcol'
              scrape_interval: 5s
              static_configs:
                - targets: ['0.0.0.0:8888']
            - job_name: k8s
              kubernetes_sd_configs:
                - role: service
              relabel_configs:
                - source_labels: [__meta_kubernetes_service_annotation_prometheus_io_scrape]
                  regex: "true"
                  action: keep
                - source_labels: [__meta_kubernetes_service_annotation_prometheus_io_path]
                  action: replace
                  target_label: __metrics_path__
                  regex: (.+)
                - source_labels: [__address__, __meta_kubernetes_service_annotation_prometheus_io_port]
                  action: replace
                  target_label: __address__
                  regex: (.+)(?::\d+);(\d+)
                  replacement: $1:$2
...
```
We are adding one static target - the metrics from the OTEL collector itself, just for demo purposes, these
won't be used for scaling decision. And also any service annotated with `prometheus.io/scrape=true`. One can
also modify the path where the metrics are exported using `prometheus.io/path=/metrics`.

We set these two annotation in our service for podinfo [here](./podinfo-values.yaml).

Install KEDA by Kedify.io:
```bash
helm upgrade -i keda kedify/keda --namespace keda --create-namespace
```

Create `ScaledObject`:
```bash
kubectl apply -f podinfo-so.yaml
```

`Podinfo` exposes some basic metrics and one of them is `http_request_duration_seconds` histogram. We can take the `http_request_duration_seconds_count`,
which is a monotonic counter that increases with each request and turn it into the metric that will determine
how many replicas of pod we want. Scaler supports `rate` "function over time" similar to the 
[one](https://prometheus.io/docs/prometheus/latest/querying/functions/#rate) from PromQL.

Finally, create some traffic. Podinfo has an endpoint that responds after a delay, in this case it's two seconds.
```bash
hey -n 5000 -z 120s http://localhost:8181/delay/2
```

Observer how number of replicas of Podinfo deployment is reacting on the load.

```bash
watch kubectl get pods -A
```

Once finished, clean the cluster:
```bash
k3d cluster delete metric-pull
```