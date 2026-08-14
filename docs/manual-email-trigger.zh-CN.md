# 手动触发邮件

在服务还没有接入业务事件之前，可以通过现有通知 API 手动触发邮件发送。

## 1. 配置 SMTP

在 `config.yaml` 里启用 `email` channel，并指向 SMTP provider：

```yaml
channels:
  email:
    enabled: true
    provider: smtp-default

providers:
  smtp-default:
    type: smtp
    host: smtp.example.com
    port: 465
    username: notify@example.com
    password: "${SMTP_PASSWORD}"
    from: notify@example.com
    fromName: "Sealos Notify"
    tlsMode: auto
```

启动服务前设置密码：

```bash
export SMTP_PASSWORD=xxxx
go run . -c config.yaml
```

## 2. 创建邮件模板

```bash
NOTIFY_APP_SECRET=dev-secret \
scripts/send-email.sh \
  --create-template \
  --template maintenance-email \
  --subject "Scheduled Maintenance Notice" \
  --body '<h1>Maintenance Notice</h1><p>Hello {{ .name }}, we will perform maintenance.</p>'
```

## 3. 发送给指定邮箱

```bash
NOTIFY_APP_SECRET=dev-secret \
scripts/send-email.sh \
  --to alice@example.com,bob@example.com \
  --template maintenance-email \
  --params '{"name":"customer"}' \
  --idempotency-key maintenance-20260709-manual-001
```

同一次手动发送建议使用稳定的 `--idempotency-key`。重复使用同一个 key 可以避免重复创建通知。

## 注意事项

- 这条链路不会查询业务数据库收件人。
- 收件人就是 `--to` 显式传入的邮箱地址。
- 发送状态可以通过 `GET /api/v1/notifications/:id` 或 `GET /api/v1/notifications/:id/deliveries` 查询。
