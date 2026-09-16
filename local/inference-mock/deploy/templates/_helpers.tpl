{{/*
Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: Apache-2.0
*/}}

{{- define "dsx-inference.name" -}}
dsx-inference
{{- end -}}

{{- define "dsx-inference.labels" -}}
app.kubernetes.io/name: {{ include "dsx-inference.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "dsx-inference.selectorLabels" -}}
app.kubernetes.io/name: {{ include "dsx-inference.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
