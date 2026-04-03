IMG ?= patchwork:latest
CONTROLLER_GEN ?= $(shell which controller-gen 2>/dev/null)

.PHONY: build
build: ## Build the operator binary
	go build -o bin/patchwork .

.PHONY: test
test: ## Run tests
	go test ./... -v

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: generate
generate: controller-gen ## Generate deepcopy methods and CRD manifests
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./api/..."
	$(CONTROLLER_GEN) crd paths="./api/..." output:crd:artifacts:config=config/crd/bases

.PHONY: controller-gen
controller-gen: ## Install controller-gen if not present
	@test -n "$(CONTROLLER_GEN)" || { \
		echo "Installing controller-gen..."; \
		go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.20.1; \
	}

.PHONY: docker-build
docker-build: ## Build docker image
	docker build -t $(IMG) .

.PHONY: docker-push
docker-push: ## Push docker image
	docker push $(IMG)

.PHONY: install
install: generate ## Install CRD into the cluster
	kubectl apply -f config/crd/bases/

.PHONY: uninstall
uninstall: ## Remove CRD from the cluster
	kubectl delete -f config/crd/bases/

.PHONY: run
run: build ## Run locally against the configured cluster
	./bin/patchwork

.PHONY: clean
clean: ## Clean build artifacts
	rm -rf bin/
