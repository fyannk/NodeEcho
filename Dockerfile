# NodeEcho manager image.
#
#   docker build -t nodeecho:latest .

# The builder runs on the build platform and cross-compiles for each target
# (linux/amd64, linux/arm64 with buildx), without emulation.
FROM --platform=$BUILDPLATFORM golang:1.27.4@sha256:5e8afd493a7124a8586e134d9b532731161d4b9becf9dee5a5956b107426f93d AS builder
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

FROM gcr.io/distroless/static:nonroot@sha256:aafe993665bf6200db38a02c11a9351e782a84b6a45e7f33b6695638c88e3931
COPY --from=builder /workspace/nodeecho /
USER 65532:65532
ENTRYPOINT ["/nodeecho"]
