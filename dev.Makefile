include Makefile

.PHONY: dev-k3d
dev-k3d: build-image  ## Builds the container image for current arch, imports it to running k3d and restarts the scaler.
	@$(call say,Doing the dev cycle)
	k3d image import ghcr.io/kedify/otel-add-on:latest
	helm upgrade --reuse-values \
        keda-otel helmchart/otel-add-on \
		--set image.tag=latest  \
		--set image.pullPolicy=IfNotPresent \
		--set settings.logs.logLvl=debug \
	kubectl rollout restart deploy/otel-add-on-scaler

.PHONY: dev-local
dev-local: ## Prepare the SO and otel collector for local debug
	@$(call say,Prepare the conditions for local debug)
	helm upgrade --reuse-values \
 		keda-otel helmchart/otel-add-on \
 		--set replicaCount=1 \
 		--set opentelemetry-collector.config.exporters.otlp.endpoint=$(LOCAL_ENDPOINT):4317
	kubectl patch so otel-example --type=json -p '[{"op":"replace","path":"/spec/triggers/0/metadata/scalerAddress","value":"$(LOCAL_ENDPOINT):4318"}]'
	@$(call say,Continue by running the scaler locally from your favorite IDE outsice of K8s)
	@echo "Make sure $(LOCAL_ENDPOINT):4317 and $(LOCAL_ENDPOINT):4318 are listening.."

.PHONY: undo-dev-local
undo-dev-local: ## Revers the SO and otel collector for local debug
	@$(call say,Revert the conditions for local debug)
	helm upgrade --reuse-values \
 		keda-otel helmchart/otel-add-on \
 		--set replicaCount=1 \
 		--set opentelemetry-collector.config.exporters.otlp.endpoint=keda-otel-scaler:4317
 	kubectl patch so otel-example --type=json -p '[{"op":"replace","path":"/spec/triggers/0/metadata/scalerAddress","value":"keda-otel-scaler:4318"}]'
 	kubectl scale deploy/otel-add-on-scaler --replicas=1
