# syntax=docker/dockerfile:1

# Both bases are pinned by digest: a tag can be moved, a digest cannot.
# Dependabot updates them.
FROM golang:1.27-alpine@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS build

WORKDIR /src

# Dependencies first: this layer is cached until go.mod or go.sum changes.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download && go mod verify

COPY . .

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_TIME=unknown
ARG TARGETOS=linux
ARG TARGETARCH

# CGO off gives a static binary the distroless base can run; -trimpath keeps
# build paths out of the binary; -s -w drop the symbol table and DWARF.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
      -ldflags="-s -w \
        -X github.com/selimslab/gobase/internal/platform/version.Version=${VERSION} \
        -X github.com/selimslab/gobase/internal/platform/version.Commit=${COMMIT} \
        -X github.com/selimslab/gobase/internal/platform/version.BuildTime=${BUILD_TIME}" \
      -o /out/server ./cmd/server

# static-debian12 carries CA certificates, /etc/passwd, and tzdata — and no
# shell, package manager, or libc. Nothing to exec if the process is
# compromised.
FROM gcr.io/distroless/static-debian12@sha256:d75cdd72874d4790092fcb1b058493ecf6bb5bf2b2b897045b00ff01d91843f2

COPY --from=build /out/server /server

# nonroot is uid 65532 in this base.
USER nonroot:nonroot

EXPOSE 8080

ENV HTTP_ADDR=:8080

# No HEALTHCHECK: there is no shell to run one. The orchestrator probes
# /healthz and /readyz over HTTP, which is what those endpoints are for.
ENTRYPOINT ["/server"]
