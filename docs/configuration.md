# Configuration Reference

See [`config.example.yaml`](../config.example.yaml) for a complete example.

## `server`

| Field | Default | Description |
| --- | --- | --- |
| `address` | `:8080` | HTTP listen address. |
| `readTimeout` | `30s` | HTTP read timeout. |
| `writeTimeout` | `30s` | HTTP write timeout. |
| `idleTimeout` | `60s` | HTTP idle timeout. |

## `database`

| Field | Default | Description |
| --- | --- | --- |
| `host` | `localhost` | PostgreSQL host. |
| `port` | `5432` | PostgreSQL port. |
| `user` | `postgres` | PostgreSQL user. |
| `password` | | Database password. |
| `dbname` | `sealos_notify` | Database name. |
| `sslMode` | `disable` | PostgreSQL SSL mode. |
| `maxOpenConns` | `25` | Maximum open connections. |
| `maxIdleConns` | `5` | Maximum idle connections. |
| `connMaxLifetime` | `5m` | Maximum connection lifetime. |

## `dispatcher`

| Field | Default | Description |
| --- | --- | --- |
| `enabled` | `true` | Enables the dispatcher. |
| `interval` | `10s` | Queue polling interval. |
| `batchSize` | `100` | Maximum number of pending and retry tasks claimed per cycle. |
| `leaseTimeout` | `5m` | Processing lease timeout. Expired tasks can be reclaimed by another replica. |

## `auth`

| Field | Default | Description |
| --- | --- | --- |
| `enabled` | `true` | Enables authentication for `/api/v1` endpoints. |
| `credentialsFilePath` | | Path to the app credential file, usually mounted from a Kubernetes Secret. |

## `defaults`

| Field | Default | Description |
| --- | --- | --- |
| `maxRetry` | `3` | Maximum retry count before a task is marked `dead`. |
| `retryBackoffSeconds` | `[30, 120, 300]` | Retry delay for each retry attempt. |

## `channels`

Each channel entry has this shape:

```yaml
channels:
  feishu_app:
    enabled: true
    provider: feishu-app-urgent   # References a provider name under providers.
```

## `providers`

Each provider uses `type` to select an adapter. The remaining fields are passed to the adapter constructor as provider data.

## Feishu Urgent Notifications

Feishu urgent notification is a Feishu app message feature. After the normal app message is created, the adapter can trigger an additional in-app urgent alert, SMS alert, or phone-call alert.

### Setup

1. Create an internal app in the [Feishu Open Platform](https://open.feishu.cn/app).
2. Enable these permissions in the permission management page:
   - `im:message:send_as_bot`: send messages as the bot.
   - `im:message.group_urgent_app:create`: in-app urgent alert (`urgentType: app`).
   - `im:message.group_urgent_sms:create`: SMS urgent alert (`urgentType: sms`).
   - `im:message.group_urgent_phone:create`: phone-call urgent alert (`urgentType: phone`).
3. Copy the App ID and App Secret from the app credentials page.
4. Add the bot to target groups, or make sure it can send direct messages to the target users.

### Provider Configuration

```yaml
channels:
  feishu_app:
    enabled: true
    provider: feishu-app-urgent

providers:
  feishu-app-urgent:
    type: feishu_app
    appId: "cli_xxxxxxxxxxxxxxxx"
    appSecret: "xxxxxxxxxxxxxxxx"
    receiveIdType: "open_id"    # open_id | user_id | union_id | email
    urgentUserIdType: "open_id" # open_id | user_id | union_id; defaults to receiveIdType except email/chat_id
    msgType: "text"             # text | post | interactive
    urgentType: "app"           # app | sms | phone | empty string disables urgent alerts
```

### Adapter Flow

1. Calls Feishu `im.v1.message.create` to send the message.
2. Extracts the returned `message_id` and calls the selected urgent API: `urgent_app`, `urgent_sms`, or `urgent_phone`.
3. Urgent API failures do not fail the main delivery because the message has already been created. The error is stored in `details.urgent_error`.

### `receiveIdType` to Recipient Key Mapping

| `receiveIdType` | Recipient key |
| --- | --- |
| `open_id` | `feishu_user_id` |
| `user_id` | `feishu_user_id` |
| `union_id` | `feishu_user_id` |
| `email` | `email` |

## Environment Variable Overrides

Configuration fields can be overridden with environment variables:

| Environment variable prefix | Configuration section |
| --- | --- |
| `SERVER_` | `server` |
| `DATABASE_` | `database` |
| `LOGGING_` | `logging` |
| `DISPATCHER_` | `dispatcher` |
| `AUTH_` | `auth` |

Example: `DATABASE_HOST=db.prod DATABASE_PASSWORD=secret ./sealos-notify -c config.yaml`
