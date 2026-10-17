# File-based OLM catalog for NodeEcho.
#   docker build -f deploy/olm/catalog.Dockerfile -t nodeecho-catalog:0.1.0 deploy/olm/catalog
FROM quay.io/operator-framework/opm:v1.64.0@sha256:a070b901663d00312ccabe06a3c04b961d7326868499fe6c4c6b97025c79f014

LABEL operators.operatorframework.io.index.configs.v1=/configs

COPY nodeecho /configs/nodeecho
RUN ["/bin/opm", "serve", "/configs", "--cache-dir=/tmp/cache", "--cache-only"]
ENTRYPOINT ["/bin/opm"]
CMD ["serve", "/configs", "--cache-dir=/tmp/cache"]
