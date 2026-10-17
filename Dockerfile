# NodeEcho manager image.
#
#   docker build -t nodeecho:latest .

# The builder runs on the build platform and cross-compiles for each target
# (linux/amd64, linux/arm64 with buildx), without emulation.
FROM --platform=$BUILDPLATFORM golang:1.27.2@sha256:820153096fda4de6ac1b7194d12e416b7dd8f4e9a33d17e0345b111b945162d8 AS builder
WORKDIR /workspace

# Module files first for dependency caching.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd cmd
COPY internal internal

ARG VERSION=development
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
    -ldflags "-X main.version=${VERSION}" \
    -o /workspace/nodeecho ./cmd/nodeecho

FROM gcr.io/distroless/static:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
COPY --from=builder /workspace/nodeecho /
USER 65532:65532
ENTRYPOINT ["/nodeecho"]
