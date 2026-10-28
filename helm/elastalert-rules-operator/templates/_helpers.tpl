{{/*
Expand the name of the chart.
*/}}
{{- define "elastalert-rules-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "elastalert-rules-operator.fullname" -}}
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
{{- define "elastalert-rules-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "elastalert-rules-operator.labels" -}}
helm.sh/chart: {{ include "elastalert-rules-operator.chart" . }}
{{ include "elastalert-rules-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "elastalert-rules-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "elastalert-rules-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "elastalert-rules-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "elastalert-rules-operator.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Targets grouped by namespace as JSON: {"<ns>": {"ConfigMap": [names], "Secret": [names]}}
*/}}
{{- define "elastalert-rules-operator.targetsByNamespace" -}}
{{- $byNs := dict -}}
{{- range .Values.targets -}}
{{- $ns := default $.Release.Namespace .namespace -}}
{{- $kind := default "ConfigMap" .kind -}}
{{- $entry := get $byNs $ns | default (dict "ConfigMap" (list) "Secret" (list)) -}}
{{- $_ := set $entry $kind (append (get $entry $kind) .name) -}}
{{- $_ := set $byNs $ns $entry -}}
{{- end -}}
{{- toJson $byNs -}}
{{- end }}

{{/*
Comma separated namespace/name list for one target kind
*/}}
{{- define "elastalert-rules-operator.targetList" -}}
{{- $kind := .kind -}}
{{- $items := list -}}
{{- range .root.Values.targets -}}
{{- if eq (default "ConfigMap" .kind) $kind -}}
{{- $items = append $items (printf "%s/%s" (default $.root.Release.Namespace .namespace) .name) -}}
{{- end -}}
{{- end -}}
{{- join "," $items -}}
{{- end }}

{{- define "elastalert-rules-operator.rolloutNamespace" -}}
{{- default .Release.Namespace .Values.elastalert.deploymentNamespace -}}
{{- end }}
