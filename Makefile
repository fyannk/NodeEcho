# NodeEcho build entry points.

IMG ?= nodeecho:latest
VERSION ?= development
GO_LDFLAGS ?= -X main.version=$(VERSION)

GOLANGCI_LINT_VERSION ?= v2.14.0
GOVULNCHECK_VERSION ?= v1.8.0

.PHONY: all
all: build

##@ Development

.PHONY: fmt
fmt: ## Run go fmt.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet.
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint and the repository checks.
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run
	./hack/check-go-version.sh

.PHONY: test
ENVTEST_K8S_VERSION ?= 1.36.2
ENVTEST = go run sigs.k8s.io/controller-runtime/tools/setup-envtest@release-0.25

test: fmt vet ## Run unit and envtest tests.
	KUBEBUILDER_ASSETS="$$($(MAKE) -s envtest-assets)" go test ./... -coverprofile cover.out

.PHONY: e2e
e2e: ## Run the end-to-end test on throwaway k3d clusters (see hack/e2e.sh).
	./hack/e2e.sh

.PHONY: envtest-assets
envtest-assets: ## Print the envtest binaries path, downloading them if needed.
	@$(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path

.PHONY: vuln
vuln: ## Scan the Go dependency graph for known vulnerabilities.
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

##@ Build

.PHONY: build
build: fmt vet ## Build the manager binary.
	go build -ldflags "$(GO_LDFLAGS)" -o bin/nodeecho ./cmd/nodeecho

.PHONY: run
run: fmt vet ## Run the manager against the current kubeconfig context.
	go run -ldflags "$(GO_LDFLAGS)" ./cmd/nodeecho

.PHONY: docker-build
docker-build: ## Build the container image.
	docker build --build-arg VERSION=$(VERSION) -t $(IMG) .

.PHONY: clean
clean: ## Remove build outputs.
	rm -rf bin dist cover.out

##@ Deployment

.PHONY: deploy
deploy: ## Deploy NodeEcho into the nodeecho-system namespace.
	kubectl apply -k config/default

.PHONY: undeploy
undeploy: ## Remove the NodeEcho deployment.
	kubectl delete -k config/default

.PHONY: deploy-policy
deploy-policy: ## Install the optional admission policy reserving nodeecho.fyannk.dev/ keys.
	kubectl apply -k config/policy

##@ Help

.PHONY: help
help: ## Show this help.
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  %-15s %s\n", $$1, $$2 } /^##@/ { printf "\n%s\n", substr($$0, 5) }' $(MAKEFILE_LIST)
