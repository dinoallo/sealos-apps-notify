# Kubernetes Deployment

## Helm or Cluster Image Deployment

The Helm chart does not create application credentials. Create the external Secrets before installing the chart or running the cluster image entrypoint. The repository includes a local installer and a safe Secret template:

```bash
cp deploy/secrets.example.yaml deploy/secrets.yaml
# Fill in the enabled channel credentials. Do not commit deploy/secrets.yaml.
$EDITOR deploy/secrets.yaml
./deploy/install.sh
```

The installer creates the namespace, applies `deploy/secrets.yaml`, renders the chart to check the required Secret names and keys, and then runs `helm upgrade --install`. Set `RELEASE_NAMESPACE` to install into a different namespace. The cluster image uses the same Secret names from its user values file, so those Secrets must already exist in the release namespace before the image is installed.

| Secret | Keys | Required when |
| --- | --- | --- |
| `sealos-notify-pg-conn-credential` | `password` | Always; usually created by the PostgreSQL operator. |
| `sealos-notify-api-auth` | `apps.yaml` | API authentication is enabled. |
| `sealos-notify-feishu-webhook` | `webhook-url`, `webhook-secret` | Feishu Webhook is enabled. |
| `sealos-notify-smtp` | `username`, `password` | Email is enabled. |
| `sealos-notify-feishu` | `app-id`, `app-secret` | Feishu App is enabled. |

Do not put the credential values in Helm `values.yaml`. For production, replace the local Secret manifest with Sealed Secrets or External Secrets while keeping the same Secret names and keys.

## Raw Kubernetes Manifests

```bash
# Build and push the image to Docker Hub.
make docker-build IMAGE=docker.io/<dockerhub-user>/sealos-notify VERSION=test
make docker-push IMAGE=docker.io/<dockerhub-user>/sealos-notify VERSION=test

# Update deploy/kubernetes/deployment.yaml with the image name, then create the
# Secrets referenced by the raw manifests.
kubectl create namespace ns-admin --dry-run=client -o yaml | kubectl apply -f -
kubectl create secret generic sealos-notify-feishu \
  --from-literal=app-id=cli_xxxxxxxxxxxxxxxx \
  --from-literal=app-secret=xxxxxxxxxxxxxxxx \
  -n ns-admin
kubectl create secret generic sealos-notify-api-auth \
  --from-file=apps.yaml=/path/to/apps.yaml \
  -n ns-admin

# Deploy.
kubectl apply -f deploy/kubernetes/
```

The default test manifests use:

| Item | Value |
| --- | --- |
| namespace | `ns-admin` |
| PostgreSQL host | `sealos-notify-pg-postgresql-0.sealos-notify-pg-postgresql-hl.ns-admin.svc.cluster.local` |
| PostgreSQL Secret | `sealos-notify-pg-postgresql` / `postgres-password` |
| Service URL | `http://sealos-notify.ns-admin.svc.cluster.local:8080` |

Smoke-test the send path:

```bash
kubectl -n ns-admin port-forward svc/sealos-notify 8080:8080

curl -X POST http://localhost:8080/api/v1/templates \
  -H "Content-Type: application/json" \
  -H "X-App-Id: notify-console" \
  -H "X-App-Secret: CHANGE_ME_TO_A_LONG_RANDOM_SECRET" \
  -d '{"name":"feishu-urgent-test","channel":"feishu_app","body":"[Urgent test] {{ .message }}","msgType":"text"}'

curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -H "X-App-Id: notify-console" \
  -H "X-App-Secret: CHANGE_ME_TO_A_LONG_RANDOM_SECRET" \
  -d '{"idempotencyKey":"feishu-urgent-test-001","channels":{"feishu_app":{"template":"feishu-urgent-test","params":{"message":"sealos-notify staging integration test"}}},"recipients":[{"type":"feishu_user_id","value":"ou_xxxxxxxxxxxxxxxx"}]}'
```

For multiple replicas, update the Deployment `replicas` field. Replicas automatically share work through the database delivery queue.
