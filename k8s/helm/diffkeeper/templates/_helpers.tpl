{{- define "diffkeeper.serviceAccountName" -}}
{{- if .Values.serviceAccount.name -}}
{{ .Values.serviceAccount.name }}
{{- else -}}
{{ printf "%s-sa" .Release.Name }}
{{- end -}}
{{- end -}}
