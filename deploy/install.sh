#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
RELEASE_NAME=${RELEASE_NAME:-sealos-notify}
RELEASE_NAMESPACE=${RELEASE_NAMESPACE:-notify-system}
CHART_PATH=${CHART_PATH:-"${SCRIPT_DIR}/charts/sealos-notify"}
USER_VALUES_FILE=${USER_VALUES_FILE:-"${CHART_PATH}/sealos-notify-values.yaml"}
SECRETS_FILE=${SECRETS_FILE:-"${SCRIPT_DIR}/secrets.yaml"}
KUBECTL_BIN=${KUBECTL_BIN:-kubectl}
HELM_BIN=${HELM_BIN:-helm}

for command_name in "${KUBECTL_BIN}" "${HELM_BIN}"; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "Required command not found: ${command_name}" >&2
    exit 1
  fi
done

if [[ ! -f "${SECRETS_FILE}" ]]; then
  echo "Secret file not found: ${SECRETS_FILE}" >&2
  echo "Create it with: cp ${SCRIPT_DIR}/secrets.example.yaml ${SECRETS_FILE}" >&2
  exit 1
fi

# Catch the placeholders in the example before applying anything to the cluster.
if ! awk '!/^[[:space:]]*#/ && /YOUR_|REPLACE_WITH|CHANGE_ME|CHANGE_THIS/ { found = 1 } END { exit found }' "${SECRETS_FILE}"; then
  echo "Secret file still contains example placeholders: ${SECRETS_FILE}" >&2
  exit 1
fi

if [[ ! -f "${CHART_PATH}/values.yaml" ]]; then
  echo "Chart values file not found: ${CHART_PATH}/values.yaml" >&2
  exit 1
fi

if [[ -n "${USER_VALUES_FILE}" && ! -f "${USER_VALUES_FILE}" ]]; then
  echo "User values file not found: ${USER_VALUES_FILE}" >&2
  exit 1
fi

echo "Preparing namespace: ${RELEASE_NAMESPACE}"
"${KUBECTL_BIN}" create namespace "${RELEASE_NAMESPACE}" --dry-run=client -o yaml \
  | "${KUBECTL_BIN}" apply -f - >/dev/null

echo "Applying external Secrets from ${SECRETS_FILE}"
"${KUBECTL_BIN}" apply -n "${RELEASE_NAMESPACE}" -f "${SECRETS_FILE}"

VALUES_ARGS=(-f "${CHART_PATH}/values.yaml")
if [[ -n "${USER_VALUES_FILE}" ]]; then
  VALUES_ARGS+=(-f "${USER_VALUES_FILE}")
fi

RENDERED_FILE=$(mktemp)
trap 'rm -f "${RENDERED_FILE}"' EXIT

echo "Rendering Helm chart for Secret preflight"
"${HELM_BIN}" template "${RELEASE_NAME}" "${CHART_PATH}" \
  --namespace "${RELEASE_NAMESPACE}" "${VALUES_ARGS[@]}" > "${RENDERED_FILE}"

secret_name_for_env() {
  local env_name="$1"
  awk -v target="${env_name}" '
    $0 ~ "^[[:space:]]*- name: " target "$" { env_found = 1; next }
    env_found && /^[[:space:]]*- name:/ { exit }
    env_found && /secretKeyRef:/ { ref_found = 1; next }
    env_found && ref_found && /^[[:space:]]*name:/ { print $2; exit }
  ' "${RENDERED_FILE}"
}

auth_secret_name() {
  awk '
    /^[[:space:]]*volumes:/ { volumes_found = 1; next }
    volumes_found && /^[[:space:]]*- name: api-auth$/ { auth_found = 1; next }
    auth_found && /^[[:space:]]*secretName:/ { print $2; exit }
    auth_found && /^[[:space:]]*- name:/ { exit }
  ' "${RENDERED_FILE}"
}

MISSING_SECRET=0

check_secret_key() {
  local secret_name="$1"
  local secret_key="$2"
  local description="$3"
  local encoded_value
  local decoded_value

  if [[ -z "${secret_name}" ]]; then
    echo "Unable to determine Secret for ${description} from rendered chart" >&2
    MISSING_SECRET=1
    return
  fi

  if ! "${KUBECTL_BIN}" -n "${RELEASE_NAMESPACE}" get secret "${secret_name}" >/dev/null 2>&1; then
    echo "Missing Secret: ${RELEASE_NAMESPACE}/${secret_name} (key: ${secret_key})" >&2
    MISSING_SECRET=1
    return
  fi

  encoded_value=$("${KUBECTL_BIN}" -n "${RELEASE_NAMESPACE}" get secret "${secret_name}" \
    -o "jsonpath={.data['${secret_key}']}" 2>/dev/null || true)
  if [[ -z "${encoded_value}" ]]; then
    echo "Missing Secret key: ${RELEASE_NAMESPACE}/${secret_name}/${secret_key}" >&2
    MISSING_SECRET=1
    return
  fi

  decoded_value=$(printf '%s' "${encoded_value}" | base64 -d 2>/dev/null || true)
  if [[ -z "${decoded_value}" || "${decoded_value}" == *YOUR_* || "${decoded_value}" == *REPLACE_WITH* || "${decoded_value}" == *CHANGE_ME* || "${decoded_value}" == *CHANGE_THIS* ]]; then
    echo "Secret key is empty or still contains an example value: ${RELEASE_NAMESPACE}/${secret_name}/${secret_key}" >&2
    MISSING_SECRET=1
  fi
}

if auth_name=$(auth_secret_name) && [[ -n "${auth_name}" ]]; then
  check_secret_key "${auth_name}" apps.yaml "API authentication"
fi

if grep -q -- '- name: DATABASE_PASSWORD' "${RENDERED_FILE}"; then
  check_secret_key "$(secret_name_for_env DATABASE_PASSWORD)" password "database password"
fi

if grep -q -- '- name: FEISHU_WEBHOOK_URL' "${RENDERED_FILE}"; then
  webhook_secret_name=$(secret_name_for_env FEISHU_WEBHOOK_URL)
  check_secret_key "${webhook_secret_name}" webhook-url "Feishu webhook URL"
  check_secret_key "${webhook_secret_name}" webhook-secret "Feishu webhook signing secret"
fi

if grep -q -- '- name: SMTP_USERNAME' "${RENDERED_FILE}"; then
  smtp_secret_name=$(secret_name_for_env SMTP_USERNAME)
  check_secret_key "${smtp_secret_name}" username "SMTP username"
  check_secret_key "${smtp_secret_name}" password "SMTP password"
fi

if grep -q -- '- name: FEISHU_APP_ID' "${RENDERED_FILE}"; then
  feishu_secret_name=$(secret_name_for_env FEISHU_APP_ID)
  check_secret_key "${feishu_secret_name}" app-id "Feishu app ID"
  check_secret_key "${feishu_secret_name}" app-secret "Feishu app secret"
fi

if [[ "${MISSING_SECRET}" -ne 0 ]]; then
  echo "Secret preflight failed; Helm installation was not started." >&2
  exit 1
fi

echo "Installing ${RELEASE_NAME} in namespace ${RELEASE_NAMESPACE}"
"${HELM_BIN}" upgrade --install "${RELEASE_NAME}" "${CHART_PATH}" \
  --namespace "${RELEASE_NAMESPACE}" --create-namespace "${VALUES_ARGS[@]}"
