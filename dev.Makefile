include Makefile

LOCAL_ENDPOINT ?= host.k3d.internal
SO_NAME ?= otel-example

## helpers
check_defined = \
	$(strip $(foreach 1,$1, \
		$(call __check_defined,$1,$(strip $(value 2)))))
__check_defined = \
	$(if $(value $1),, \
		$(error Undefined $1$(if $2, ($2))))
check_k3d = \
	@if [ -z $$(kubectl config current-context | grep "^k3d-") ]; then \
		echo "Create k3d cluster first!" ;\
		exit 1 ;\
	fi

##@ Dev
.PHONY: dev-k3d
dev-k3d: ## Builds the container image for current arch, imports it to running k3d and restarts the scaler.
	@$(call say,Doing the dev cycle)
	@$(call check_k3d)
	k3d image import -c $(shell kubectl config current-context | sed -e "s/^k3d-//") ghcr.io/kedify/otel-add-on:latest
	helm upgrade --reuse-values \
		keda-otel-scaler helmchart/otel-add-on \
		-nkeda \
		--set image.tag=latest \
		--set image.pullPolicy=IfNotPresent \
		--set settings.logs.logLvl=debug
	kubectl -nkeda rollout restart deploy/keda-otel-scaler

.PHONY: dev-local
dev-local: ## Prepare the SO and otel collector for local debug
	@$(call say,Prepare the conditions for local debug)
	@$(call check_k3d)
	helm upgrade --reuse-values \
		keda-otel-scaler helmchart/otel-add-on \
		-nkeda \
		--set replicaCount=1 \
		--set opentelemetry-collector.config.exporters.otlp.endpoint=$(LOCAL_ENDPOINT):4317
	kubectl patch so $(SO_NAME) --type=json -p '[{"op":"replace","path":"/spec/triggers/0/metadata/scalerAddress","value":"$(LOCAL_ENDPOINT):4318"}]'
	@$(call say,Continue by running the scaler locally from your favorite IDE outside of K8s)
	@echo "Make sure $(LOCAL_ENDPOINT):4317 and $(LOCAL_ENDPOINT):4318 are listening.."

.PHONY: undo-dev-local
undo-dev-local: ## Revers the SO and otel collector for local debug
	@$(call say,Revert the conditions for local debug)
	@$(call check_k3d)
	helm upgrade --reuse-values \
		keda-otel-scaler helmchart/otel-add-on \
		-nkeda \
		--set replicaCount=1 \
		--set opentelemetry-collector.config.exporters.otlp.endpoint=keda-otel-scaler.keda.svc:4317
	kubectl patch so $(SO_NAME) --type=json -p '[{"op":"replace","path":"/spec/triggers/0/metadata/scalerAddress","value":"keda-otel-scaler.keda.svc:4318"}]'
	kubectl scale -nkeda deploy/keda-otel-scaler --replicas=1

.PHONY: k8s-certs
k8s-certs: test-certs ## Creates k8s secrets from the generated certificates
	@$(call say,Preparing certs)
	@$(call check_k3d)
	kubectl -nkeda delete secret --ignore-not-found server-tls client-tls root-ca
	kubectl -nkeda create secret tls server-tls --cert=certs/server.crt --key=certs/server.key
	kubectl -nkeda create secret tls client-tls --cert=certs/client.crt --key=certs/client.key
	kubectl -nkeda create secret generic root-ca --from-file=rootCA.crt=certs/rootCA.crt

##@ Demos
.PHONY: demo-podinfo
demo-podinfo: ## setup ./examples/metric-pull
	./examples/metric-pull/setup.sh

.PHONY: demo-podinfo-dev
demo-podinfo-dev: ## setup ./examples/metric-pull
	SETUP_ONLY=true ./examples/metric-pull/setup.sh
	$(MAKE) -f dev.Makefile dev-k3d
	@$(call say,Done)
	@echo "Continue with: (hey -z 180s http://localhost:8181/delay/2 &> /dev/null)&"

.PHONY: demo-podinfo-tls
demo-podinfo-tls: ## setup ./examples/metric-pull with TLS
	SETUP_ONLY=true ./examples/metric-pull/setup.sh
	$(MAKE) -f dev.Makefile k8s-certs
	helm upgrade -i -nkeda keda-otel-scaler ./helmchart/otel-add-on -f ./examples/metric-pull/scaler-with-collector-pull-tls-values.yaml
	$(MAKE) -f dev.Makefile dev-k3d
	kubectl apply -f ./examples/metric-pull/podinfo-so.yaml
	@$(call say,Done)
	@echo "Continue with: (hey -z 180s http://localhost:8181/delay/2 &> /dev/null)&"

.PHONY: demo-otel-upstream
demo-otel-upstream: ## setup ./examples/metric-push
	./examples/metric-push/setup.sh
	$(MAKE) -f dev.Makefile dev-k3d

.PHONY: demo-operator
demo-operator: ## setup ./examples/otel-operator
	@:$(call check_defined, PR_BRANCH GH_PAT)
	$(call check_k3d)
	./examples/otel-operator/setup.sh
	$(MAKE) -f dev.Makefile dev-k3d

.PHONY: demo-operator-tls
demo-operator-tls: ## setup ./examples/otel-operator with TLS
	SETUP_ONLY=true ./examples/otel-operator/setup.sh
	$(MAKE) -f dev.Makefile k8s-certs
	# helm cant merge correctly value files when the later overrides an item in an array (w/ unique name)
	rm -rf values_tmp && ./hack/mergeValues.sh \
		examples/otel-operator/scaler-with-operator-with-collector-values.yaml \
		examples/otel-operator/tls-overlay-values.yaml > values_tmp
	@$(call say,Merged values:)
	yq values_tmp
	helm upgrade -i \
			keda-otel-scaler helmchart/otel-add-on \
			-nkeda \
			-f ./values_tmp
	rm -rf values_tmp
	$(MAKE) -f dev.Makefile dev-k3d
	@$(call say,Creating SO)
	kubectl apply -f <(cat ./examples/otel-operator/so.yaml | envsubst)
