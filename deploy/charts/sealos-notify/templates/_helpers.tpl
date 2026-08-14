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
{{- if .Values.auth.existingSecret -}}
{{- .Values.auth.existingSecret -}}
{{- else -}}
{{- .Values.auth.secretName -}}
{{- end -}}
{{- end }}

{{- define "sealos-notify.databaseSecretName" -}}
{{- if .Values.database.passwordSecretName -}}
{{- .Values.database.passwordSecretName -}}
{{- else if .Values.database.password -}}
{{- printf "%s-database" (include "sealos-notify.fullname" .) -}}
{{- end -}}
{{- end }}

{{- define "sealos-notify.smtpSecretName" -}}
{{- if .Values.providers.smtp.passwordSecretName -}}
{{- .Values.providers.smtp.passwordSecretName -}}
{{- else if .Values.providers.smtp.password -}}
{{- printf "%s-smtp" (include "sealos-notify.fullname" .) -}}
{{- end -}}
{{- end }}

{{- define "sealos-notify.feishuAppSecretName" -}}
{{- if .Values.providers.feishuApp.secretName -}}
{{- .Values.providers.feishuApp.secretName -}}
{{- else if or .Values.providers.feishuApp.appId .Values.providers.feishuApp.appSecret -}}
{{- printf "%s-feishu-app" (include "sealos-notify.fullname" .) -}}
{{- end -}}
{{- end }}
