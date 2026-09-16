{{/*
Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: Apache-2.0
*/}}

{{- define "dsx-dashboard.name" -}}
dsx-dashboard
{{- end -}}

{{- define "dsx-dashboard.labels" -}}
app.kubernetes.io/name: {{ include "dsx-dashboard.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "dsx-dashboard.selectorLabels" -}}
app.kubernetes.io/name: {{ include "dsx-dashboard.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
