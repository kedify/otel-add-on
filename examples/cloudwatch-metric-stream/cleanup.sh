#!/usr/bin/env bash

set -euo pipefail

NAMESPACE="cloudwatch-metric-stream"
NAMESPACE_OWNER_LABEL="examples.kedify.io/owner"
NAMESPACE_CLUSTER_ANNOTATION="examples.kedify.io/cluster-uid"
NAMESPACE_STACK_ANNOTATION="examples.kedify.io/stack-name"
NAMESPACE_BUCKET_ANNOTATION="examples.kedify.io/failed-delivery-bucket"
STACK_PROJECT_VALUE="otel-add-on"
STACK_EXAMPLE_VALUE="cloudwatch-metric-stream"
STACK_CLUSTER_UID_TAG="KubernetesClusterUID"
STACK_NAME="${STACK_NAME:-kedify-cloudwatch-metric-stream}"

EXAMPLE_AWS_REGION="${AWS_REGION:-${AWS_DEFAULT_REGION:-}}"
if [[ -z "${EXAMPLE_AWS_REGION}" ]]; then
  EXAMPLE_AWS_REGION="$(aws configure get region 2>/dev/null || true)"
fi
if [[ -z "${EXAMPLE_AWS_REGION}" ]]; then
  echo "Set AWS_REGION (or configure a default AWS Region)." >&2
  exit 1
fi

for command_name in aws kubectl jq; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "Missing required command: ${command_name}" >&2
    exit 1
  fi
done

KUBE_CONTEXT="${KUBE_CONTEXT:-$(command kubectl config current-context)}"
kubectl() {
  command kubectl --context "${KUBE_CONTEXT}" "$@"
}

echo "Using Kubernetes context: ${KUBE_CONTEXT}"
cluster_uid="$(kubectl get namespace kube-system -o jsonpath='{.metadata.uid}')"

# Refuse to delete a same-named namespace unless setup.sh marked it as owned by
# this stack on this exact cluster.
namespace_exists=false
namespace_backup_bucket=""
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
    echo "Refusing to delete namespace ${NAMESPACE}: ownership or cluster identity does not match." >&2
    exit 1
  fi
  namespace_exists=true
fi

backup_bucket="${namespace_backup_bucket}"
set +e
stack_description="$(
  aws cloudformation describe-stacks \
    --region "${EXAMPLE_AWS_REGION}" \
    --stack-name "${STACK_NAME}" \
    --output json 2>&1
)"
describe_stack_status=$?
set -e
if ((describe_stack_status == 0)); then
  stack_project="$(
    jq -r '[.Stacks[0].Tags[]? | select(.Key == "Project") | .Value][0] // ""' \
      <<<"${stack_description}"
  )"
  stack_example="$(
    jq -r '[.Stacks[0].Tags[]? | select(.Key == "Example") | .Value][0] // ""' \
      <<<"${stack_description}"
  )"
  stack_cluster_uid="$(
    jq -r --arg key "${STACK_CLUSTER_UID_TAG}" \
      '[.Stacks[0].Tags[]? | select(.Key == $key) | .Value][0] // ""' \
      <<<"${stack_description}"
  )"
  if [[ "${stack_project}" != "${STACK_PROJECT_VALUE}" || \
    "${stack_example}" != "${STACK_EXAMPLE_VALUE}" || \
    "${stack_cluster_uid}" != "${cluster_uid}" ]]; then
    echo "Refusing to delete stack ${STACK_NAME}: ownership or cluster identity does not match." >&2
    exit 1
  fi

  stack_backup_bucket="$(
    jq -r '[.Stacks[0].Outputs[]? | select(.OutputKey == "FailedDeliveryBucketName") | .OutputValue][0] // ""' \
      <<<"${stack_description}"
  )"
  if [[ -n "${stack_backup_bucket}" ]]; then
    backup_bucket="${stack_backup_bucket}"
  fi

  echo "Deleting CloudFormation stack ${STACK_NAME}."
  aws cloudformation delete-stack \
    --region "${EXAMPLE_AWS_REGION}" \
    --stack-name "${STACK_NAME}"
  aws cloudformation wait stack-delete-complete \
    --region "${EXAMPLE_AWS_REGION}" \
    --stack-name "${STACK_NAME}"
elif [[ "${stack_description}" != *"does not exist"* ]]; then
  echo "Unable to determine whether stack ${STACK_NAME} exists:" >&2
  echo "${stack_description}" >&2
  exit 1
fi

# If a prior cleanup was interrupted after stack deletion, recover the retained
# bucket by the four tags that bind it to this example and Kubernetes cluster.
if [[ -z "${backup_bucket}" ]]; then
  backup_bucket_candidates="$(
    aws resourcegroupstaggingapi get-resources \
      --region "${EXAMPLE_AWS_REGION}" \
      --resource-type-filters s3 \
      --tag-filters \
        "Key=Name,Values=${STACK_NAME}-firehose-failed-deliveries" \
        "Key=Project,Values=${STACK_PROJECT_VALUE}" \
        "Key=Example,Values=${STACK_EXAMPLE_VALUE}" \
        "Key=${STACK_CLUSTER_UID_TAG},Values=${cluster_uid}" \
      --output json
  )"
  backup_bucket_count="$(jq '.ResourceTagMappingList | length' <<<"${backup_bucket_candidates}")"
  if ((backup_bucket_count > 1)); then
    echo "Refusing to choose among ${backup_bucket_count} retained buckets with matching ownership tags." >&2
    echo "Inspect them with the Resource Groups Tagging API and remove the intended buckets explicitly." >&2
    exit 1
  elif ((backup_bucket_count == 1)); then
    backup_bucket_arn="$(jq -r '.ResourceTagMappingList[0].ResourceARN' <<<"${backup_bucket_candidates}")"
    backup_bucket="${backup_bucket_arn##*:::}"
  fi
fi

# The stack deliberately retains this bucket so stack deletion cannot fail when
# Firehose has written a failed-delivery object. Check its explicit ownership
# tags before emptying it now that the delivery stream no longer exists.
if [[ -n "${backup_bucket}" && "${backup_bucket}" != "None" ]]; then
  set +e
  bucket_tags="$(
    aws s3api get-bucket-tagging \
      --bucket "${backup_bucket}" \
      --region "${EXAMPLE_AWS_REGION}" \
      --output json 2>&1
  )"
  get_bucket_tags_status=$?
  set -e
  if ((get_bucket_tags_status == 0)); then
    bucket_name_tag="$(
      jq -r '[.TagSet[]? | select(.Key == "Name") | .Value][0] // ""' <<<"${bucket_tags}"
    )"
    bucket_project_tag="$(
      jq -r '[.TagSet[]? | select(.Key == "Project") | .Value][0] // ""' <<<"${bucket_tags}"
    )"
    bucket_example_tag="$(
      jq -r '[.TagSet[]? | select(.Key == "Example") | .Value][0] // ""' <<<"${bucket_tags}"
    )"
    bucket_cluster_uid_tag="$(
      jq -r --arg key "${STACK_CLUSTER_UID_TAG}" \
        '[.TagSet[]? | select(.Key == $key) | .Value][0] // ""' <<<"${bucket_tags}"
    )"
    if [[ "${bucket_name_tag}" != "${STACK_NAME}-firehose-failed-deliveries" || \
      "${bucket_project_tag}" != "${STACK_PROJECT_VALUE}" || \
      "${bucket_example_tag}" != "${STACK_EXAMPLE_VALUE}" || \
      "${bucket_cluster_uid_tag}" != "${cluster_uid}" ]]; then
      echo "Refusing to empty bucket ${backup_bucket}: ownership tags do not match." >&2
      exit 1
    fi

    echo "Emptying and deleting retained bucket ${backup_bucket}."
    aws s3 rm "s3://${backup_bucket}" --recursive --region "${EXAMPLE_AWS_REGION}"
    aws s3api delete-bucket \
      --bucket "${backup_bucket}" \
      --region "${EXAMPLE_AWS_REGION}"
  elif [[ "${bucket_tags}" != *"NoSuchBucket"* ]]; then
    echo "Unable to verify retained bucket ${backup_bucket}:" >&2
    echo "${bucket_tags}" >&2
    exit 1
  fi
fi

if [[ "${namespace_exists}" == "true" ]]; then
  echo "Deleting namespace ${NAMESPACE}; the AWS Load Balancer Controller will remove both NLBs."
  kubectl delete namespace "${NAMESPACE}" --wait=true --timeout=10m
fi

echo "Cleanup complete. The KEDA installation was left untouched."
