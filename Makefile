###############################
#		CONSTANTS
###############################
SHELL       = /bin/bash
GH_REPO_ORG = jkremser
VERSION 		?= main
GIT_COMMIT  ?= $(shell git rev-list -1 HEAD)
GO_LDFLAGS="-X github.com/${GH_REPO_ORG}/otel-add-on/pkg/build.version=${VERSION} -X github.com/${GH_REPO_ORG}/otel-add-on/pkg/build.gitCommit=${GIT_COMMIT}"
BUILD_PLATFORMS ?= linux/amd64,linux/arm64

CONTAINER_IMAGE ?= ghcr.io/jkremser/otel-add-on
HACK_BIN ?= bin
ARCH       ?=amd64
CGO        ?=0
TARGET_OS  ?=linux

GO_BUILD_VARS= GO111MODULE=on CGO_ENABLED=$(CGO) GOOS=$(TARGET_OS) GOARCH=$(ARCH)

MINIO_USER := $(shell export LC_ALL=C; tr -dc 'A-Za-z0-9' </dev/urandom | head -c 8)
MINIO_PASSWORD := $(shell export LC_ALL=C; tr -dc 'A-Za-z0-9!$%' </dev/urandom | head -c 15)

RABBITMQ_USER := $(shell export LC_ALL=C; tr -dc 'A-Za-z0-9' </dev/urandom | head -c 8)
RABBITMQ_PASSWORD := $(shell export LC_ALL=C; tr -dc 'A-Za-z0-9!$%' </dev/urandom | head -c 15)


###############################
#		TARGETS
###############################
all: help

.PHONY: build
build: ## Builds the binary.
	${GO_BUILD_VARS} go build -ldflags $(GO_LDFLAGS) -trimpath -a -o bin/otel-add-on ./otel-add-on

.PHONY: build-multiarch-image
build-multiarch-image:  ## Builds the container image.
	@$(call say,Build container image $(WORKER_IMAGE))
	docker buildx build --output=type=registry --platform=${BUILD_PLATFORMS} . -t ${CONTAINER_IMAGE} --build-arg VERSION=${VERSION} --build-arg GIT_COMMIT=${GIT_COMMIT}

.PHONY: run
run: ## Runs the built image.
	docker run -ti $(WORKER_IMAGE)

.PHONY: run-example
run-example: ## Runs the built image and creates an example image. Usage: PROMPT="dog" NUM_IMAGES=2 make run-example
	@mkdir -p results
	docker run -ti --volume $(PWD)/results:/app/results $(WORKER_IMAGE) --prompt "$(PROMPT)" --number_of_images $(NUM_IMAGES)
	@echo Done. Check the results directory.

.PHONY: deploy-minio
deploy-minio: ## Deploys minio into current Kubernetes context.
	@$(call say,Deploy Minio)
	@$(call createNs,stable-diff)
	helm repo add minio https://charts.min.io/
	@helm upgrade -i \
	minio minio/minio \
	--wait \
	-nstable-diff \
	--timeout=2m \
	-f minio-values.yaml \
	--set rootUser=$(MINIO_USER),rootPassword=$(MINIO_PASSWORD)

.PHONY: deploy-app
deploy-app: ## Deploys the webui.
	@$(call say,Deploy stable diffusion app)
	@$(call createNs,stable-diff)
	kubectl apply -nstable-diff -f manifests/webapp.yaml
	kubectl wait -nstable-diff --timeout=90s --for=condition=ready pod -lapp=stable-diffusion-webui

	@$(call say,Exposing..)
	@echo the webapp should be available on http://$(shell echo $(shell kubectl get ing -n stable-diff stable-diffusion-webui -ojson | jq -r '.status.loadBalancer.ingress[0].ip'))
	@echo -------------------------------------------------------

.PHONY: deploy-kafka
deploy-kafka: ## Deploys the strimzi operator and creates a kafka cluster with a single topic.
	@$(call say,Deploy Kafka)
	@$(call createNs,kafka)
	kubectl apply -f 'https://strimzi.io/install/latest?namespace=kafka' -n kafka
	kubectl wait --timeout=90s -nkafka --for=condition=ready pod -lname=strimzi-cluster-operator
	kubectl apply -f manifests/kafka.yaml
	kubectl wait --timeout=60s -nkafka --for=condition=ready pod -lstrimzi.io/name=stablediff-cluster-kafka
	kubectl wait --timeout=60s -nkafka --for=condition=ready pod -lstrimzi.io/component-type=entity-operator

.PHONY: deploy-rabbitmq
deploy-rabbitmq: ## Deploys rabbitmq.
	@$(call say,Deploy RabbitMQ)
	@$(call createNs,rabbitmq-system)
	@$(call createNs,stable-diff)
	kubectl apply -f https://github.com/rabbitmq/cluster-operator/releases/latest/download/cluster-operator.yml
	kubectl wait --timeout=90s -nrabbitmq-system --for=condition=ready pod -lapp.kubernetes.io/name=rabbitmq-cluster-operator
	@cat manifests/default_user.conf.tmpl | RABBITMQ_USER=$(RABBITMQ_USER) RABBITMQ_PASSWORD=$(RABBITMQ_PASSWORD) envsubst > manifests/default_user.conf
	@kubectl create secret generic stablediff -nrabbitmq-system --from-file=manifests/default_user.conf --from-literal password=$(RABBITMQ_PASSWORD) --from-literal username=$(RABBITMQ_USER) --dry-run=client -o yaml | kubectl apply -f -
	@kubectl create secret generic stablediff-rabbitmq -nstable-diff --from-literal host=amqp://$(RABBITMQ_USER):$(RABBITMQ_PASSWORD)@rabbitmq-cluster.rabbitmq-system.svc.cluster.local:5672 --dry-run=client -o yaml | kubectl apply -f -
	kubectl apply -nrabbitmq-system -f manifests/rabbitmq.yaml
	kubectl wait --timeout=90s -nrabbitmq-system --for=condition=established crd/rabbitmqclusters.rabbitmq.com
	@while ! kubectl wait --timeout=90s -nrabbitmq-system --for=condition=ready pod -lapp.kubernetes.io/name=rabbitmq-cluster; do "echo waiting for rabbitmq.."; sleep 1; done

.PHONY: deploy-scaledobject
deploy-scaledobject: ## Deploys KEDA scaledobject.
	@$(call say,Deploy KEDA scaledobject)
	-kubectl delete -nstable-diff -f manifests/scaledjob.yaml
	kubectl apply -nstable-diff -f manifests/app$(GPU).yaml
	kubectl wait -nstable-diff --timeout=600s --for=condition=ready pod -lapp=stable-diffusion-worker
	kubectl apply -nstable-diff -f manifests/scaledobject.yaml

.PHONY: deploy-scaledjob
deploy-scaledjob: ## Deploys KEDA scaledjob.
	@$(call say,Deploy KEDA scaledjob)
	-kubectl delete -nstable-diff -f manifests/scaledobject.yaml
	kubectl apply -nstable-diff -f manifests/scaledjob.yaml

.PHONY: deploy-cert-manager
deploy-cert-manager: ## Deploys cert manager and related CRs.
	@$(call say,Deploy cert manager)
	kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v$(CERT_MANAGER_VERSION)/cert-manager.yaml
	kubectl apply -nstable-diff -f manifests/cert-manager.yaml
	-kubectl annotate ing --overwrite -nstable-diff stable-diffusion-webui cert-manager.io/issuer=letsencrypt-production

.PHONY: deploy
deploy: deploy-minio deploy-rabbitmq deploy-app ## Deploys minio, RabbitMQ and the web ui.
	@$(call say,Deploy the required infrastructure)
	@echo Done. Continue with either make deploy-scaledjob XOR make deploy-scaledobject.

.PHONY: undeploy
undeploy:
	@$(call say,Undeploying the use-case)
	-kubectl delete -nrabbitmq-system rabbitmqcluster rabbitmq-cluster
	-helm uninstall minio -nstable-diff
	-kubectl delete ns stable-diff
	-kubectl delete ns rabbitmq-system

.PHONY: deploy-from-scratch
deploy-from-scratch: cluster import deploy ## Prepares also k3s cluster and deploys everything on it.

.PHONY: cluster
cluster: ## Creates simple k3d cluster with ingress controller (traefik).
	@$(call say,k3d cluster $(CLUSTER_NAME))
	k3d cluster create $(CLUSTER_NAME) -p "8081:80@loadbalancer"

.PHONY: import
import: ## Import images to k3d cluster to speed up the container creation.
	@$(call say,Import container images)
	k3d image import $(WORKER_IMAGE) -c $(CLUSTER_NAME)
	k3d image import $(WEBUI_IMAGE) -c $(CLUSTER_NAME)

.PHONY: run-webui-dev
run-webui-dev: $(VENDIR) ## Runs the webui locally.
	@$(call say,Starting web ui)
	(kubectl port-forward svc/rabbitmq-cluster -n rabbitmq-system 5672:5672 &)
	bash -c "trap 'trap - SIGINT SIGTERM ERR; echo cleanup ; kill $(shell ps aux | grep '[k]ubectl port-forward' | awk '{ print $$2}') 2> /dev/null || true; exit 0' SIGINT SIGTERM ERR; cd webui && AMQP_URL=amqp://$(shell kubectl get secrets stablediff -n rabbitmq-system -o jsonpath={.data.username} | base64 --decode):$(shell kubectl get secrets stablediff -n rabbitmq-system -o jsonpath={.data.password} | base64 --decode)@localhost:5672 npm run dev"

.PHONY: sync
sync: $(VENDIR) ## Syncs the rupeshs/fastsdcpu the repo.
	@$(call say,Sync upstream repo)
	$(VENDIR) sync

$(VENDIR): 
	@$(call say,Download vendir)
	mkdir -p $(HACK_BIN)
	curl -Ls https://carvel.dev/install.sh | K14SIO_INSTALL_BIN_DIR=$(HACK_BIN) bash
	@echo "vendir downloaded"

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
