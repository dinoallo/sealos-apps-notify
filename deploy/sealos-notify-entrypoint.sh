#!/bin/bash
set -e

RELEASE_NAME=${RELEASE_NAME:-"sealos-notify"}
RELEASE_NAMESPACE=${RELEASE_NAMESPACE:-"notify-system"}
CHART_PATH=${CHART_PATH:-"./charts/sealos-notify"}
HELM_OPTS=${HELM_OPTS:-""}
HELM_OPTIONS=${HELM_OPTIONS:-""}
SERVICE_NAME="sealos-notify"
USER_VALUES_PATH="/root/.sealos/cloud/values/core/${SERVICE_NAME}-values.yaml"

AUTO_CONFIG_HELM_OPTS=""

add_set_string() {
  local key="$1"
  local value="$2"
  if [ -n "${value}" ]; then
    value=${value//\\/\\\\}
    value=${value//,/\\,}
    AUTO_CONFIG_HELM_OPTS="${AUTO_CONFIG_HELM_OPTS} --set-string ${key}=${value}"
  fi
}

adopt_namespaced_resource() {
  local namespace="$1"
  local kind="$2"
  local name="$3"
  if kubectl -n "${namespace}" get "${kind}" "${name}" >/dev/null 2>&1; then
    kubectl -n "${namespace}" label "${kind}" "${name}" app.kubernetes.io/managed-by=Helm --overwrite >/dev/null 2>&1 || true
    kubectl -n "${namespace}" annotate "${kind}" "${name}" meta.helm.sh/release-name="${RELEASE_NAME}" meta.helm.sh/release-namespace="${RELEASE_NAMESPACE}" --overwrite >/dev/null 2>&1 || true
  fi
}

add_set_string image.repository "${SEALOS_NOTIFY_IMAGE_REPOSITORY:-}"
add_set_string image.tag "${SEALOS_NOTIFY_IMAGE_TAG:-}"
add_set_string image.digest "${SEALOS_NOTIFY_IMAGE_DIGEST:-}"

if ! helm status "${RELEASE_NAME}" -n "${RELEASE_NAMESPACE}" >/dev/null 2>&1; then
  if kubectl get namespace "${RELEASE_NAMESPACE}" >/dev/null 2>&1; then
    kubectl label namespace "${RELEASE_NAMESPACE}" app.kubernetes.io/managed-by=Helm --overwrite >/dev/null 2>&1 || true
    kubectl annotate namespace "${RELEASE_NAMESPACE}" meta.helm.sh/release-name="${RELEASE_NAME}" meta.helm.sh/release-namespace="${RELEASE_NAMESPACE}" --overwrite >/dev/null 2>&1 || true
  fi

  adopt_namespaced_resource "${RELEASE_NAMESPACE}" configmap sealos-notify-config
  adopt_namespaced_resource "${RELEASE_NAMESPACE}" deployment sealos-notify
  adopt_namespaced_resource "${RELEASE_NAMESPACE}" service sealos-notify
  adopt_namespaced_resource "${RELEASE_NAMESPACE}" ingress sealos-notify
fi

if [ ! -f "${USER_VALUES_PATH}" ]; then
  mkdir -p "$(dirname "${USER_VALUES_PATH}")"
  cp "./charts/${SERVICE_NAME}/${SERVICE_NAME}-values.yaml" "${USER_VALUES_PATH}"
fi

HELM_ARGS="${AUTO_CONFIG_HELM_OPTS} ${HELM_OPTIONS} ${HELM_OPTS}"

helm upgrade -i "${RELEASE_NAME}" -n "${RELEASE_NAMESPACE}" --create-namespace "${CHART_PATH}" \
  -f "./charts/${SERVICE_NAME}/values.yaml" \
  -f "${USER_VALUES_PATH}" \
  ${HELM_ARGS}
