// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package web embeds the static dashboard UI assets so they ship inside the
// single Go binary (no separate build step or asset volume).
package web

import "embed"

// FS holds the embedded dashboard UI assets.
//
//go:embed index.html app.js style.css
var FS embed.FS
