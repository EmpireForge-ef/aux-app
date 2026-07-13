{{- define "aux.name" -}}
{{- .Chart.Name -}}
{{- end -}}

{{- define "aux.fullname" -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "aux.labels" -}}
app.kubernetes.io/name: {{ include "aux.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "aux.selectorLabels" -}}
app.kubernetes.io/name: {{ include "aux.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "aux.secretName" -}}
{{- if .Values.secrets.existingSecret -}}
{{- .Values.secrets.existingSecret -}}
{{- else -}}
{{- include "aux.fullname" . -}}
{{- end -}}
{{- end -}}
