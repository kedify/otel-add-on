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
TLS_STACK_NAME="${TLS_STACK_NAME:-${STACK_NAME}-tls}"

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

# Capture the automatically managed certificate's validation record before the
# certificate is deleted. CloudFormation creates this Route 53 CNAME for ACM,
# but ACM intentionally leaves validation records behind when certificates are
# removed.
tls_stack_exists=false
validation_record_name=""
validation_record_type=""
validation_record_value=""
tls_hosted_zone_id=""
set +e
tls_stack_description="$(
  aws cloudformation describe-stacks \
    --region "${EXAMPLE_AWS_REGION}" \
    --stack-name "${TLS_STACK_NAME}" \
    --output json 2>&1
)"
describe_tls_stack_status=$?
set -e
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
    echo "Refusing to delete TLS stack ${TLS_STACK_NAME}: ownership or cluster identity does not match." >&2
    exit 1
  fi
  tls_stack_exists=true
  tls_certificate_arn="$(
    jq -r '[.Stacks[0].Outputs[]? | select(.OutputKey == "CertificateArn") | .OutputValue][0] // ""' \
      <<<"${tls_stack_description}"
  )"
  tls_hosted_zone_id="$(
    jq -r '([.Stacks[0].Outputs[]? | select(.OutputKey == "HostedZoneId") | .OutputValue][0]
      // [.Stacks[0].Parameters[]? | select(.ParameterKey == "HostedZoneId") | .ParameterValue][0]
      // "")' <<<"${tls_stack_description}"
  )"
  if [[ -n "${tls_certificate_arn}" ]]; then
    set +e
    certificate_description="$(
      aws acm describe-certificate \
        --region "${EXAMPLE_AWS_REGION}" \
        --certificate-arn "${tls_certificate_arn}" \
        --output json 2>&1
    )"
    describe_certificate_status=$?
    set -e
    if ((describe_certificate_status == 0)); then
      validation_record_name="$(
        jq -r '.Certificate.DomainValidationOptions[0].ResourceRecord.Name // ""' \
          <<<"${certificate_description}"
      )"
      validation_record_type="$(
        jq -r '.Certificate.DomainValidationOptions[0].ResourceRecord.Type // ""' \
          <<<"${certificate_description}"
      )"
      validation_record_value="$(
        jq -r '.Certificate.DomainValidationOptions[0].ResourceRecord.Value // ""' \
          <<<"${certificate_description}"
      )"
    elif [[ "${certificate_description}" != *"ResourceNotFound"* ]]; then
      echo "Unable to inspect certificate ${tls_certificate_arn}:" >&2
      echo "${certificate_description}" >&2
      exit 1
    fi
  fi
elif [[ "${tls_stack_description}" != *"does not exist"* ]]; then
  echo "Unable to determine whether TLS stack ${TLS_STACK_NAME} exists:" >&2
  echo "${tls_stack_description}" >&2
  exit 1
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

if [[ "${tls_stack_exists}" == "true" ]]; then
  if [[ -n "${validation_record_name}" && \
    -n "${validation_record_type}" && \
    -n "${validation_record_value}" && \
    -n "${tls_hosted_zone_id}" ]]; then
    validation_record_set="$(
      aws route53 list-resource-record-sets \
        --hosted-zone-id "${tls_hosted_zone_id}" \
        --start-record-name "${validation_record_name}" \
        --start-record-type "${validation_record_type}" \
        --max-items 1 \
        --output json | \
        jq -c \
          --arg name "${validation_record_name}" \
          --arg type "${validation_record_type}" \
          --arg value "${validation_record_value}" \
          '[.ResourceRecordSets[]?
            | select(.Name == $name and .Type == $type)
            | select(.ResourceRecords == [{"Value": $value}])][0] // empty'
    )"
    if [[ -n "${validation_record_set}" ]]; then
      delete_change_batch="$(
        jq -cn --argjson record_set "${validation_record_set}" \
          '{Changes: [{Action: "DELETE", ResourceRecordSet: $record_set}]}'
      )"
      change_id="$(
        aws route53 change-resource-record-sets \
          --hosted-zone-id "${tls_hosted_zone_id}" \
          --change-batch "${delete_change_batch}" \
          --query 'ChangeInfo.Id' \
          --output text
      )"
      aws route53 wait resource-record-sets-changed --id "${change_id}"
      echo "Deleted ACM validation record ${validation_record_name}."
    fi
  fi

  echo "Deleting automatically managed certificate stack ${TLS_STACK_NAME}."
  aws cloudformation delete-stack \
    --region "${EXAMPLE_AWS_REGION}" \
    --stack-name "${TLS_STACK_NAME}"
  aws cloudformation wait stack-delete-complete \
    --region "${EXAMPLE_AWS_REGION}" \
    --stack-name "${TLS_STACK_NAME}"
fi

echo "Cleanup complete. The KEDA installation was left untouched."
