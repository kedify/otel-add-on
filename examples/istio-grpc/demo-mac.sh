#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != Darwin ]]; then
  echo 'This demo requires macOS and iTerm2.' >&2
  exit 1
fi

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONTEXT=k3d-istio-grpc

k() { command kubectl --context="${CONTEXT}" "$@"; }
k_display() {
  if command -v kubecolor >/dev/null 2>&1; then
    # Keep colors when buffering a refresh; machine-readable queries use k().
    KUBECOLOR_PAGING=never command kubecolor --force-colors --context="${CONTEXT}" "$@"
  else
    k "$@"
  fi
}
heading() { printf '\033[1;36m%s\033[0m\n' "$*"; }

# Build the complete redraw before writing it, including errors and colors.
# Synchronized output lets iTerm2 present it as one update:
# https://contour-terminal.org/vt-extensions/synchronized-output/
render_frame() {
  local frame= line row=0 rows columns
  read -r rows columns < <(stty size 2>/dev/null) || rows="${LINES:-24}"
  while IFS= read -r line; do
    (( row >= rows )) && break
    row=$((row + 1))
    printf -v frame '%s\033[%d;1H\033[2K%s' "${frame}" "${row}" "${line}"
  done <<< "$1"
  if (( row < rows )); then
    printf -v frame '%s\033[%d;1H\033[J' "${frame}" "$((row + 1))"
  fi
  # Disable wrapping while drawing so narrow or short panes cannot scroll.
  printf '\033[?2026h\033[?25l\033[?7l%s\033[0m\033[?7h\033[?2026l' "${frame}"
}

watch_output() {
  local frame
  watch_active=1
  while true; do
    frame="$("$@" 2>&1)"
    render_frame "${frame}"
    sleep 2
  done
}

wait_for_setup() {
  heading 'Waiting for setup in the left pane...'
  until [[ -f "${run_dir}/ready" ]]; do
    if [[ -f "${run_dir}/failed" ]]; then
      echo 'Setup failed. See the left pane.' >&2
      exit 1
    fi
    sleep 1
  done
}

cleanup_pane() {
  if [[ "${watch_active:-0}" == 1 ]]; then
    printf '\033[?2026l\033[0m\033[?7h\033[?25h\n'
  fi
  if [[ -n "${log_pid:-}" ]]; then
    kill "${log_pid}" 2>/dev/null || true
    wait "${log_pid}" 2>/dev/null || true
  fi
  if [[ "${pane}" == deploy && ! -f "${run_dir}/ready" ]]; then
    touch "${run_dir}/failed"
  fi
  touch "${run_dir}/${pane}.done"
  local name
  for name in deploy scaler pods metrics scale; do
    [[ -f "${run_dir}/${name}.done" ]] || return 0
  done
  rm -rf "${run_dir}"
}

# Reconnect when pods are replaced or added, including producer scale-out.
follow_logs() {
  local namespace="$1" selector="$2" container="$3" pods current_pods
  while true; do
    pods="$(k -n "${namespace}" get pods -l "${selector}" -o name --request-timeout=3s 2>/dev/null || true)"
    if [[ -z "${pods}" ]]; then
      echo 'Waiting for pods...'
      sleep 2
      continue
    fi
    k_display -n "${namespace}" logs -l "${selector}" -c "${container}" \
      --follow --prefix --tail=10 --max-log-requests=20 --ignore-errors=true &
    log_pid=$!
    while kill -0 "${log_pid}" 2>/dev/null; do
      sleep 2
      if current_pods="$(k -n "${namespace}" get pods -l "${selector}" -o name --request-timeout=3s 2>/dev/null)"; then
        [[ "${current_pods}" == "${pods}" ]] || break
      fi
    done
    kill "${log_pid}" 2>/dev/null || true
    wait "${log_pid}" 2>/dev/null || true
    log_pid=
    sleep 1
  done
}

pods_snapshot() {
  heading 'All Kubernetes pods (refresh: 2s)'
  k_display get pods --all-namespaces --request-timeout=3s || true
}

metrics_snapshot() {
  local pods pod metrics value total unavailable
  heading 'Active inbound gRPC streams (refresh: 2s)'
  printf '%s\n\n' 'envoy_cluster_upstream_rq_active'
  total=0
  unavailable=0
  if pods="$(k -n istio-grpc get pods -l app=producer \
    --field-selector=status.phase=Running \
    -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' --request-timeout=3s)"; then
    printf '%-38s %s\n' POD ACTIVE
    for pod in ${pods}; do
      value=
      # The API server proxies to each pod's Envoy metrics port. No local
      # port-forward, in-container curl, or fixed replica list is needed.
      if metrics="$(k get --raw "/api/v1/namespaces/istio-grpc/pods/${pod}:15090/proxy/stats/prometheus" \
        --request-timeout=2s 2>/dev/null)"; then
        value="$(printf '%s\n' "${metrics}" | awk '
          /^envoy_cluster_upstream_rq_active\{/ && /cluster_name="inbound\|50051\|\|"/ {
            total += $NF; found = 1
          }
          END { if (found) printf "%.0f", total }
        ')"
      fi
      if [[ -n "${value}" ]]; then
        total=$((total + value))
        printf '%-38s %s\n' "${pod}" "${value}"
      else
        unavailable=$((unavailable + 1))
        printf '%-38s %s\n' "${pod}" '?'
      fi
    done
    if [[ -z "${pods}" ]]; then
      echo 'Waiting for running producer pods...'
    elif (( unavailable > 0 )); then
      printf '\nObserved total: %s (%s pods unavailable)\n' "${total}" "${unavailable}"
    else
      printf '\nTotal active streams: %s\n' "${total}"
    fi
  else
    echo 'Waiting for the Kubernetes API...'
  fi
}

if [[ "${1:-}" == --pane ]]; then
  pane="${2:?Missing pane name}"
  run_dir="${3:?Missing demo state directory}"
  source "${run_dir}/environment.sh"
  trap cleanup_pane EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM HUP
  case "${pane}" in
    deploy)
      heading 'Creating the cluster and deploying Helm charts'
      /bin/bash -x "${DIR}/setup.sh"
      heading 'ScaledObject producer: .spec'
      k -n istio-grpc get scaledobject producer -o json | python3 -c \
        'import json,sys; print(json.dumps(json.load(sys.stdin)["spec"], indent=2))'
      heading 'kubectl get hpa -n istio-grpc keda-hpa-producer'
      k_display get hpa -n istio-grpc keda-hpa-producer
      heading 'kubectl get deploy -n istio-grpc'
      k_display get deploy -n istio-grpc
      touch "${run_dir}/ready"
      printf '\n\033[1;33mPress any key to show the producer\047s log..\033[0m'
      IFS= read -r -s -n 1
      printf '\n'
      follow_logs istio-grpc app=producer producer
      ;;
    scaler)
      wait_for_setup
      heading 'OTel Scaler logs'
      follow_logs keda 'app.kubernetes.io/instance=keda-otel-scaler,app.kubernetes.io/name=otel-add-on' main
      ;;
    pods)
      watch_output pods_snapshot
      ;;
    metrics)
      wait_for_setup
      watch_output metrics_snapshot
      ;;
    scale)
      wait_for_setup
      # Keep subsequent interactive commands on the demo cluster without
      # switching the context in the user's shared kubeconfig.
      (umask 077; k config view --minify --flatten --raw >"${run_dir}/kubeconfig")
      export KUBECONFIG="${run_dir}/kubeconfig"
      printf '\033[1;33mRun the following command to scale out the consumers to 4 replicas\033[0m\n\n'
      # Display the command and wait for Return; compatible with macOS Bash 3.2.
      printf '$ kubectl --context=k3d-istio-grpc scale deployment -n istio-grpc consumer --replicas 4'
      IFS= read -r
      k_display scale deployment -n istio-grpc consumer --replicas 5
      heading "Kubernetes context: ${CONTEXT}"
      "${SHELL:-/bin/zsh}" -i
      ;;
    *) echo "Unknown demo pane: ${pane}" >&2; exit 1 ;;
  esac
  exit 0
fi

if (( $# > 0 )); then
  echo "Usage: ${0##*/}" >&2
  exit 1
fi

for command_name in osascript k3d docker helm kubectl curl python3; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "Missing required command: ${command_name}" >&2
    exit 1
  fi
done
if ! osascript -e 'path to application id "com.googlecode.iterm2"' >/dev/null 2>&1; then
  echo 'Install iTerm2 before running this demo.' >&2
  exit 1
fi

run_dir="$(mktemp -d "${TMPDIR:-/tmp}/istio-grpc-demo.XXXXXX")"
# New iTerm shells do not inherit variables supplied to this launcher. Preserve
# setup overrides, tool paths, and any explicitly selected kubeconfig.
for name in PATH KUBECONFIG K3S_IMAGE ISTIO_VERSION KEDA_VERSION \
  MESSAGE_INTERVAL_SECONDS MESSAGE_COUNT CONSUMER_START_DELAY_SECONDS \
  PRODUCER_CAPACITY PI_INITIAL_DIGITS PI_DIGITS_STEP PRODUCER_ADDRESS; do
  if [[ -n "${!name+x}" ]]; then
    printf 'export %s=%q\n' "${name}" "${!name}" >>"${run_dir}/environment.sh"
  fi
done

# Native iTerm2 AppleScript interface: https://iterm2.com/documentation-scripting.html
trap 'touch "${run_dir}/failed"' EXIT
osascript - "${DIR}/demo-mac.sh" "${run_dir}" <<'APPLESCRIPT'
on run arguments
    set demoScript to item 1 of arguments
    set stateDirectory to item 2 of arguments
    set commandPrefix to "clear; /bin/bash " & quoted form of demoScript & " --pane "
    set commandSuffix to " " & quoted form of stateDirectory
    tell application id "com.googlecode.iterm2"
        activate
        set demoWindow to (create window with default profile)
        set zoomed of demoWindow to true
        set demoBounds to bounds of demoWindow
        set deploySession to current session of demoWindow
        tell deploySession
            set name to "Deployment / producer logs"
            set totalColumns to columns
            set podsSession to (split vertically with default profile)
            set scalerSession to (split vertically with default profile)
        end tell
        tell scalerSession to set name to "OTel Scaler logs"
        -- Give pod listings half the height; metrics and controls a quarter each.
        tell podsSession
            set name to "All Kubernetes pods"
            set metricsSession to (split horizontally with default profile)
        end tell
        tell metricsSession
            set name to "Active gRPC streams per producer"
            set scaleSession to (split horizontally with default profile)
        end tell
        tell scaleSession to set name to "Scale consumers / press Return"
        -- Splitting can rebalance columns; resize once the entire layout exists.
        -- Reserve about 40% for the right column and keep the maximized bounds.
        set columns of podsSession to (totalColumns * 40) div 100
        set bounds of demoWindow to demoBounds
        tell podsSession to write text (commandPrefix & "pods" & commandSuffix)
        tell scalerSession to write text (commandPrefix & "scaler" & commandSuffix)
        tell metricsSession to write text (commandPrefix & "metrics" & commandSuffix)
        tell scaleSession to write text (commandPrefix & "scale" & commandSuffix)
        tell deploySession to write text (commandPrefix & "deploy" & commandSuffix)
        tell deploySession to select
        tell demoWindow to select
    end tell
end run
APPLESCRIPT
trap - EXIT
