# sealos-notify

sealos-notify is the unified notification service for the Sealos platform. It supports multi-channel delivery with reliable retries, idempotent requests, and horizontally scalable workers.

[中文部署说明](README.zh-CN.md)

## Features

- **Multiple channels**: in-app messages through CRDs, email, SMS, voice calls, Feishu webhooks, and Feishu app messages.
- **Feishu urgent notifications**: after sending a message, the Feishu app adapter can trigger in-app, SMS, or phone-call urgent reminders.
- **Template-driven content**: notification content is rendered from database-managed templates, with template CRUD exposed through the API.
- **API authentication and auditability**: all `/api/v1` endpoints use `appId` + `appSecret` authentication, and notifications store the sender `appId`.
- **Reliable delivery**: database-backed delivery queue, exponential backoff retries, and configurable retry limits.
- **Idempotent API**: repeated requests with the same `idempotencyKey` are handled safely.
- **High availability**: multiple replicas share the delivery queue and use database-level `FOR UPDATE SKIP LOCKED` to claim tasks without conflicts.
- **Hot reload**: channel, provider, and authentication credential changes can be reloaded without restarting the service.
- **Graceful shutdown**: the service waits for in-flight delivery tasks before exiting.

## Architecture

```text
HTTP API -> Engine -> delivery_tasks table -> Dispatcher -> Channel Adapters
                                                |
                                                v
                                      delivery_attempts table
```

1. `POST /api/v1/notifications` creates a notification record, recipient records, and delivery tasks. A task is generated for each compatible recipient and channel pair.
2. The **Dispatcher** polls the queue at the configured interval. It concurrently claims pending and retry-ready tasks with `FOR UPDATE SKIP LOCKED`.
3. Each task runs in its own goroutine. The dispatcher loads the template, renders content, calls the configured **Adapter**, records the result in `delivery_attempts`, and schedules retries with backoff. Tasks that exceed `maxRetry` are marked `dead`.

## Quick Start

### Prerequisites

- Go 1.21+
- PostgreSQL 14+

### 1. Clone and Prepare Configuration

```bash
git clone https://github.com/labring/sealos-notify.git
cd sealos-notify
cp config.example.yaml config.yaml
# Edit config.yaml for your database and channel settings.
```

### 2. Start PostgreSQL for Development

```bash
docker run -d --name postgres \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=sealos_notify \
  -p 5432:5432 postgres:16-alpine
```

### 3. Run the Service

For local development, create an API credential file first:

```bash
mkdir -p /tmp/sealos-notify-auth
cat >/tmp/sealos-notify-auth/apps.yaml <<'EOF'
apps:
  - appId: "notify-console"
    appSecret: "dev-secret"
    name: "Notify Console"
    enabled: true
EOF
```

Then set `auth.credentialsFilePath` in `config.yaml` to `/tmp/sealos-notify-auth/apps.yaml`.

```bash
go run . -c config.yaml
```

For detailed local debugging, set `logging.debug: true` and `logging.format: debug`. This enables debug-level logs and prints each HTTP request with method, path, query, headers, remote address, and raw body. Sensitive credential headers such as `Authorization` and `X-App-Secret` are redacted.

Or run with Docker:

```bash
docker build -t sealos-notify .
docker run -p 8080:8080 -v $(pwd)/config.yaml:/config.yaml sealos-notify -c /config.yaml
```

### 4. Create a Template and Send a Notification

```bash
# Create a template.
curl -X POST http://localhost:8080/api/v1/templates \
  -H "Content-Type: application/json" \
  -H "X-App-Id: notify-console" \
  -H "X-App-Secret: dev-secret" \
  -d '{
    "name": "feishu-alert",
    "channel": "feishu_app",
    "msgType": "text",
    "body": "[Alert] {{ .incident }} (severity: {{ .severity }})"
  }'

# Send a notification. Template parameters live under channels;
# recipients use the {type, value} structure.
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -H "X-App-Id: notify-console" \
  -H "X-App-Secret: dev-secret" \
  -d '{
    "idempotencyKey": "incident-001",
    "channels": {
      "feishu_app": {
        "template": "feishu-alert",
        "params": {"incident": "database primary unavailable", "severity": "P0"}
      }
    },
    "recipients": [
      {"type": "feishu_user_id", "value": "ou_xxxxxxxx"},
      {"type": "feishu_user_id", "value": "ou_yyyyyyyy"}
    ]
  }'
```

## API

All `/api/v1/*` endpoints require authentication except `GET /health`. See the [API reference](docs/api-reference.md) for authentication, notification, template, and health-check details.

## Configuration

See the [configuration reference](docs/configuration.md) and [`config.example.yaml`](config.example.yaml) for runtime settings, provider configuration, Feishu urgent notifications, and environment variable overrides.

## Project Layout

```text
sealos-notify/
├── main.go                         # Program entrypoint
├── config.example.yaml             # Example configuration
├── pkg/
│   ├── config/                     # Configuration loading and hot reload
│   ├── logger/                     # Logger setup
│   ├── database/                   # GORM database connection and schema initialization
│   ├── storage/                    # Data access layer
│   │   ├── notification.go         # Notification and recipient storage
│   │   ├── delivery.go             # Delivery task and attempt storage
│   │   └── template.go             # Template CRUD storage
│   ├── render/                     # Template rendering with text/template
│   ├── engine/                     # Request validation and task generation
│   ├── dispatcher/                 # Queue polling, dispatch, and retry logic
│   └── adapter/
│       ├── adapter.go              # Adapter interface definitions
│       └── feishu_app/             # Feishu app urgent notification adapter
├── server/                         # HTTP server, routes, and handlers
└── deploy/kubernetes/              # Kubernetes manifests
```

## Adding a New Channel

1. Create `pkg/adapter/<channel_name>/` and implement the `adapter.Adapter` interface:

   ```go
   type Adapter interface {
       Send(ctx context.Context, request *SendRequest) (*SendResponse, error)
       Name() string
       ChannelType() ChannelType
       Validate() error
   }
   ```

2. Add the recipient identifier mapping in `RecipientIdentifierKeys()` in `pkg/adapter/adapter.go`.
3. Register the provider type in `server/server.go`:

   ```go
   case "my_channel":
       a, err := mychannel.New(providerConfig.Data)
       s.adapters[providerName] = a
   ```

4. Add example channel and provider configuration to `config.example.yaml`.

## Kubernetes Deployment

See the [Kubernetes deployment guide](docs/deployment.md) for Helm, cluster image, raw manifest, and Secret management instructions.

## Build

```bash
make build         # Build the binary.
make docker-build  # Build the Docker image.
make run           # Run locally; requires PostgreSQL.
make test          # Run unit tests with race detection and coverage.
```

## License

Apache 2.0
