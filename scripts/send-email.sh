#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  scripts/send-email.sh --to alice@example.com,bob@example.com --template maintenance-email --params '{"name":"Alice"}'
  scripts/send-email.sh --create-template --template maintenance-email --subject "Maintenance" --body '<p>Hello</p>'

Environment:
  NOTIFY_URL          Base URL. Default: http://localhost:8080
  NOTIFY_APP_ID       API credential app id. Default: notify-console
  NOTIFY_APP_SECRET   API credential secret. Required unless auth is disabled.

Options:
  --to EMAILS             Comma-separated recipient emails.
  --template NAME         Template name. Default: manual-email.
  --params JSON           Template params JSON object. Default: {}.
  --idempotency-key KEY   Idempotency key. Default: manual-email-<unix timestamp>.
  --create-template       Create an email template instead of sending.
  --subject SUBJECT       Template subject when creating a template.
  --body BODY             Template body when creating a template.
USAGE
}

notify_url="${NOTIFY_URL:-http://localhost:8080}"
app_id="${NOTIFY_APP_ID:-notify-console}"
app_secret="${NOTIFY_APP_SECRET:-}"
template="manual-email"
to=""
params="{}"
idempotency_key="manual-email-$(date +%s)"
create_template=false
subject=""
body=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --to)
      to="${2:-}"
      shift 2
      ;;
    --template)
      template="${2:-}"
      shift 2
      ;;
    --params)
      params="${2:-}"
      shift 2
      ;;
    --idempotency-key)
      idempotency_key="${2:-}"
      shift 2
      ;;
    --create-template)
      create_template=true
      shift
      ;;
    --subject)
      subject="${2:-}"
      shift 2
      ;;
    --body)
      body="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "$template" ]]; then
  echo "--template is required" >&2
  exit 2
fi

tmp_payload="$(mktemp)"
cleanup() {
  rm -f "$tmp_payload"
}
trap cleanup EXIT

if [[ "$create_template" == true ]]; then
  if [[ -z "$subject" || -z "$body" ]]; then
    echo "--subject and --body are required with --create-template" >&2
    exit 2
  fi

  TEMPLATE="$template" SUBJECT="$subject" BODY="$body" python3 - <<'PY' >"$tmp_payload"
import json
import os

print(json.dumps({
    "name": os.environ["TEMPLATE"],
    "channel": "email",
    "subject": os.environ["SUBJECT"],
    "body": os.environ["BODY"],
}))
PY

  endpoint="${notify_url%/}/api/v1/templates"
else
  if [[ -z "$to" ]]; then
    echo "--to is required when sending" >&2
    exit 2
  fi

  TO="$to" TEMPLATE="$template" PARAMS="$params" IDEMPOTENCY_KEY="$idempotency_key" python3 - <<'PY' >"$tmp_payload"
import json
import os
import sys

try:
    params = json.loads(os.environ["PARAMS"])
except json.JSONDecodeError as exc:
    print(f"--params must be a JSON object: {exc}", file=sys.stderr)
    sys.exit(2)

if not isinstance(params, dict):
    print("--params must be a JSON object", file=sys.stderr)
    sys.exit(2)

recipients = [
    {"type": "email", "value": item.strip()}
    for item in os.environ["TO"].split(",")
    if item.strip()
]
if not recipients:
    print("--to did not contain any email address", file=sys.stderr)
    sys.exit(2)

print(json.dumps({
    "idempotencyKey": os.environ["IDEMPOTENCY_KEY"],
    "channels": {
        "email": {
            "template": os.environ["TEMPLATE"],
            "params": params,
        },
    },
    "recipients": recipients,
}))
PY

  endpoint="${notify_url%/}/api/v1/notifications"
fi

curl_args=(
  -sS
  -X POST
  "$endpoint"
  -H "Content-Type: application/json"
  -d "@$tmp_payload"
)

if [[ -n "$app_id" ]]; then
  curl_args+=(-H "X-App-Id: $app_id")
fi
if [[ -n "$app_secret" ]]; then
  curl_args+=(-H "X-App-Secret: $app_secret")
fi

curl "${curl_args[@]}"
echo
