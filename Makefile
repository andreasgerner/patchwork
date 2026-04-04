IMG ?= ghcr.io/andreasgerner/patchwork:latest
CONTAINER_TOOL ?= docker

# Tool versions
CONTROLLER_TOOLS_VERSION ?= v0.20.1
KUSTOMIZE_VERSION ?= v5.6.0
ENVTEST_VERSION ?= release-0.20
GOLANGCI_LINT_VERSION ?= v2.1.6

# Tool binaries
LOCALBIN ?= $(shell pwd)/bin
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
KUSTOMIZE ?= $(LOCALBIN)/kustomize
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "Usage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} /^[a-zA-Z_-]+:.*?##/ {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate CRD and RBAC manifests
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd paths="./..." output:crd:artifacts:config=config/crd/bases output:rbac:artifacts:config=config/rbac

.PHONY: generate
generate: controller-gen ## Generate DeepCopy methods
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt
	go fmt ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: lint
lint: golangci-lint ## Run golangci-lint
	$(GOLANGCI_LINT) run

.PHONY: test
test: manifests generate fmt vet envtest ## Run tests
	KUBEBUILDER_ASSETS="$$($(ENVTEST) use $(ENVTEST_VERSION) --bin-dir $(LOCALBIN) -p path)" go test $$(go list ./... | grep -v /e2e) -coverprofile cover.out

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary
	go build -o bin/manager cmd/main.go

.PHONY: run
run: manifests generate fmt vet ## Run controller locally
	go run ./cmd/main.go

.PHONY: docker-build
docker-build: ## Build Docker image
	$(CONTAINER_TOOL) build -t $(IMG) .

.PHONY: docker-push
docker-push: ## Push Docker image
	$(CONTAINER_TOOL) push $(IMG)

.PHONY: docker-buildx
docker-buildx: ## Build and push multi-arch Docker image
	- $(CONTAINER_TOOL) buildx create --use
	$(CONTAINER_TOOL) buildx build --push --platform linux/arm64,linux/amd64 --tag $(IMG) -f Dockerfile .

##@ Deployment

.PHONY: install
install: manifests kustomize ## Install CRDs into the cluster
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the cluster
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller via kustomize
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: kustomize ## Undeploy controller via kustomize
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found -f -

.PHONY: build-installer
build-installer: manifests kustomize ## Generate consolidated install manifest
	mkdir -p dist
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/default > dist/install.yaml

##@ Helm

.PHONY: helm-install
helm-install: manifests ## Install via Helm
	cp config/crd/bases/patchwork.io_patchrules.yaml charts/patchwork/crds/patchrules.patchwork.io.yaml
	helm install patchwork charts/patchwork --namespace patchwork-system --create-namespace

.PHONY: helm-uninstall
helm-uninstall: ## Uninstall via Helm
	helm uninstall patchwork -n patchwork-system

##@ Dependencies

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN)
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: kustomize
kustomize: $(KUSTOMIZE)
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: envtest
envtest: $(ENVTEST)
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT)
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

$(LOCALBIN):
	mkdir -p $(LOCALBIN)

# go-install-tool will install a Go tool in $(LOCALBIN) if it doesn't exist.
define go-install-tool
@[ -f $(1) ] || { \
set -e; \
package=$(2)@$(3); \
echo "Installing $${package}"; \
GOBIN=$(LOCALBIN) go install $${package}; \
}
endef
