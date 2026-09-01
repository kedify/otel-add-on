#!/usr/bin/env bash

set -euo pipefail

DIR="${DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)}"
NAMESPACE="cloudwatch-metric-stream"
NAMESPACE_OWNER_LABEL="examples.kedify.io/owner"
NAMESPACE_CLUSTER_ANNOTATION="examples.kedify.io/cluster-uid"
NAMESPACE_STACK_ANNOTATION="examples.kedify.io/stack-name"
NAMESPACE_BUCKET_ANNOTATION="examples.kedify.io/failed-delivery-bucket"
STACK_PROJECT_VALUE="otel-add-on"
STACK_EXAMPLE_VALUE="cloudwatch-metric-stream"
STACK_CLUSTER_UID_TAG="KubernetesClusterUID"
RELEASE_NAME="cloudwatch-otel-scaler"
STACK_NAME="${STACK_NAME:-kedify-cloudwatch-metric-stream}"
RUN_VERIFICATION="${RUN_VERIFICATION:-true}"
USE_LOCAL_CHART="${USE_LOCAL_CHART:-false}"

: "${ACM_CERTIFICATE_ARN:?Set ACM_CERTIFICATE_ARN to an issued certificate in the cluster AWS Region}"
: "${FIREHOSE_HOSTNAME:?Set FIREHOSE_HOSTNAME to a new name covered by the ACM certificate}"
: "${ROUTE53_HOSTED_ZONE_ID:?Set ROUTE53_HOSTED_ZONE_ID to the public zone that contains FIREHOSE_HOSTNAME}"

EXAMPLE_AWS_REGION="${AWS_REGION:-${AWS_DEFAULT_REGION:-}}"
if [[ -z "${EXAMPLE_AWS_REGION}" ]]; then
  EXAMPLE_AWS_REGION="$(aws configure get region 2>/dev/null || true)"
fi
if [[ -z "${EXAMPLE_AWS_REGION}" ]]; then
  echo "Set AWS_REGION (or configure a default AWS Region)." >&2
  exit 1
fi

for command_name in aws kubectl helm curl jq envsubst openssl base64 grep seq; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "Missing required command: ${command_name}" >&2
    exit 1
  fi
done

KUBE_CONTEXT="${KUBE_CONTEXT:-$(command kubectl config current-context)}"
kubectl() {
  command kubectl --context "${KUBE_CONTEXT}" "$@"
}

wait_for_load_balancer() {
  local service_name="$1"
  local hostname=""
  local attempt

  for attempt in $(seq 1 90); do
    hostname="$(kubectl get service "${service_name}" -n "${NAMESPACE}" -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null || true)"
    if [[ -n "${hostname}" ]]; then
      printf '%s' "${hostname}"
      return 0
    fi
    sleep 10
  done

  echo "Timed out waiting for Service ${NAMESPACE}/${service_name} to get a load balancer." >&2
  return 1
}

wait_for_load_balancer_arn() {
  local hostname="$1"
  local load_balancer_arn=""
  local attempt

  for attempt in $(seq 1 60); do
    load_balancer_arn="$(
      aws elbv2 describe-load-balancers \
        --region "${EXAMPLE_AWS_REGION}" \
        --query "LoadBalancers[?DNSName=='${hostname}'].LoadBalancerArn | [0]" \
        --output text 2>/dev/null || true
    )"
    if [[ -n "${load_balancer_arn}" && "${load_balancer_arn}" != "None" ]]; then
      printf '%s' "${load_balancer_arn}"
      return 0
    fi
    sleep 5
  done

  echo "Timed out resolving the ARN for ${hostname}. Check AWS_REGION." >&2
  return 1
}

echo "Using Kubernetes context: ${KUBE_CONTEXT}"
aws sts get-caller-identity --region "${EXAMPLE_AWS_REGION}" --output json | jq '{Account, Arn}'

cluster_uid="$(kubectl get namespace kube-system -o jsonpath='{.metadata.uid}')"

set +e
existing_stack_description="$(
  aws cloudformation describe-stacks \
    --region "${EXAMPLE_AWS_REGION}" \
    --stack-name "${STACK_NAME}" \
    --output json 2>&1
)"
describe_stack_status=$?
set -e
if ((describe_stack_status == 0)); then
  existing_stack_project="$(
    jq -r '[.Stacks[0].Tags[]? | select(.Key == "Project") | .Value][0] // ""' \
      <<<"${existing_stack_description}"
  )"
  existing_stack_example="$(
    jq -r '[.Stacks[0].Tags[]? | select(.Key == "Example") | .Value][0] // ""' \
      <<<"${existing_stack_description}"
  )"
  existing_stack_cluster_uid="$(
    jq -r --arg key "${STACK_CLUSTER_UID_TAG}" \
      '[.Stacks[0].Tags[]? | select(.Key == $key) | .Value][0] // ""' \
      <<<"${existing_stack_description}"
  )"
  if [[ "${existing_stack_project}" != "${STACK_PROJECT_VALUE}" || \
    "${existing_stack_example}" != "${STACK_EXAMPLE_VALUE}" || \
    "${existing_stack_cluster_uid}" != "${cluster_uid}" ]]; then
    echo "Refusing to update stack ${STACK_NAME}: it is not owned by this example." >&2
    exit 1
  fi
elif [[ "${existing_stack_description}" != *"does not exist"* ]]; then
  echo "Unable to determine whether stack ${STACK_NAME} already exists:" >&2
  echo "${existing_stack_description}" >&2
  exit 1
fi

certificate_status="$(
  aws acm describe-certificate \
    --region "${EXAMPLE_AWS_REGION}" \
    --certificate-arn "${ACM_CERTIFICATE_ARN}" \
    --query 'Certificate.Status' \
    --output text
)"
if [[ "${certificate_status}" != "ISSUED" ]]; then
  echo "ACM certificate is ${certificate_status}; it must be ISSUED." >&2
  exit 1
fi

if ! kubectl get deployment --all-namespaces \
  -l app.kubernetes.io/name=aws-load-balancer-controller \
  -o name | grep -q .; then
  echo "AWS Load Balancer Controller was not found in the current cluster." >&2
  echo "Install it before running this example." >&2
  exit 1
fi

namespace_exists=false
if kubectl get namespace "${NAMESPACE}" >/dev/null 2>&1; then
  namespace_json="$(kubectl get namespace "${NAMESPACE}" -o json)"
  namespace_owner="$(
    jq -r --arg key "${NAMESPACE_OWNER_LABEL}" '.metadata.labels[$key] // ""' \
      <<<"${namespace_json}"
  )"
  namespace_cluster_uid="$(
    jq -r --arg key "${NAMESPACE_CLUSTER_ANNOTATION}" \
      '.metadata.annotations[$key] // ""' <<<"${namespace_json}"
  )"
  namespace_stack_name="$(
    jq -r --arg key "${NAMESPACE_STACK_ANNOTATION}" \
      '.metadata.annotations[$key] // ""' <<<"${namespace_json}"
  )"
  namespace_backup_bucket="$(
    jq -r --arg key "${NAMESPACE_BUCKET_ANNOTATION}" \
      '.metadata.annotations[$key] // ""' <<<"${namespace_json}"
  )"
  if [[ "${namespace_owner}" != "${STACK_EXAMPLE_VALUE}" || \
    "${namespace_cluster_uid}" != "${cluster_uid}" || \
    "${namespace_stack_name}" != "${STACK_NAME}" ]]; then
    echo "Refusing to use namespace ${NAMESPACE}: ownership or cluster identity does not match." >&2
    exit 1
  fi
  if ((describe_stack_status != 0)) && [[ -n "${namespace_backup_bucket}" ]]; then
    echo "Refusing to create a replacement stack while retained bucket ${namespace_backup_bucket} is still recorded." >&2
    echo "Run cleanup.sh first so the previous run's bucket cannot be orphaned." >&2
    exit 1
  fi
  namespace_exists=true
fi

keda_deployments_json="$(kubectl get deployments --all-namespaces -o json)"
keda_operator_count="$(
  jq '[.items[] | select(.metadata.labels["app.kubernetes.io/name"] == "keda-operator")] | length' \
    <<<"${keda_deployments_json}"
)"
keda_metrics_server_count="$(
  jq '[.items[] | select(.metadata.labels["app.kubernetes.io/name"] == "keda-operator-metrics-apiserver")] | length' \
    <<<"${keda_deployments_json}"
)"

if ((keda_operator_count == 0 && keda_metrics_server_count == 0)); then
  echo "KEDA is not installed; installing it in namespace keda."
  helm repo add kedify https://kedify.github.io/charts --force-update
  helm repo update kedify
  helm upgrade --install keda kedify/keda \
    --kube-context "${KUBE_CONTEXT}" \
    --namespace keda \
    --create-namespace \
    --version "${KEDA_VERSION:-v2.19.0-1}"
  keda_operator_namespace="keda"
  keda_operator_name="keda-operator"
  keda_metrics_server_namespace="keda"
  keda_metrics_server_name="keda-operator-metrics-apiserver"
elif ((keda_operator_count != 1 || keda_metrics_server_count != 1)); then
  echo "Found an incomplete or ambiguous KEDA installation." >&2
  echo "Expected exactly one operator and one metrics API server Deployment." >&2
  exit 1
else
  keda_operator_namespace="$(
    jq -r '.items[] | select(.metadata.labels["app.kubernetes.io/name"] == "keda-operator") | .metadata.namespace' \
      <<<"${keda_deployments_json}"
  )"
  keda_operator_name="$(
    jq -r '.items[] | select(.metadata.labels["app.kubernetes.io/name"] == "keda-operator") | .metadata.name' \
      <<<"${keda_deployments_json}"
  )"
  keda_metrics_server_namespace="$(
    jq -r '.items[] | select(.metadata.labels["app.kubernetes.io/name"] == "keda-operator-metrics-apiserver") | .metadata.namespace' \
      <<<"${keda_deployments_json}"
  )"
  keda_metrics_server_name="$(
    jq -r '.items[] | select(.metadata.labels["app.kubernetes.io/name"] == "keda-operator-metrics-apiserver") | .metadata.name' \
      <<<"${keda_deployments_json}"
  )"
fi
kubectl rollout status deployment/"${keda_operator_name}" \
  -n "${keda_operator_namespace}" --timeout=10m
kubectl rollout status deployment/"${keda_metrics_server_name}" \
  -n "${keda_metrics_server_namespace}" --timeout=10m

if [[ "${namespace_exists}" != "true" ]]; then
  kubectl create namespace "${NAMESPACE}" --dry-run=client -o json | \
    jq --arg owner_key "${NAMESPACE_OWNER_LABEL}" \
      --arg owner_value "${STACK_EXAMPLE_VALUE}" \
      --arg cluster_key "${NAMESPACE_CLUSTER_ANNOTATION}" \
      --arg cluster_value "${cluster_uid}" \
      --arg stack_key "${NAMESPACE_STACK_ANNOTATION}" \
      --arg stack_value "${STACK_NAME}" \
      '.metadata.labels = (.metadata.labels // {}) |
       .metadata.labels[$owner_key] = $owner_value |
       .metadata.annotations = (.metadata.annotations // {}) |
       .metadata.annotations[$cluster_key] = $cluster_value |
       .metadata.annotations[$stack_key] = $stack_value' | \
    kubectl apply -f -
fi

echo "Creating the demo workload and its Network Load Balancer."
kubectl apply -n "${NAMESPACE}" -f "${DIR}/workload.yaml"
kubectl rollout status deployment/cloudwatch-metric-demo -n "${NAMESPACE}" --timeout=10m

workload_hostname="$(wait_for_load_balancer cloudwatch-metric-demo)"
workload_load_balancer_arn="$(wait_for_load_balancer_arn "${workload_hostname}")"
workload_load_balancer_dimension="${workload_load_balancer_arn#*:loadbalancer/}"
if [[ "${workload_load_balancer_dimension}" != net/* ]]; then
  echo "Expected a Network Load Balancer dimension, got ${workload_load_balancer_dimension}." >&2
  exit 1
fi

if kubectl get secret cloudwatch-metric-stream -n "${NAMESPACE}" >/dev/null 2>&1; then
  firehose_access_key="$(
    kubectl get secret cloudwatch-metric-stream -n "${NAMESPACE}" \
      -o jsonpath='{.data.firehose-access-key}' | base64 --decode
  )"
else
  firehose_access_key="$(openssl rand -hex 32)"
fi

kubectl create secret generic cloudwatch-metric-stream \
  -n "${NAMESPACE}" \
  --from-literal="firehose-access-key=${firehose_access_key}" \
  --from-literal="workload-load-balancer-dimension=${workload_load_balancer_dimension}" \
  --dry-run=client -o yaml | kubectl apply -f -

certificate_value="otelCollector.service.annotations.service\\.beta\\.kubernetes\\.io/aws-load-balancer-ssl-cert=${ACM_CERTIFICATE_ARN}"

echo "Installing the OTel scaler and Firehose receiver."
if [[ "${USE_LOCAL_CHART}" == "true" ]]; then
  helm upgrade --install "${RELEASE_NAME}" "${DIR}/../../helmchart/otel-add-on" \
    --kube-context "${KUBE_CONTEXT}" \
    --namespace "${NAMESPACE}" \
    -f "${DIR}/otel-scaler-values.yaml" \
    --set-string "${certificate_value}"
else
  helm upgrade --install "${RELEASE_NAME}" oci://ghcr.io/kedify/charts/otel-add-on --version=0.1.4 \
    --kube-context "${KUBE_CONTEXT}" \
    --namespace "${NAMESPACE}" \
    -f "${DIR}/otel-scaler-values.yaml" \
    --set-string "${certificate_value}"
fi

kubectl rollout restart deployment/cloudwatch-firehose-receiver -n "${NAMESPACE}"
kubectl rollout status deployment/cloudwatch-otel-scaler -n "${NAMESPACE}" --timeout=10m
kubectl rollout status deployment/cloudwatch-firehose-receiver -n "${NAMESPACE}" --timeout=10m

collector_hostname="$(wait_for_load_balancer cloudwatch-firehose-receiver)"
hosted_zone_id="${ROUTE53_HOSTED_ZONE_ID##*/}"

echo "Creating Route 53, Firehose, and CloudWatch Metric Stream resources."
aws cloudformation deploy \
  --region "${EXAMPLE_AWS_REGION}" \
  --stack-name "${STACK_NAME}" \
  --template-file "${DIR}/cloudformation.yaml" \
  --capabilities CAPABILITY_IAM \
  --no-fail-on-empty-changeset \
  --tags "Project=${STACK_PROJECT_VALUE}" "Example=${STACK_EXAMPLE_VALUE}" \
    "${STACK_CLUSTER_UID_TAG}=${cluster_uid}" \
  --parameter-overrides \
    "CollectorHostname=${FIREHOSE_HOSTNAME}" \
    "CollectorLoadBalancerHostname=${collector_hostname}" \
    "HostedZoneId=${hosted_zone_id}" \
    "FirehoseAccessKey=${firehose_access_key}" \
    "KubernetesClusterUID=${cluster_uid}"

failed_delivery_bucket="$(
  aws cloudformation describe-stacks \
    --region "${EXAMPLE_AWS_REGION}" \
    --stack-name "${STACK_NAME}" \
    --query 'Stacks[0].Outputs[?OutputKey==`FailedDeliveryBucketName`].OutputValue | [0]' \
    --output text
)"
kubectl annotate namespace "${NAMESPACE}" \
  "${NAMESPACE_BUCKET_ANNOTATION}=${failed_delivery_bucket}" --overwrite

export WORKLOAD_LOAD_BALANCER_DIMENSION="${workload_load_balancer_dimension}"
envsubst '${WORKLOAD_LOAD_BALANCER_DIMENSION}' < "${DIR}/scaledobject.yaml" | \
  kubectl apply -n "${NAMESPACE}" -f -

cat <<EOF

CloudWatch Metric Streams example is ready.

  Workload:           http://${workload_hostname}
  Firehose endpoint:  https://${FIREHOSE_HOSTNAME}
  NLB dimension:      ${workload_load_balancer_dimension}
  CloudFormation:     ${STACK_NAME} (${EXAMPLE_AWS_REGION})

CloudWatch publishes NetworkELB metrics once per minute. A newly created metric
stream can take several minutes to deliver its first batch.
EOF

if [[ "${RUN_VERIFICATION}" == "true" ]]; then
  KUBE_CONTEXT="${KUBE_CONTEXT}" AWS_REGION="${EXAMPLE_AWS_REGION}" \
    STACK_NAME="${STACK_NAME}" "${DIR}/verify.sh"
else
  echo "Run ${DIR}/verify.sh to generate traffic and verify scaling."
fi

echo "Clean up with: AWS_REGION=${EXAMPLE_AWS_REGION} STACK_NAME=${STACK_NAME} ${DIR}/cleanup.sh"
