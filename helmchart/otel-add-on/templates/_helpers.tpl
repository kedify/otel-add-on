{{/*
Expand the name of the chart.
*/}}
{{- define "otel-add-on.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "otel-add-on.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "otel-add-on.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "otel-add-on.labels" -}}
helm.sh/chart: {{ include "otel-add-on.chart" . }}
{{ include "otel-add-on.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "otel-add-on.selectorLabels" -}}
app.kubernetes.io/name: {{ include "otel-add-on.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "otel-add-on.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "otel-add-on.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Merge the default settings
*/}}
{{- define "operatorCrs" -}}
    {{- if .Values.otelOperatorCrs }}
        {{- $allmerged := list }}
        {{- $merged := dict }}
        {{- range .Values.otelOperatorCrs }}
          {{- if .enabled }}
          {{- $merged = mergeOverwrite (deepCopy $.Values.otelOperatorCrDefaultTemplate) . }}
          {{- $allmerged = append $allmerged $merged }}
          {{- end }}
        {{- end }}
        {{- $allmerged | toJson  -}}
    {{- end }}
{{- end }}

{{/*
Check if at least one otelOperatorCrs has .enabled set to true
*/}}
{{- define "atLeastOneOperatorCr" -}}
    {{- if .Values.otelOperatorCrs }}
        {{- $enablements := list }}
        {{- range .Values.otelOperatorCrs }}
          {{- $enablements = append $enablements .enabled }}
        {{- end }}
        {{- if has true $enablements }}
            {{- printf "true" -}}
        {{- end }}
    {{- end }}
{{- end }}
