# Use-case: pull metrics

This use-case demonstrates how OTEL collector can be used as a scraper of another metric endpoints and
then forwarding the filtered metrics into OTLP receiver in our scaler.

Prepare helm chart repos:

```bash
helm repo add kedacore https://kedacore.github.io/charts
helm repo add podinfo https://stefanprodan.github.io/podinfo
helm repo add kedify-otel https://kedify.github.io/otel-add-on/
helm repo update
```

Install demo webapp:

```bash
helm upgrade -i podinfo podinfo/podinfo -f podinfo-values.yaml
kubectl -n default port-forward deploy/podinfo 8080:9898
```

Install this addon:
```bash
helm upgrade -i kedify-otel kedify-otel/otel-add-on --version=v0.0.0-1 -f collector-pull-values.yaml
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
...
```
We are adding one static target - the metrics from the OTEL collector itself, just for demo purposes, these
won't be used for scaling decision. And also any service annotated with `prometheus.io/scrape=true`. One can
also modify the path where the metrics are exported using `prometheus.io/path=/metrics`.

We set these two annotation in our service for podinfo [here](./podinfo-values.yaml).

Install KEDA:
```bash
helm upgrade -i keda kedacore/keda --namespace keda --create-namespace
```

Create `ScaledObject`:
```bash
kubectl apply -f podinfo-so.yaml
```

```bash
watch kubectl get pods -A
```

Create some traffic:
```bash
hey -n 5000 -c 4 -q 20 -z 70s http://localhost:8080/delay/2
```
