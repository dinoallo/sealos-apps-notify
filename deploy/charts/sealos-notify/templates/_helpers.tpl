{{/*
Expand the chart name.
*/}}
{{- define "sealos-notify.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create the fully qualified app name.
*/}}
{{- define "sealos-notify.fullname" -}}
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
Create chart name and version labels.
*/}}
{{- define "sealos-notify.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "sealos-notify.labels" -}}
helm.sh/chart: {{ include "sealos-notify.chart" . }}
{{ include "sealos-notify.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "sealos-notify.selectorLabels" -}}
app.kubernetes.io/name: {{ include "sealos-notify.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Runtime image reference.
*/}}
{{- define "sealos-notify.image" -}}
{{- if .Values.image.digest -}}
{{- printf "%s@%s" .Values.image.repository .Values.image.digest -}}
{{- else -}}
{{- printf "%s:%s" .Values.image.repository .Values.image.tag -}}
{{- end -}}
{{- end }}

{{- define "sealos-notify.configMapName" -}}
{{- printf "%s-config" (include "sealos-notify.fullname" .) -}}
{{- end }}

{{- define "sealos-notify.authSecretName" -}}
{{- .Values.auth.secretName -}}
{{- end }}

{{- define "sealos-notify.databaseSecretName" -}}
{{- .Values.database.passwordSecretName -}}
{{- end }}

{{- define "sealos-notify.smtpSecretName" -}}
{{- .Values.providers.smtp.passwordSecretName -}}
{{- end }}

{{- define "sealos-notify.feishuAppSecretName" -}}
{{- .Values.providers.feishuApp.secretName -}}
{{- end }}

{{- define "sealos-notify.feishuWebhookSecretName" -}}
{{- .Values.providers.feishuWebhook.secretName -}}
{{- end }}
