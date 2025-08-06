include Makefile

LOCAL_ENDPOINT ?= host.k3d.internal
SO_NAME ?= otel-example

.PHONY: dev-k3d
dev-k3d: build-image ## Builds the container image for current arch, imports it to running k3d and restarts the scaler.
	@$(call say,Doing the dev cycle)
	kubectl config current-context | grep "^k3d-" &> /dev/null || echo "Create k3d cluster first"
	k3d image import -c $(shell kubectl config current-context | sed -e "s/^k3d-//") ghcr.io/kedify/otel-add-on:latest
	helm upgrade --reuse-values \
        keda-otel-scaler helmchart/otel-add-on \
        -nkeda \
		--set image.tag=latest  \
		--set image.pullPolicy=IfNotPresent \
		--set settings.logs.logLvl=debug
	kubectl -nkeda rollout restart deploy/keda-otel-scaler

.PHONY: dev-local
dev-local: ## Prepare the SO and otel collector for local debug
	@$(call say,Prepare the conditions for local debug)
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
	helm upgrade --reuse-values \
		keda-otel-scaler helmchart/otel-add-on \
		-nkeda \
		--set replicaCount=1 \
		--set opentelemetry-collector.config.exporters.otlp.endpoint=keda-otel-scaler.keda.svc:4317
	kubectl patch so $(SO_NAME) --type=json -p '[{"op":"replace","path":"/spec/triggers/0/metadata/scalerAddress","value":"keda-otel-scaler.keda.svc:4318"}]'
	kubectl scale -nkeda deploy/keda-otel-scaler --replicas=1

.PHONY: k8s-certs
k8s-certs: test-certs ## Creates k8s secrets from the generated certificates
	kubectl config current-context | grep "^k3d-" &> /dev/null || echo "Create k3d cluster first"
	@$(call say,Preparing certs)
	kubectl -nkeda delete secret --ignore-not-found server-tls client-tls root-ca
	kubectl -nkeda create secret tls server-tls --cert=certs/server.crt --key=certs/server.key
	kubectl -nkeda create secret tls client-tls --cert=certs/client.crt --key=certs/client.key
	kubectl -nkeda create secret generic root-ca --from-file=rootCA.crt=certs/rootCA.crt

.PHONY: help-dev
help-dev: ## >> Start HERE <<
	@$(call say,* Run the podinfo use-case with latest)
	@echo './examples/metric-pull/setup.sh && make -f dev.Makefile dev-k3d'

	@$(call say,* Run the podinfo use-case with latest and TLS)
	@echo 'SETUP_ONLY=true ./examples/metric-pull/setup.sh && helm upgrade -i -nkeda keda-otel-scaler ./helmchart/otel-add-on -f ./examples/metric-pull/scaler-with-collector-pull-tls-values.yaml && make -f dev.Makefile k8s-certs dev-k3d'

	@$(call say,* Run the OpenTelemetry demo use-case with latest)
	@echo './examples/metric-push/setup.sh && make -f dev.Makefile dev-k3d'

	@$(call say,* Run the OTel Operator demo use-case with latest)
	@echo './examples/otel-operator/setup.sh && make -f dev.Makefile dev-k3d'
