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
TLS_STACK_NAME="${TLS_STACK_NAME:-${STACK_NAME}-tls}"
RUN_VERIFICATION="${RUN_VERIFICATION:-true}"
USE_LOCAL_CHART="${USE_LOCAL_CHART:-false}"
ACM_CERTIFICATE_ARN="${ACM_CERTIFICATE_ARN:-}"
FIREHOSE_HOSTNAME="${FIREHOSE_HOSTNAME:-}"
ROUTE53_HOSTED_ZONE_ID="${ROUTE53_HOSTED_ZONE_ID:-}"

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

# Resolve the public hosted zone. A supplied hostname selects its most-specific
# matching zone; without either override, automatic selection is safe only when
# the account contains exactly one public zone.
FIREHOSE_HOSTNAME="$(
  jq -nr --arg value "${FIREHOSE_HOSTNAME%.}" '$value | ascii_downcase'
)"
if [[ -n "${ROUTE53_HOSTED_ZONE_ID}" ]]; then
  hosted_zone_id="${ROUTE53_HOSTED_ZONE_ID##*/}"
  hosted_zone_json="$(aws route53 get-hosted-zone --id "${hosted_zone_id}" --output json)"
  if [[ "$(jq -r '.HostedZone.Config.PrivateZone' <<<"${hosted_zone_json}")" == "true" ]]; then
    echo "Hosted zone ${hosted_zone_id} is private; Firehose requires a publicly resolvable endpoint." >&2
    exit 1
  fi
  hosted_zone_name="$(
    jq -r '.HostedZone.Name | sub("\\.$"; "") | ascii_downcase' <<<"${hosted_zone_json}"
  )"
else
  public_hosted_zones="$(
    aws route53 list-hosted-zones --output json | \
      jq '[.HostedZones[]
        | select(.Config.PrivateZone != true)
        | {id: (.Id | sub("^/hostedzone/"; "")),
           name: (.Name | sub("\\.$"; "") | ascii_downcase)}]'
  )"
  if [[ -n "${FIREHOSE_HOSTNAME}" ]]; then
    matching_hosted_zones="$(
      jq --arg hostname "${FIREHOSE_HOSTNAME}" \
        '[.[]
          | .name as $zone_name
          | select($hostname | endswith("." + $zone_name))]
         | sort_by(.name | length)
         | reverse' <<<"${public_hosted_zones}"
    )"
    matching_zone_count="$(jq 'length' <<<"${matching_hosted_zones}")"
    if ((matching_zone_count == 0)); then
      echo "No public Route 53 hosted zone contains ${FIREHOSE_HOSTNAME}." >&2
      echo "Set ROUTE53_HOSTED_ZONE_ID explicitly or choose a hostname in this account's public zones." >&2
      exit 1
    fi
    hosted_zone_name="$(jq -r '.[0].name' <<<"${matching_hosted_zones}")"
    most_specific_zone_count="$(
      jq --arg name "${hosted_zone_name}" '[.[] | select(.name == $name)] | length' \
        <<<"${matching_hosted_zones}"
    )"
    if ((most_specific_zone_count != 1)); then
      echo "Multiple public hosted zones named ${hosted_zone_name} match the hostname." >&2
      echo "Set ROUTE53_HOSTED_ZONE_ID to select one explicitly." >&2
      exit 1
    fi
    hosted_zone_id="$(jq -r '.[0].id' <<<"${matching_hosted_zones}")"
  else
    public_zone_count="$(jq 'length' <<<"${public_hosted_zones}")"
    if ((public_zone_count != 1)); then
      echo "Automatic DNS selection requires exactly one public Route 53 hosted zone; found ${public_zone_count}." >&2
      jq -r '.[] | "  \(.id)  \(.name)"' <<<"${public_hosted_zones}" >&2
      echo "Set FIREHOSE_HOSTNAME or ROUTE53_HOSTED_ZONE_ID to disambiguate." >&2
      exit 1
    fi
    hosted_zone_id="$(jq -r '.[0].id' <<<"${public_hosted_zones}")"
    hosted_zone_name="$(jq -r '.[0].name' <<<"${public_hosted_zones}")"
  fi
fi

if [[ -z "${FIREHOSE_HOSTNAME}" ]]; then
  FIREHOSE_HOSTNAME="cw-otel-${cluster_uid%%-*}.${hosted_zone_name}"
fi
if [[ "${FIREHOSE_HOSTNAME}" == *"*"* || \
  "${FIREHOSE_HOSTNAME}" != *."${hosted_zone_name}" ]]; then
  echo "FIREHOSE_HOSTNAME must be a concrete subdomain of ${hosted_zone_name}." >&2
  exit 1
fi
ROUTE53_HOSTED_ZONE_ID="${hosted_zone_id}"

echo "Using public DNS name ${FIREHOSE_HOSTNAME} in hosted zone ${hosted_zone_id} (${hosted_zone_name})."

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

if ! kubectl get deployment --all-namespaces \
  -l app.kubernetes.io/name=aws-load-balancer-controller \
  -o name | grep -q .; then
  echo "AWS Load Balancer Controller was not found in the current cluster." >&2
  echo "Install it before running this example." >&2
  exit 1
fi

ensure_certificate() {
  # Unless the caller supplies an existing certificate, create a small owned
  # CloudFormation stack that requests and DNS-validates one automatically.
  set +e
  tls_stack_description="$(
    aws cloudformation describe-stacks \
      --region "${EXAMPLE_AWS_REGION}" \
      --stack-name "${TLS_STACK_NAME}" \
      --output json 2>&1
  )"
  describe_tls_stack_status=$?
  set -e
  tls_stack_exists=false
  tls_stack_certificate_arn=""
  if ((describe_tls_stack_status == 0)); then
    tls_stack_project="$(
      jq -r '[.Stacks[0].Tags[]? | select(.Key == "Project") | .Value][0] // ""' \
        <<<"${tls_stack_description}"
    )"
    tls_stack_example="$(
      jq -r '[.Stacks[0].Tags[]? | select(.Key == "Example") | .Value][0] // ""' \
        <<<"${tls_stack_description}"
    )"
    tls_stack_cluster_uid="$(
      jq -r --arg key "${STACK_CLUSTER_UID_TAG}" \
        '[.Stacks[0].Tags[]? | select(.Key == $key) | .Value][0] // ""' \
        <<<"${tls_stack_description}"
    )"
    if [[ "${tls_stack_project}" != "${STACK_PROJECT_VALUE}" || \
      "${tls_stack_example}" != "${STACK_EXAMPLE_VALUE}" || \
      "${tls_stack_cluster_uid}" != "${cluster_uid}" ]]; then
      echo "Refusing to use TLS stack ${TLS_STACK_NAME}: ownership or cluster identity does not match." >&2
      exit 1
    fi
    tls_stack_exists=true
    tls_stack_certificate_arn="$(
      jq -r '[.Stacks[0].Outputs[]? | select(.OutputKey == "CertificateArn") | .OutputValue][0] // ""' \
        <<<"${tls_stack_description}"
    )"
    tls_stack_domain_name="$(
      jq -r '([.Stacks[0].Parameters[]? | select(.ParameterKey == "CertificateDomainName") | .ParameterValue][0]
        // "") | ascii_downcase' <<<"${tls_stack_description}"
    )"
    tls_stack_hosted_zone_id="$(
      jq -r '[.Stacks[0].Parameters[]? | select(.ParameterKey == "HostedZoneId") | .ParameterValue][0] // ""' \
        <<<"${tls_stack_description}"
    )"
    if [[ "${tls_stack_domain_name%.}" != "${FIREHOSE_HOSTNAME}" || \
      "${tls_stack_hosted_zone_id##*/}" != "${hosted_zone_id}" ]]; then
      echo "TLS stack ${TLS_STACK_NAME} belongs to a different hostname or hosted zone." >&2
      echo "Run cleanup.sh before changing the automatic certificate's DNS settings." >&2
      exit 1
    fi
  elif [[ "${tls_stack_description}" != *"does not exist"* ]]; then
    echo "Unable to determine whether TLS stack ${TLS_STACK_NAME} already exists:" >&2
    echo "${tls_stack_description}" >&2
    exit 1
  fi

  if [[ -z "${ACM_CERTIFICATE_ARN}" ]]; then
    echo "Creating or updating DNS-validated certificate stack ${TLS_STACK_NAME}."
    aws cloudformation deploy \
      --region "${EXAMPLE_AWS_REGION}" \
      --stack-name "${TLS_STACK_NAME}" \
      --template-file "${DIR}/certificate.yaml" \
      --no-fail-on-empty-changeset \
      --tags "Project=${STACK_PROJECT_VALUE}" "Example=${STACK_EXAMPLE_VALUE}" \
        "${STACK_CLUSTER_UID_TAG}=${cluster_uid}" \
      --parameter-overrides \
        "CertificateDomainName=${FIREHOSE_HOSTNAME}" \
        "HostedZoneId=${hosted_zone_id}" \
        "KubernetesClusterUID=${cluster_uid}"
    ACM_CERTIFICATE_ARN="$(
      aws cloudformation describe-stacks \
        --region "${EXAMPLE_AWS_REGION}" \
        --stack-name "${TLS_STACK_NAME}" \
        --query 'Stacks[0].Outputs[?OutputKey==`CertificateArn`].OutputValue | [0]' \
        --output text
    )"
    tls_stack_exists=true
  elif [[ "${tls_stack_exists}" == "true" && \
    "${tls_stack_certificate_arn}" != "${ACM_CERTIFICATE_ARN}" ]]; then
    echo "TLS stack ${TLS_STACK_NAME} already owns a different certificate." >&2
    echo "Unset ACM_CERTIFICATE_ARN to reuse it, or run cleanup.sh before selecting another certificate." >&2
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
  if [[ "${tls_stack_exists}" == "true" ]]; then
    certificate_source="managed by ${TLS_STACK_NAME}"
  else
    certificate_source="caller supplied"
  fi
}

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
keda_crd_exists=false
if kubectl get customresourcedefinition scaledobjects.keda.sh >/dev/null 2>&1; then
  keda_crd_exists=true
fi

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
elif ((keda_operator_count != 1 || keda_metrics_server_count != 1)) || \
  [[ "${keda_crd_exists}" != "true" ]]; then
  echo "Found an incomplete or ambiguous KEDA installation." >&2
  echo "Expected the ScaledObject CRD, one operator, and one metrics API server Deployment." >&2
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
if ! kubectl get customresourcedefinition scaledobjects.keda.sh >/dev/null 2>&1; then
  echo "KEDA's ScaledObject CRD is unavailable after installation." >&2
  exit 1
fi
kubectl wait --for=condition=Available apiservice/v1beta1.external.metrics.k8s.io \
  --timeout=5m

ensure_certificate

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
  Certificate:        ${ACM_CERTIFICATE_ARN} (${certificate_source})
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
