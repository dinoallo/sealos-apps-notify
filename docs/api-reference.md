# API Reference

All `/api/v1/*` endpoints require authentication except `GET /health`. The recommended authentication method is headers:

```http
X-App-Id: notify-console
X-App-Secret: dev-secret
```

`Authorization: Bearer <appId>:<appSecret>` is also supported.

Credential files support YAML or JSON:

```yaml
apps:
  - appId: notify-console
    appSecret: CHANGE_ME_TO_A_LONG_RANDOM_SECRET
    name: Notify Console
    enabled: true
```

Disabled credentials are ignored. Credential file changes are hot reloaded when the auth watcher is running.

## Notifications

### `POST /api/v1/notifications` - Send a Notification

Request body:

```json
{
  "idempotencyKey": "unique key; repeated requests with the same key run once",
  "channels": {
    "feishu_app": {
      "template": "feishu-alert",
      "params": {"incident": "database primary unavailable", "severity": "P0"}
    },
    "email": {
      "template": "email-alert",
      "params": {"incident": "database primary unavailable", "severity": "P0"}
    }
  },
  "recipients": [
    {"type": "feishu_user_id", "value": "ou_xxxxxxxx"},
    {"type": "email", "value": "alice@example.com"},
    {"type": "phone", "value": "+8613800000000"}
  ]
}
```

- `channels`: a map from channel name to `{template, params}`.
- `template`: the template name stored in the database. This field is required for each channel.
- `params`: values injected into the template. All recipients for the same channel share the same rendered content.
- `recipients`: a list of delivery addresses.
- `type`: the address type used to match recipients to channels.
- `value`: the concrete delivery address, such as an Open ID, email address, or phone number.

Recipient `type` to channel mapping:

| `type` value | Matching channels |
| --- | --- |
| `email` | `email`, `feishu_app`, `feishu_webhook` |
| `phone` | `sms`, `voice` |
| `user_id` | `inapp` |
| `feishu_user_id` | `feishu_app`, `feishu_webhook` |

Response:

```json
{"notificationId": "uuid", "status": "accepted"}
```

### `GET /api/v1/notifications/:id` - Get Notification Status

Returns the notification details and all delivery tasks, including `senderAppId`.

### `GET /api/v1/notifications/:id/deliveries` - List Delivery Tasks

Returns all delivery tasks for the notification.

## Template Management

Templates are stored in the database and managed through the API. Each template belongs to one channel and contains a Go `text/template` body plus an optional subject for email.

### `POST /api/v1/templates` - Create a Template

Request body:

```json
{
  "name": "feishu-incident",
  "channel": "feishu_app",
  "description": "Feishu incident alert template",
  "subject": "",
  "body": "[{{ .severity }}] {{ .incident }}\nAffected user: {{ .name }}",
  "msgType": "text",
  "templateCode": ""
}
```

| Field | Description |
| --- | --- |
| `name` | Unique template name used by notification requests. Required. |
| `channel` | Channel name, such as `feishu_app`, `email`, or `sms`. Required. |
| `body` | Message body written with Go `text/template` syntax. |
| `subject` | Email subject, also rendered with Go `text/template`. |
| `msgType` | Message format used by Feishu channels: `text`, `post`, or `interactive`. |
| `templateCode` | Provider-side template code used by SMS or voice channels. |

Response: the created template object with HTTP `201`.

### `GET /api/v1/templates` - List Templates

Use `?channel=feishu_app` to filter by channel.

### `GET /api/v1/templates/:name` - Get a Template

Returns the template identified by `name`.

### `PUT /api/v1/templates/:name` - Update a Template

The request body uses the same shape as create. `name` and `channel` are not changed by this endpoint; the target template is selected by the URL parameter.

### `DELETE /api/v1/templates/:name` - Delete a Template

Deletes the template identified by `name`.

## Health Check

### `GET /health`

Returns `200 {"status":"healthy"}` when the database is reachable.
