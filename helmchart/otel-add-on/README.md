# otel-add-on

![Version: v0.0.13](https://img.shields.io/badge/Version-v0.0.13-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: v0.0.13](https://img.shields.io/badge/AppVersion-v0.0.13-informational?style=flat-square)

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
helm upgrade -i oci://ghcr.io/kedify/charts/otel-add-on --version=v0.0.13
```

## Source Code

* <https://github.com/kedify/otel-add-on>
* <https://github.com/open-telemetry/opentelemetry-helm-charts>

## Requirements

Kubernetes: `>= 1.19.0-0`

| Repository | Name | Version |
|------------|------|---------|
| https://open-telemetry.github.io/opentelemetry-helm-charts | otelCollector(opentelemetry-collector) | 0.110.0 |
| https://open-telemetry.github.io/opentelemetry-helm-charts | otelOperator(opentelemetry-operator) | 0.90.0 |

## OTel Collector Sub-Chart

This helm chart, if not disabled by `--set opentelemetry-collector.enabled=false`, installs the OTel collector using
its upstream [helm chart](https://github.com/open-telemetry/opentelemetry-helm-charts/tree/main/charts/opentelemetry-collector).

To check all the possible values for this dependent helm chart, consult [values.yaml](https://github.com/open-telemetry/opentelemetry-helm-charts/blob/main/charts/opentelemetry-collector/values.yaml)
or [docs](https://github.com/open-telemetry/opentelemetry-helm-charts/blob/main/charts/opentelemetry-collector/README.md).

## Values

## Values

<table>
     <thead>
          <th>Key</th>
          <th>Description</th>
          <th>Default</th>
     </thead>
     <tbody>
          <tr>
               <td id="image--repository">
               <a href="./values.yaml#L10">image.repository</a><br/>
               (string)
               </td>
               <td>
               Image to use for the Deployment
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
"ghcr.io/kedify/otel-add-on"
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="image--pullPolicy">
               <a href="./values.yaml#L12">image.pullPolicy</a><br/>
               (string)
               </td>
               <td>
               Image pull policy, consult <a href="https://kubernetes.io/docs/concepts/containers/images/#image-pull-policy">docs</a>
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
"Always"
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="image--tag">
               <a href="./values.yaml#L14">image.tag</a><br/>
               (string)
               </td>
               <td>
               Image version to use for the Deployment, if not specified, it defaults to <code>.Chart.AppVersion</code>
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
""
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="settings--metricStore--retentionSeconds">
               <a href="./values.yaml#L19">settings.metricStore.retentionSeconds</a><br/>
               (int)
               </td>
               <td>
               how long the metrics should be kept in the short term (in memory) storage
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
120
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="settings--metricStore--lazySeries">
               <a href="./values.yaml#L23">settings.metricStore.lazySeries</a><br/>
               (bool)
               </td>
               <td>
               if enabled, no metrics will be stored until there is a request for such metric from KEDA operator.
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
false
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="settings--metricStore--lazyAggregates">
               <a href="./values.yaml#L27">settings.metricStore.lazyAggregates</a><br/>
               (bool)
               </td>
               <td>
               if enabled, the only aggregate that will be calculated on the fly is the one referenced in the metric query  (by default, we calculate and store all of them - sum, rate, min, max, etc.)
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
false
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="settings--metricStore--errIfNotFound">
               <a href="./values.yaml#L30">settings.metricStore.errIfNotFound</a><br/>
               (bool)
               </td>
               <td>
               when enabled, the scaler will be returning error to KEDA's <code>GetMetrics()</code> call
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
false
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="settings--metricStore--valueIfNotFound">
               <a href="./values.yaml#L33">settings.metricStore.valueIfNotFound</a><br/>
               (float)
               </td>
               <td>
               default value, that is reported in case of error or if the value is not in the mem store
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
0
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="settings--isActivePollingIntervalMilliseconds">
               <a href="./values.yaml#L36">settings.isActivePollingIntervalMilliseconds</a><br/>
               (int)
               </td>
               <td>
               how often (in milliseconds) should the IsActive method be tried
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
500
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="settings--internalMetricsPort">
               <a href="./values.yaml#L39">settings.internalMetricsPort</a><br/>
               (int)
               </td>
               <td>
               internal (mostly golang) metrics will be exposed on <code>:8080/metrics</code>
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
8080
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="settings--restApiPort">
               <a href="./values.yaml#L42">settings.restApiPort</a><br/>
               (int)
               </td>
               <td>
               port where rest api should be listening
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
9090
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="settings--logs--logLvl">
               <a href="./values.yaml#L47">settings.logs.logLvl</a><br/>
               (string)
               </td>
               <td>
               Can be one of 'debug', 'info', 'error', or any integer value > 0 which corresponds to custom debug levels of increasing verbosity
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
"info"
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="settings--logs--stackTracesLvl">
               <a href="./values.yaml#L50">settings.logs.stackTracesLvl</a><br/>
               (string)
               </td>
               <td>
               one of: info, error, panic
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
"error"
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="settings--logs--noColor">
               <a href="./values.yaml#L53">settings.logs.noColor</a><br/>
               (bool)
               </td>
               <td>
               if anything else than 'false', the log will not contain colors
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
false
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="settings--logs--noBanner">
               <a href="./values.yaml#L56">settings.logs.noBanner</a><br/>
               (bool)
               </td>
               <td>
               if anything else than 'false', the log will not print the ascii logo
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
false
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="deploymentStrategy">
               <a href="./values.yaml#L59">deploymentStrategy</a><br/>
               (string)
               </td>
               <td>
               one of: RollingUpdate, Recreate (<a href="https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#strategy">details</a>)
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
"RollingUpdate"
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="deployScaler">
               <a href="./values.yaml#L62">deployScaler</a><br/>
               (bool)
               </td>
               <td>
               when disabled, the deployment with KEDA Scaler will not be rendered
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
true
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="validatingAdmissionPolicy--enabled">
               <a href="./values.yaml#L66">validatingAdmissionPolicy.enabled</a><br/>
               (bool)
               </td>
               <td>
               whether the ValidatingAdmissionPolicy and ValidatingAdmissionPolicyBinding resources should be also rendered
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
false
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="asciiArt">
               <a href="./values.yaml#L70">asciiArt</a><br/>
               (bool)
               </td>
               <td>
               should the ascii logo be printed when this helm chart is installed
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
true
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="imagePullSecrets">
               <a href="./values.yaml#L73">imagePullSecrets</a><br/>
               (list)
               </td>
               <td>
               <a href="https://kubernetes.io/docs/concepts/containers/images/#specifying-imagepullsecrets-on-a-pod">details</a>
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
[]
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="serviceAccount--create">
               <a href="./values.yaml#L79">serviceAccount.create</a><br/>
               (bool)
               </td>
               <td>
               should the service account be also created and linked in the deployment
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
true
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="serviceAccount--annotations">
               <a href="./values.yaml#L82">serviceAccount.annotations</a><br/>
               (object)
               </td>
               <td>
               further custom annotation that will be added on the service account
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
{}
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="serviceAccount--name">
               <a href="./values.yaml#L84">serviceAccount.name</a><br/>
               (string)
               </td>
               <td>
               name of the service account, defaults to <code>otel-add-on.fullname</code> ~ release name if not overriden
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
""
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="podAnnotations">
               <a href="./values.yaml#L87">podAnnotations</a><br/>
               (object)
               </td>
               <td>
               additional custom pod annotations that will be used for pod
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
{}
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="podLabels">
               <a href="./values.yaml#L90">podLabels</a><br/>
               (object)
               </td>
               <td>
               additional custom pod labels that will be used for pod
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
{}
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="podSecurityContext">
               <a href="./values.yaml#L93">podSecurityContext</a><br/>
               (object)
               </td>
               <td>
               <a href="https://kubernetes.io/docs/tasks/configure-pod-container/security-context/#set-the-security-context-for-a-pod">details</a>
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
{}
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="securityContext--readOnlyRootFilesystem">
               <a href="./values.yaml#L100">securityContext.readOnlyRootFilesystem</a><br/>
               (bool)
               </td>
               <td>
               <a href="https://kubernetes.io/docs/tasks/configure-pod-container/security-context/">details</a>
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
true
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="securityContext--runAsNonRoot">
               <a href="./values.yaml#L102">securityContext.runAsNonRoot</a><br/>
               (bool)
               </td>
               <td>
               <a href="https://kubernetes.io/docs/tasks/configure-pod-container/security-context/#implicit-group-memberships-defined-in-etc-group-in-the-container-image">details</a>
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
true
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="securityContext--runAsUser">
               <a href="./values.yaml#L104">securityContext.runAsUser</a><br/>
               (int)
               </td>
               <td>
               <a href="https://kubernetes.io/docs/tasks/configure-pod-container/security-context/#implicit-group-memberships-defined-in-etc-group-in-the-container-image">details</a>
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
1000
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="service--type">
               <a href="./values.yaml#L109">service.type</a><br/>
               (string)
               </td>
               <td>
               Under this service, the otel add on needs to be reachable by KEDA operator and OTel collector (<a href="https://kubernetes.io/docs/concepts/services-networking/service/#publishing-services-service-types">details</a>)
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
"ClusterIP"
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="service--otlpReceiverPort">
               <a href="./values.yaml#L111">service.otlpReceiverPort</a><br/>
               (int)
               </td>
               <td>
               OTLP receiver will be opened on this port. OTel exporter configured in the OTel collector needs to have this value set.
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
4317
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="service--kedaExternalScalerPort">
               <a href="./values.yaml#L113">service.kedaExternalScalerPort</a><br/>
               (int)
               </td>
               <td>
               KEDA external scaler will be opened on this port. ScaledObject's <code>.spec.triggers[].metadata.scalerAddress</code> needs to be set to this svc and this port.
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
4318
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="resources--limits--cpu">
               <a href="./values.yaml#L118">resources.limits.cpu</a><br/>
               (string)
               </td>
               <td>
               cpu limit for the pod, enforced by cgroups (<a href="https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/">details</a>)
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
"500m"
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="resources--limits--memory">
               <a href="./values.yaml#L120">resources.limits.memory</a><br/>
               (string)
               </td>
               <td>
               memory limit for the pod, used by oomkiller (<a href="https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/">details</a>)
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
"256Mi"
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="resources--requests--cpu">
               <a href="./values.yaml#L123">resources.requests.cpu</a><br/>
               (string)
               </td>
               <td>
               cpu request for the pod, used by k8s scheduler (<a href="https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/">details</a>)
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
"500m"
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="resources--requests--memory">
               <a href="./values.yaml#L125">resources.requests.memory</a><br/>
               (string)
               </td>
               <td>
               memory request for the pod, used by k8s scheduler (<a href="https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/">details</a>)
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
"128Mi"
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="nodeSelector">
               <a href="./values.yaml#L139">nodeSelector</a><br/>
               (object)
               </td>
               <td>
               <a href="https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#nodeselector">details</a>
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
{}
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="tolerations">
               <a href="./values.yaml#L142">tolerations</a><br/>
               (list)
               </td>
               <td>
               <a href="https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/">details</a>
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
[]
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="affinity">
               <a href="./values.yaml#L145">affinity</a><br/>
               (object)
               </td>
               <td>
               <a href="https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#affinity-and-anti-affinity">details</a>
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
{}
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="kubectlImage">
               <a href="./values.yaml#L148">kubectlImage</a><br/>
               (object)
               </td>
               <td>
               helper container image that creates the OpenTelemetryCollector CR as post-install helm hook
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
{
  "pullPolicy": "Always",
  "pullSecrets": [],
  "repository": "ghcr.io/kedify/kubectl",
  "tag": "v1.33.1"
}
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="otelOperatorCrDefaultTemplate">
               <a href="./values.yaml#L155">otelOperatorCrDefaultTemplate</a><br/>
               (object)
               </td>
               <td>
               default template for OpenTelemetryCollector CR. Override the specifics under <code>otelOperatorCrs</code> section
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
{
  "alternateExtensions": {},
  "alternateOtelConfig": {},
  "alternateReceivers": {},
  "clusterRole": {
    "annotations": {},
    "create": false,
    "rules": []
  },
  "debug": false,
  "env": [],
  "envFrom": [],
  "includeMetrics": [],
  "metricsgeneration": {
    "rules": []
  },
  "mode": "sidecar",
  "prometheusScrapeConfigs": [
    {
      "job_name": "otel-collector",
      "scrape_interval": "3s",
      "static_configs": [
        {
          "targets": [
            "0.0.0.0:8080"
          ]
        }
      ]
    }
  ],
  "resources": {
    "limits": {
      "cpu": "400m",
      "memory": "128Mi"
    },
    "requests": {
      "cpu": "200m",
      "memory": "64Mi"
    }
  },
  "serviceAccount": {
    "annotations": {},
    "create": false,
    "name": ""
  },
  "targetAllocator": {
    "enabled": true,
    "prometheusCR": {
      "allowNamespaces": [],
      "denyNamespaces": [],
      "enabled": true,
      "podMonitorSelector": {},
      "serviceMonitorSelector": {}
    },
    "serviceAccount": "otel-prom-reader"
  },
  "targetAllocatorClusterRoles": [
    "kube-prometheus-stack-operator",
    "kube-prometheus-stack-prometheus"
  ],
  "targetAllocatorEnabled": false
}
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="otelOperatorCrDefaultTemplate--debug">
               <a href="./values.yaml#L157">otelOperatorCrDefaultTemplate.debug</a><br/>
               (bool)
               </td>
               <td>
               container image for post-install helm hook that help with OpenTelemetryCollector CR installation
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
false
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="otelOperatorCrDefaultTemplate--mode">
               <a href="./values.yaml#L160">otelOperatorCrDefaultTemplate.mode</a><br/>
               (string)
               </td>
               <td>
               how the otel collector should be deployed: sidecar, statefulset, deployment, daemonset
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
"sidecar"
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="otelOperatorCrDefaultTemplate--targetAllocatorEnabled">
               <a href="./values.yaml#L163">otelOperatorCrDefaultTemplate.targetAllocatorEnabled</a><br/>
               (bool)
               </td>
               <td>
               whether TargetAllocator feature (Prometheus Custom Resources for service discovery) should be enabled (<a href="https://github.com/open-telemetry/opentelemetry-operator?tab=readme-ov-file#target-allocator">details</a>) make sure the mode is not set to <code>sidecar</code> when this is enabled
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
false
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="otelOperatorCrDefaultTemplate--targetAllocatorClusterRoles">
               <a href="./values.yaml#L165">otelOperatorCrDefaultTemplate.targetAllocatorClusterRoles</a><br/>
               (list)
               </td>
               <td>
               list of existing cluster roles that will be bound to the service account (in order to be able to work with <code>{Pod,Service}Monitor</code> CRD)
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
[
  "kube-prometheus-stack-operator",
  "kube-prometheus-stack-prometheus"
]
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="otelOperatorCrDefaultTemplate--targetAllocator--prometheusCR--serviceMonitorSelector">
               <a href="./values.yaml#L174">otelOperatorCrDefaultTemplate.targetAllocator.prometheusCR.serviceMonitorSelector</a><br/>
               (object)
               </td>
               <td>
               further narrow the ServiceMonitor CRs
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
{}
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="otelOperatorCrDefaultTemplate--targetAllocator--prometheusCR--podMonitorSelector">
               <a href="./values.yaml#L178">otelOperatorCrDefaultTemplate.targetAllocator.prometheusCR.podMonitorSelector</a><br/>
               (object)
               </td>
               <td>
               further narrow the ServiceMonitor CRs
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
{}
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="otelOperatorCrDefaultTemplate--resources">
               <a href="./values.yaml#L199">otelOperatorCrDefaultTemplate.resources</a><br/>
               (object)
               </td>
               <td>
               resources for the OTel collector container
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
{
  "limits": {
    "cpu": "400m",
    "memory": "128Mi"
  },
  "requests": {
    "cpu": "200m",
    "memory": "64Mi"
  }
}
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="otelOperatorCrDefaultTemplate--alternateOtelConfig">
               <a href="./values.yaml#L208">otelOperatorCrDefaultTemplate.alternateOtelConfig</a><br/>
               (object)
               </td>
               <td>
               free form OTel configuration that will be used for the OpenTelemetryCollector CR (no checks) this is mutually exclusive w/ all the following options
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
{}
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="otelOperatorCrDefaultTemplate--prometheusScrapeConfigs">
               <a href="./values.yaml#L212">otelOperatorCrDefaultTemplate.prometheusScrapeConfigs</a><br/>
               (list)
               </td>
               <td>
               static targets for prometheus receiver, this needs to take into account the deployment mode of the collector (127.0.0.1 in case of a sidecar mode will mean something else than for statefulset mode)
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
[
  {
    "job_name": "otel-collector",
    "scrape_interval": "3s",
    "static_configs": [
      {
        "targets": [
          "0.0.0.0:8080"
        ]
      }
    ]
  }
]
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="otelOperatorCrDefaultTemplate--alternateReceivers">
               <a href="./values.yaml#L218">otelOperatorCrDefaultTemplate.alternateReceivers</a><br/>
               (object)
               </td>
               <td>
               mutually exclusive with prometheusScrapeConfigs option
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
{}
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="otelOperatorCrDefaultTemplate--includeMetrics">
               <a href="./values.yaml#L222">otelOperatorCrDefaultTemplate.includeMetrics</a><br/>
               (list)
               </td>
               <td>
               if not empty, only following metrics will be sent. This translates to filter/metrics processor. Empty array means include all.
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
[]
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="otelOperatorCrs">
               <a href="./values.yaml#L251">otelOperatorCrs</a><br/>
               (list)
               </td>
               <td>
               create also OpenTelemetryCollector CRs that will be reconciled by OTel Operator it takes all the default settings defined in <code>otelOperatorCrDefaultTemplate</code> and allows overriding them
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
[
  {
    "enabled": false,
    "name": "",
    "namespace": ""
  },
  {
    "enabled": false,
    "mode": "deployment",
    "name": "target-allocator",
    "targetAllocatorEnabled": true
  }
]
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="otelOperatorCrs[0]">
               <a href="./values.yaml#L253">otelOperatorCrs[0]</a><br/>
               (object)
               </td>
               <td>
               if enabled, the OpenTelemetryCollector CR will be created using post-install hook job_name
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
{
  "enabled": false,
  "name": "",
  "namespace": ""
}
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="otelOperatorCrs[0]--name">
               <a href="./values.yaml#L255">otelOperatorCrs[0].name</a><br/>
               (string)
               </td>
               <td>
               name of the OpenTelemetryCollector CR. If left empty, the release name will be used.
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
""
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="otelOperatorCrs[0]--namespace">
               <a href="./values.yaml#L257">otelOperatorCrs[0].namespace</a><br/>
               (string)
               </td>
               <td>
               in what k8s namespace the OpenTelemetryCollector CR should be created. If left empty, the release namespace will be used.
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
""
</pre>
</div>
               </td>
          </tr>
          <tr>
               <td id="otelCollector--mode">
               <a href="./values.yaml#L288">otelCollector.mode</a><br/>
               (string)
               </td>
               <td>
               Valid values are "daemonset", "deployment", "sidecar" and "statefulset"
               </td>
               <td>
                    <div style="max-width: 200px;">
<pre lang="json">
"deployment"
</pre>
</div>
               </td>
          </tr>
     </tbody>
</table>

<!-- uncomment this for markdown style (use either valuesTableHtml or valuesSection)
(( template "chart.valuesSection" . )) -->
