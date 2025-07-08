## Requirements
- helm cli installed
- kubectl installed
- Hugging Face token
- k8s cluster with accelerators - [cloud deployment](https://github.com/vllm-project/production-stack/tree/main/tutorials/cloud_deployments) 
or [local](https://github.com/vllm-project/production-stack/blob/main/tutorials/00-install-kubernetes-env.md) environment (linux + nvidia accelerator)

# There are three examples for scaling the vLLM workloads
- using DCGM metrics
- sidecar approach, where each vLLM pod will get a sidecar with OTel Collector injected. These sidecars report the metrics into KEDA OTel scaler.
- centralized approach with only one OTel collector scraping the metrics from the vLLM Router

For all examples, first run the [`./setup.sh`](./setup.sh)
