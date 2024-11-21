# otel-add-on

![Version: v0.0.2](https://img.shields.io/badge/Version-v0.0.2-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: v0.0.2](https://img.shields.io/badge/AppVersion-v0.0.2-informational?style=flat-square)

[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/otel-add-on)](https://artifacthub.io/packages/search?repo=otel-add-on)

A Helm chart for KEDA otel-add-on

```
:::^.     .::::^:     :::::::::::::::    .:::::::::.                   .^.
7???~   .^7????~.     7??????????????.   :?????????77!^.              .7?7.
7???~  ^7???7~.       ~!!!!!!!!!!!!!!.   :????!!!!7????7~.           .7???7.
7???~^7????~.                            :????:    :~7???7.         :7?????7.
7???7????!.           ::::::::::::.      :????:      .7???!        :7??77???7.
7????????7:           7???????????~      :????:       :????:      :???7?5????7.
7????!~????^          !77777777777^      :????:       :????:     ^???7?#P7????7.
7???~  ^????~                            :????:      :7???!     ^???7J#@J7?????7.
7???~   :7???!.                          :????:   .:~7???!.    ~???7Y&@#7777????7.
7???~    .7???7:      !!!!!!!!!!!!!!!    :????7!!77????7^     ~??775@@@GJJYJ?????7.
7???~     .!????^     7?????????????7.   :?????????7!~:      !????G@@@@@@@@5??????7:
::::.       :::::     :::::::::::::::    .::::::::..        .::::JGGGB@@@&7:::::::::
        _       _               _     _                               ?@@#~
   ___ | |_ ___| |     __ _  __| | __| |     ___  _ __                P@B^
  / _ \| __/ _ \ |    / _` |/ _` |/ _` |___ / _ \| '_ \             :&G:
 | (_) | ||  __/ |   | (_| | (_| | (_| |___| (_) | | | |            !5.
  \___/ \__\___|_|    \__,_|\__,_|\__,_|    \___/|_| |_|            ,
                                                                    .
```

**Homepage:** <https://github.com/kedify/otel-add-on>

## Usage

Check available version in OCI repo:
```
crane ls ghcr.io/kedify/charts/otel-add-on | grep -E '^v?[0-9]'
```

Install specific version:
```
helm upgrade -i oci://ghcr.io/kedify/charts/otel-add-on --version=v0.0.1
```

## Source Code

* <https://github.com/kedify/otel-add-on>
* <https://github.com/open-telemetry/opentelemetry-helm-charts>

## Requirements

Kubernetes: `>= 1.19.0-0`

| Repository | Name | Version |
|------------|------|---------|
| https://open-telemetry.github.io/opentelemetry-helm-charts | opentelemetry-collector | 0.108.0 |

## OTEL Collector Sub-Chart

This helm chart, if not disabled by `--set opentelemetry-collector.enabled=false`, installs the OTEL collector using
its upstream [helm chart](https://github.com/open-telemetry/opentelemetry-helm-charts/tree/main/charts/opentelemetry-collector).

To check all the possible values for this dependent helm chart, consult [values.yaml](https://github.com/open-telemetry/opentelemetry-helm-charts/blob/main/charts/opentelemetry-collector/values.yaml)
or [docs](https://github.com/open-telemetry/opentelemetry-helm-charts/blob/main/charts/opentelemetry-collector/README.md).

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| image.repository | string | `"ghcr.io/kedify/otel-add-on"` | Image to use for the Deployment |
| image.pullPolicy | string | `"Always"` | Image pull policy, consult [docs](https://kubernetes.io/docs/concepts/containers/images/#image-pull-policy) |
| image.tag | string | `""` | Image version to use for the Deployment, if not specified, it defaults to `.Chart.AppVersion` |
| settings.metricStoreRetentionSeconds | int | `120` | how long the metrics should be kept in the short term (in memory) storage |
| settings.isActivePollingIntervalMilliseconds | int | `500` | how often (in milliseconds) should the IsActive method be tried |
| settings.internalMetricsPort | int | `8080` | internal (mostly golang) metrics will be exposed on `:8080/metrics` |
| settings.restApiPort | int | `9090` | port where rest api should be listening |
| settings.logs.logLvl | string | `"info"` | Can be one of 'debug', 'info', 'error', or any integer value > 0 which corresponds to custom debug levels of increasing verbosity |
| settings.logs.stackTracesLvl | string | `"error"` | one of: info, error, panic |
| settings.logs.noColor | bool | `false` | if anything else than 'false', the log will not contain colors |
| settings.logs.noBanner | bool | `false` | if anything else than 'false', the log will not print the ascii logo |
| asciiArt | bool | `true` | should the ascii logo be printed when this helm chart is installed |
| imagePullSecrets | list | `[]` | [details](https://kubernetes.io/docs/concepts/containers/images/#specifying-imagepullsecrets-on-a-pod) |
| serviceAccount.create | bool | `true` | should the service account be also created and linked in the deployment |
| serviceAccount.annotations | object | `{}` | further custom annotation that will be added on the service account |
| serviceAccount.name | string | `""` | name of the service account, defaults to `otel-add-on.fullname` ~ release name if not overriden |
| podAnnotations | object | `{}` | additional custom pod annotations that will be used for pod |
| podLabels | object | `{}` | additional custom pod labels that will be used for pod |
| podSecurityContext | object | `{}` | [details](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/#set-the-security-context-for-a-pod) |
| securityContext.readOnlyRootFilesystem | bool | `true` | [details](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/) |
| securityContext.runAsNonRoot | bool | `true` | [details](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/#implicit-group-memberships-defined-in-etc-group-in-the-container-image) |
| securityContext.runAsUser | int | `1000` | [details](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/#implicit-group-memberships-defined-in-etc-group-in-the-container-image) |
| service.type | string | `"ClusterIP"` | Under this service, the otel add on needs to be reachable by KEDA operator and OTEL collector ([details](https://kubernetes.io/docs/concepts/services-networking/service/#publishing-services-service-types)) |
| service.otlpReceiverPort | int | `4317` | OTLP receiver will be opened on this port. OTEL exporter configured in the OTEL collector needs to have this value set. |
| service.kedaExternalScalerPort | int | `4318` | KEDA external scaler will be opened on this port. ScaledObject's `.spec.triggers[].metadata.scalerAddress` needs to be set to this svc and this port. |
| resources.limits.cpu | string | `"500m"` | cpu limit for the pod, enforced by cgroups ([details](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/)) |
| resources.limits.memory | string | `"256Mi"` | memory limit for the pod, used by oomkiller ([details](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/)) |
| resources.requests.cpu | string | `"500m"` | cpu request for the pod, used by k8s scheduler ([details](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/)) |
| resources.requests.memory | string | `"128Mi"` | memory request for the pod, used by k8s scheduler ([details](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/)) |
| nodeSelector | object | `{}` | [details](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#nodeselector) |
| tolerations | list | `[]` | [details](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) |
| affinity | object | `{}` | [details](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#affinity-and-anti-affinity) |
