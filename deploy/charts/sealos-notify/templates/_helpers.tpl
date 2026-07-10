{{- define "sealos-notify.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "sealos-notify.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "sealos-notify.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ include "sealos-notify.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "sealos-notify.selectorLabels" -}}
app.kubernetes.io/name: {{ include "sealos-notify.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "sealos-notify.appSelectorLabels" -}}
{{ include "sealos-notify.selectorLabels" . }}
app.kubernetes.io/component: server
{{- end -}}

{{- define "sealos-notify.postgresql.fullname" -}}
{{- printf "%s-postgresql" (include "sealos-notify.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "sealos-notify.database.host" -}}
{{- if .Values.externalDatabase.host -}}
{{- .Values.externalDatabase.host -}}
{{- else -}}
{{- include "sealos-notify.postgresql.fullname" . -}}
{{- end -}}
{{- end -}}

{{- define "sealos-notify.database.port" -}}
{{- if .Values.externalDatabase.host -}}
{{- .Values.externalDatabase.port -}}
{{- else -}}
{{- .Values.postgresql.port -}}
{{- end -}}
{{- end -}}

{{- define "sealos-notify.database.user" -}}
{{- if .Values.externalDatabase.host -}}
{{- .Values.externalDatabase.user -}}
{{- else -}}
{{- .Values.postgresql.username -}}
{{- end -}}
{{- end -}}

{{- define "sealos-notify.database.name" -}}
{{- if .Values.externalDatabase.host -}}
{{- .Values.externalDatabase.database -}}
{{- else -}}
{{- .Values.postgresql.database -}}
{{- end -}}
{{- end -}}

{{- define "sealos-notify.database.sslMode" -}}
{{- if .Values.externalDatabase.host -}}
{{- .Values.externalDatabase.sslMode -}}
{{- else -}}
disable
{{- end -}}
{{- end -}}

{{- define "sealos-notify.host" -}}
{{- if .Values.ingress.host -}}
{{- .Values.ingress.host -}}
{{- else -}}
{{- printf "%s.%s" (include "sealos-notify.fullname" .) .Values.sealos.cloudDomain -}}
{{- end -}}
{{- end -}}

{{- define "sealos-notify.scheme" -}}
{{- if eq (toString .Values.sealos.disableHttps) "true" -}}http{{- else -}}https{{- end -}}
{{- end -}}

{{- define "sealos-notify.externalPort" -}}
{{- if eq (toString .Values.sealos.disableHttps) "true" -}}
{{- .Values.sealos.httpPort -}}
{{- else -}}
{{- .Values.sealos.cloudPort -}}
{{- end -}}
{{- end -}}

{{- define "sealos-notify.url" -}}
{{- $port := include "sealos-notify.externalPort" . -}}
{{- if $port -}}
{{- printf "%s://%s:%s" (include "sealos-notify.scheme" .) (include "sealos-notify.host" .) $port -}}
{{- else -}}
{{- printf "%s://%s" (include "sealos-notify.scheme" .) (include "sealos-notify.host" .) -}}
{{- end -}}
{{- end -}}
