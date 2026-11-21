{{/* HA names follow the existing Service and workload naming precedence. */}}
{{- define "otel-add-on.headlessServiceName" -}}
{{- $name := printf "%s-h" (.Values.service.name | default .Values.fullnameOverride | default .Release.Name) -}}
{{- if gt (len $name) 63 -}}
{{- fail "HA ingestion Service name exceeds 63 characters; shorten service.name or fullnameOverride to leave room for -h" -}}
{{- end -}}
{{- $name -}}
{{- end -}}

{{- define "otel-add-on.haWorkloadName" -}}
{{- $name := .Values.fullnameOverride | default .Release.Name -}}
{{- $last := sub (int ((.Values.settings.ha | default dict).replicas | default 3)) 1 -}}
{{- if gt (len (printf "%s-%d" $name $last)) 63 -}}
{{- fail "HA Pod hostname exceeds 63 characters; shorten fullnameOverride to leave room for the replica ordinal" -}}
{{- end -}}
{{- $name -}}
{{- end -}}

{{- define "otel-add-on.replicaEndpoint" -}}
{{- $root := .root -}}
{{- printf "%s-%d.%s.%s.svc:%v" (include "otel-add-on.haWorkloadName" $root) (int .ordinal) (include "otel-add-on.headlessServiceName" $root) ($root.Values.namespace | default $root.Release.Namespace) $root.Values.service.otlpReceiverPort -}}
{{- end -}}

{{/* Replace a scaler exporter with independently queued exporters in metrics pipelines.
     Preserve its TLS/auth/timeout settings and every unrelated pipeline/exporter. */}}
{{- define "otel-add-on.haCollectorConfig" -}}
{{- $root := .root -}}
{{- $config := deepCopy .config -}}
{{- $source := .exporter -}}
{{- $original := get ($config.exporters | default dict) $source -}}
{{- $used := false -}}
{{- range $name, $pipeline := $config.service.pipelines -}}
{{- if and (eq (first (splitList "/" $name)) "metrics") (has $source ($pipeline.exporters | default list)) -}}
{{- $used = true -}}
{{- end -}}
{{- end -}}
{{- if and $original $used -}}
{{- $replicas := list -}}
{{- $prefix := $source -}}
{{- if not (contains "/" $prefix) -}}{{- $prefix = printf "%s/scaler" $prefix -}}{{- end -}}
{{- range $ordinal := until (int (($root.Values.settings.ha | default dict).replicas | default 3)) -}}
{{- $name := printf "%s-%d" $prefix $ordinal -}}
{{- if hasKey $config.exporters $name -}}
{{- fail (printf "HA exporter %q conflicts with an existing exporter" $name) -}}
{{- end -}}
{{- $exporter := deepCopy $original -}}
{{- $_ := set $exporter "endpoint" (include "otel-add-on.replicaEndpoint" (dict "root" $root "ordinal" $ordinal)) -}}
{{/* Retain the original logical TLS identity when dialing a Pod directly. */}}
{{- $tls := $exporter.tls | default dict -}}
{{- $originalEndpoint := $original.endpoint | default "" -}}
{{- if hasPrefix "https://" $originalEndpoint -}}
{{- $_ := set $tls "insecure" false -}}
{{- $_ := set $exporter "tls" $tls -}}
{{- end -}}
{{- if and $originalEndpoint (or (not $tls.insecure) $tls.ca_file $tls.ca_pem) (not $exporter.authority) (not (hasKey $tls "server_name_override")) -}}
{{- $address := trimPrefix "dns:///" $originalEndpoint -}}
{{- if not (contains "://" $address) -}}{{- $address = printf "https://%s" $address -}}{{- end -}}
{{- $hostname := (urlParse $address).hostname -}}
{{- if $hostname -}}
{{- $_ := set $tls "server_name_override" $hostname -}}
{{- $_ := set $exporter "tls" $tls -}}
{{- end -}}
{{- end -}}
{{- $_ := set $exporter "sending_queue" (mergeOverwrite ($exporter.sending_queue | default dict) (dict "enabled" true "wait_for_result" false "block_on_overflow" false)) -}}
{{- $_ := set $exporter "retry_on_failure" (mergeOverwrite ($exporter.retry_on_failure | default dict) (dict "enabled" true)) -}}
{{- $_ := set $config.exporters $name $exporter -}}
{{- $replicas = append $replicas $name -}}
{{- end -}}
{{- $keepOriginal := false -}}
{{- range $name, $pipeline := $config.service.pipelines -}}
{{- $exporters := list -}}
{{- range $exporter := $pipeline.exporters -}}
{{- if and (eq (first (splitList "/" $name)) "metrics") (eq $exporter $source) -}}
{{- $exporters = concat $exporters $replicas -}}
{{- else -}}
{{- if eq $exporter $source -}}{{- $keepOriginal = true -}}{{- end -}}
{{- $exporters = append $exporters $exporter -}}
{{- end -}}
{{- end -}}
{{- $_ := set $pipeline "exporters" ($exporters | uniq) -}}
{{- end -}}
{{- if not $keepOriginal -}}{{- $_ := unset $config.exporters $source -}}{{- end -}}
{{- end -}}
{{- toYaml $config -}}
{{- end -}}
