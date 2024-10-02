###############################
#		CONSTANTS
###############################
SHELL       = /bin/bash
GH_REPO_ORG = kedify
VERSION 		?= main
GIT_COMMIT  ?= $(shell git rev-list -1 HEAD)
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
	${GO_BUILD_VARS} go build -ldflags $(GO_LDFLAGS) -o bin/otel-add-on .

.PHONY: run
run:  ## Runs the scaler locally.
	go run ./main.go

.PHONY: build-image
build-image: build  ## Builds the container image for current arch.
	@$(call say,Build container image $(WORKER_IMAGE))
	docker build . -t ${CONTAINER_IMAGE} --build-arg VERSION=${VERSION} --build-arg GIT_COMMIT=${GIT_COMMIT}

.PHONY: build-image-multiarch
build-image-multiarch:  ## Builds the container image for arm64 and amd64.
	@$(call say,Build container image $(WORKER_IMAGE))
	docker buildx build --output=type=registry --platform=${BUILD_PLATFORMS} . -t ${CONTAINER_IMAGE} --build-arg VERSION=${VERSION} --build-arg GIT_COMMIT=${GIT_COMMIT}

.PHONY: build-image-goreleaser
build-image-goreleaser: ## Builds the multi-arch container image using goreleaser.
	goreleaser release --skip=validate,publish,sbom --clean --snapshot

codegen: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile='hack/boilerplate.go.txt' paths='./...'
	./hack/update-codegen.sh

.PHONY: deploy-helm
deploy-helm:  ## Deploys helm chart with otel-collector and otel scaler.
	cd helmchart/otel-add-on && \
	helm dependency build && \
	helm upgrade -i otel-add-on .

CONTROLLER_GEN = ${HACK_BIN}/controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	GOBIN=$(shell pwd)/bin go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.15.0

.PHONY: help
help: ## Show this help.
	@egrep -h '\s##\s' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-25s\033[0m %s\n", $$1, $$2}'


###############################
#		HELPERS
###############################

ifndef NO_COLOR
YELLOW=\033[0;33m
# no color
NC=\033[0m
endif

define say
echo "\n$(shell echo "$1  " | sed s/./=/g)\n $(YELLOW)$1$(NC)\n$(shell echo "$1  " | sed s/./=/g)"
endef

define createNs
@kubectl create namespace $1 --dry-run=client -o yaml | kubectl apply -f -
endef
