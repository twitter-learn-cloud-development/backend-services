{{/*
Expand the name of the chart.
*/}}
{{- define "twitter.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
*/}}
{{- define "twitter.fullname" -}}
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
Common labels
*/}}
{{- define "twitter.labels" -}}
helm.sh/chart: {{ include "twitter.chart" . }}
{{ include "twitter.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "twitter.selectorLabels" -}}
app.kubernetes.io/name: {{ include "twitter.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Chart name and version as used by the chart label.
*/}}
{{- define "twitter.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Define service names for dependencies
*/}}
{{- define "twitter.mysql.host" -}}
{{- printf "%s-mysql" .Release.Name }}
{{- end }}

{{- define "twitter.redis.host" -}}
{{- printf "%s-redis-master" .Release.Name }}
{{- end }}

{{- define "twitter.rabbitmq.host" -}}
{{- printf "%s-rabbitmq" .Release.Name }}
{{- end }}

{{- define "twitter.consul.host" -}}
{{- printf "consul-server" }}
{{- end }}

{{- define "twitter.jaeger.host" -}}
{{- printf "%s-jaeger-agent" .Release.Name }}
{{- end }}
