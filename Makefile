###############################
#		CONSTANTS
###############################
SHELL       = /bin/bash
GH_REPO_ORG = kedify
VERSION 		?= main
GIT_COMMIT  ?= $(shell git rev-list -1 HEAD)
LATEST_TAG ?= $(shell git fetch --force --tags &> /dev/null ; git describe --tags $(git rev-list --tags --max-count=1))
GO_LDFLAGS="-X github.com/${GH_REPO_ORG}/otel-add-on/build.version=${VERSION} -X github.com/${GH_REPO_ORG}/otel-add-on/build.gitCommit=${GIT_COMMIT}"
BUILD_PLATFORMS ?= linux/amd64,linux/arm64

CONTAINER_IMAGE ?= ghcr.io/kedify/otel-add-on
HACK_BIN ?= bin
ARCH ?= $(shell uname -m)
ifeq ($(ARCH), x86_64)
	ARCH=amd64
endif
CGO        ?=0
TARGET_OS  ?=linux

GO_BUILD_VARS= GO111MODULE=on CGO_ENABLED=$(CGO) GOOS=$(TARGET_OS) GOARCH=$(ARCH)

###############################
#		TARGETS
###############################
all: help

.PHONY: build
build:  ## Builds the binary.
	@$(call say,Build the binary)
	${GO_BUILD_VARS} go build -ldflags $(GO_LDFLAGS) -o bin/otel-add-on .

.PHONY: run
run:  ## Runs the scaler locally.
	go run ./main.go

.PHONY: build-image
build-image: build  ## Builds the container image for current arch.
	@$(call say,Build container image $(CONTAINER_IMAGE))
	docker build . -t ${CONTAINER_IMAGE} --build-arg VERSION=${VERSION} --build-arg GIT_COMMIT=${GIT_COMMIT}

.PHONY: build-image-multiarch
build-image-multiarch:  ## Builds the container image for arm64 and amd64.
	@$(call say,Build container image $(CONTAINER_IMAGE))
	docker buildx build --output=type=registry --platform=${BUILD_PLATFORMS} . -t ${CONTAINER_IMAGE} --build-arg VERSION=${VERSION} --build-arg GIT_COMMIT=${GIT_COMMIT}

.PHONY: build-image-goreleaser
build-image-goreleaser: ## Builds the multi-arch container image using goreleaser.
	goreleaser release --skip=validate,publish,sbom --clean --snapshot

.PHONY: test
test:  ## Runs golang unit tests.
	@$(call say,Running golang unit tests)
	go test -race -v ./...

.PHONY: e2e-test
e2e-test:  ## Runs end to end tests. This will spawn a k3d cluster.
	@$(call say,Running end to end tests)
	cd e2e-tests && go test -race -v ./...

.PHONY: generate
generate: codegen-tags codegen ## Generate code DeepCopy, DeepCopyInto, and DeepCopyObject method implementations + json tags.
	@$(call say,Generate code)

.PHONY: codegen-tags
codegen-tags: gomodifytags ## Generate json tags for certain structs.
	$(GO_MODIFY_TAGS) -file rest/api.go -struct MetricDataPayload -add-tags json -transform camelcase -quiet -w
	$(GO_MODIFY_TAGS) -file rest/api.go -struct OperationResult -add-tags json -transform camelcase -quiet -w
	$(GO_MODIFY_TAGS) -file rest/api.go -struct QueryRequest -add-tags json -transform camelcase -quiet -w
	$(GO_MODIFY_TAGS) -file types/metrics.go -all -add-tags json -transform camelcase -quiet -w

.PHONY: codegen
codegen: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile='hack/boilerplate.go.txt' paths='./...'
	./hack/update-codegen.sh

.PHONY: deploy-helm
deploy-helm:  ## Deploys helm chart with otel-collector and otel scaler.
	@$(call say,Deploy helm chart to current k8s context)
	cd helmchart/otel-add-on && \
	helm dependency build && \
	helm upgrade -i kedify-otel .

.PHONY: logs
logs:
	@$(call say,logs)
	kubectl logs -lapp.kubernetes.io/name=otel-add-on --tail=-1 --follow

CONTROLLER_GEN = ${HACK_BIN}/controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	GOBIN=$(shell pwd)/bin go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.15.0

GO_MODIFY_TAGS = ${HACK_BIN}/gomodifytags
gomodifytags: ## Download gomodifytags locally if necessary.
	GOBIN=$(shell pwd)/bin go install github.com/fatih/gomodifytags@v1.17.0

.PHONY: help
help: ## Show this help.
	@egrep -h '\s##\s' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-25s\033[0m %s\n", $$1, $$2}'


###############################
#		HELPERS
###############################

.PHONY: version
version:
	@echo $(LATEST_TAG)

ifndef NO_COLOR
YELLOW=\033[0;33m
# no color
NC=\033[0m
endif

define say
echo -e "\n$(shell echo "$1  " | sed s/./=/g)\n $(YELLOW)$1$(NC)\n$(shell echo "$1  " | sed s/./=/g)"
endef

define createNs
@kubectl create namespace $1 --dry-run=client -o yaml | kubectl apply -f -
endef
