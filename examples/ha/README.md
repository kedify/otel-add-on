# High availability for the OTel scaler

This example runs three active-active OTel scaler replicas. An OTel Collector
scrapes podinfo's Prometheus metrics and sends the same samples to every scaler.
KEDA uses the rate of successful HTTP requests to scale podinfo between one and
three replicas.

## Set up the demo

You need a running Docker daemon, `k3d`, `helm`, `kubectl`, `jq`, `curl`, and
`hey`. From this directory, run the setup script.

```bash
./setup.sh
```

The script creates or reuses the local `otel-scaler-ha` k3d cluster, installs
KEDA in `keda`, and deploys podinfo, the Collector, the scaler StatefulSet, and
the `podinfo-pull-example` ScaledObject in `ha-enabled`. It uses the same
[Prometheus scrape configuration](../../e2e-tests/testdata/scaler-ha-values.yaml)
as the HA end-to-end test. The setup adds the `k3d-otel-scaler-ha` context to
your kubeconfig and keeps your current context selected.

Setup builds the scaler from the checkout for your local Docker architecture.
To use a published HA image tag from `ghcr.io/kedify/otel-add-on` that supports
your cluster's architecture, set `OTEL_SCALER_VERSION`.

```bash
OTEL_SCALER_VERSION=ha ./setup.sh
```

Set `CLUSTER` to use a different local cluster name and pass its `k3d-` context
to the commands below.

## Test failover

```bash
./test-failover.sh k3d-otel-scaler-ha
```

The first round stops one scaler Pod and the second stops two. In each round,
the script sends traffic to podinfo and checks that it scales up before the
stopped scalers are replaced. After the replacements are ready, it waits for
podinfo to return to one replica. Pass `0` as the second argument to check
scaling without stopping any scaler Pods.

To watch podinfo scale during the test, run this in another terminal.

```bash
kubectl --context k3d-otel-scaler-ha -n ha-enabled get deployment podinfo --watch
```

## Clean up

```bash
k3d cluster delete otel-scaler-ha
```
