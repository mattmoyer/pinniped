# syntax = docker/dockerfile:1.0-experimental

# Copyright 2020-2021 the Pinniped contributors. All Rights Reserved.
# SPDX-License-Identifier: Apache-2.0

FROM golang:1.16.2 as build-env

WORKDIR /work
COPY . .
ARG GOPROXY

# Build the executable binary (CGO_ENABLED=0 means static linking)
# Pass in GOCACHE (build cache) and GOMODCACHE (module cache) so they
# can be re-used between image builds.
RUN \
  --mount=type=cache,target=/cache/gocache \
  --mount=type=cache,target=/cache/gomodcache \
  mkdir out && \
  GOCACHE=/cache/gocache \
  GOMODCACHE=/cache/gomodcache \
  CGO_ENABLED=0 \
  GOOS=linux \
  GOARCH=amd64 \
  go build -v -ldflags "$(hack/get-ldflags.sh)" -o out \
    ./cmd/pinniped-concierge/... \
    ./cmd/pinniped-supervisor/... \
    ./cmd/local-user-authenticator/...

# Use a distroless runtime image with CA certificates, timezone data, and not much else.
FROM gcr.io/distroless/static:nonroot@sha256:76c39a6f76890f8f8b026f89e081084bc8c64167d74e6c93da7a053cb4ccb5dd

# Copy the binaries from the build-env stage.
COPY --from=build-env /work/out/ /usr/local/bin/

# Document the ports
EXPOSE 8080 8443

# Run as non-root for security posture
USER 1001:1001

# Set the entrypoint
ENTRYPOINT ["/usr/local/bin/pinniped-concierge"]
