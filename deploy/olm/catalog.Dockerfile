# File-based OLM catalog for NodeEcho.
#   docker build -f deploy/olm/catalog.Dockerfile -t nodeecho-catalog:0.1.1 deploy/olm/catalog
#
# opm and grpc_health_probe are built from the module in catalog/opm rather
# than taken from the upstream opm image, which ships them built with an older
# Go and dependencies that carry fixable vulnerabilities. opm is built without
# cgo: its SQLite backend is unused by a file-based catalog.
FROM --platform=$BUILDPLATFORM golang:1.27.4@sha256:5e8afd493a7124a8586e134d9b532731161d4b9becf9dee5a5956b107426f93d AS tools
ARG BUILDARCH
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ENV CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-trimpath
WORKDIR /src
COPY opm/go.mod opm/go.sum ./
RUN go mod download
RUN GOOS=linux GOARCH=${BUILDARCH} go build -tags=json1,netgo,osusergo,containers_image_openpgp -ldflags="-s -w" -o /out/build/opm github.com/operator-framework/operator-registry/cmd/opm \
 && GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -tags=json1,netgo,osusergo,containers_image_openpgp -ldflags="-s -w" -o /out/target/opm github.com/operator-framework/operator-registry/cmd/opm \
 && GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o /out/target/grpc_health_probe github.com/grpc-ecosystem/grpc-health-probe

# The build platform's opm validates the catalog and fills the cache.
FROM --platform=$BUILDPLATFORM golang:1.27.4@sha256:5e8afd493a7124a8586e134d9b532731161d4b9becf9dee5a5956b107426f93d AS cache
COPY --from=tools /out/build/opm /bin/opm
COPY nodeecho /configs/nodeecho
RUN ["/bin/opm", "serve", "/configs", "--cache-dir=/tmp/cache", "--cache-only"]
# opm locks the cache when it starts. OLM runs it as 1001, OpenShift as an
# arbitrary user in group 0: both must be able to write to it.
RUN chmod -R g=u /tmp/cache

FROM gcr.io/distroless/static:nonroot@sha256:8ce6dd87e9823c5b48dcfb2c8604714ccb351ce59607327bcc4957448da279b5

LABEL operators.operatorframework.io.index.configs.v1=/configs

COPY --from=tools /out/target/opm /bin/opm
COPY --from=tools /out/target/grpc_health_probe /bin/grpc_health_probe
COPY nodeecho /configs/nodeecho
COPY --from=cache --chown=1001:0 /tmp/cache /tmp/cache
USER 1001
ENTRYPOINT ["/bin/opm"]
CMD ["serve", "/configs", "--cache-dir=/tmp/cache"]
