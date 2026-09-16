# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

################################################################################
### Build arguments
################################################################################

ARG BUILDER_IMG=golang
ARG BUILDER_TAG=1.26.5

ARG FINAL_IMG=gcr.io/distroless/static-debian12
ARG FINAL_TAG=nonroot

ARG SERVICE_PORT=8080

ARG LABEL_CREATED=unknown
ARG LABEL_REVISION=unknown
ARG LABEL_VERSION=dev

################################################################################
### Stage: Builder
################################################################################

FROM ${BUILDER_IMG}:${BUILDER_TAG} AS builder

WORKDIR /app

# Copy source code
COPY . .

# Build the inference binary.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o inference ./cmd/inference

################################################################################
### Stage: Final
################################################################################

FROM ${FINAL_IMG}:${FINAL_TAG}

ARG LABEL_VERSION
ARG LABEL_CREATED
ARG LABEL_REVISION

LABEL org.opencontainers.image.created="${LABEL_CREATED}" \
      org.opencontainers.image.description="DSX mock AI inference service (DSX Flex agent)" \
      org.opencontainers.image.documentation="https://github.com/NVIDIA/dsx-exchange/tree/main/local/inference-mock" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.revision="${LABEL_REVISION}" \
      org.opencontainers.image.source="https://github.com/NVIDIA/dsx-exchange/tree/main/local/inference-mock" \
      org.opencontainers.image.title="dsx-inference" \
      org.opencontainers.image.url="https://github.com/NVIDIA/dsx-exchange/tree/main/local/inference-mock" \
      org.opencontainers.image.vendor="NVIDIA" \
      org.opencontainers.image.version="${LABEL_VERSION}"

WORKDIR /app
COPY --from=builder --chown=65532:65532 /app/inference /app/inference
USER 65532:65532

EXPOSE ${SERVICE_PORT}

ENTRYPOINT ["./inference"]
