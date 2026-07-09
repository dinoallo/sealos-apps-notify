# Manual Email Trigger

Before the service is integrated with product events, email delivery can be triggered manually through the existing notification API.

## 1. Configure SMTP

In `config.yaml`, enable the `email` channel and point it to an SMTP provider:

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

Set the password before starting the service:

```bash
export SMTP_PASSWORD=xxxx
go run . -c config.yaml
```

## 2. Create an Email Template

```bash
NOTIFY_APP_SECRET=dev-secret \
scripts/send-email.sh \
  --create-template \
  --template maintenance-email \
  --subject "Scheduled Maintenance Notice" \
  --body '<h1>Maintenance Notice</h1><p>Hello {{ .name }}, we will perform maintenance.</p>'
```

## 3. Send to Specific Emails

```bash
NOTIFY_APP_SECRET=dev-secret \
scripts/send-email.sh \
  --to alice@example.com,bob@example.com \
  --template maintenance-email \
  --params '{"name":"customer"}' \
  --idempotency-key maintenance-20260709-manual-001
```

Use a stable `--idempotency-key` for the same manual send. Reusing the same key prevents duplicate notification creation.

## Notes

- This flow does not query business databases for recipients.
- Recipients are the explicit `--to` email addresses.
- Delivery status can be checked with `GET /api/v1/notifications/:id` or `GET /api/v1/notifications/:id/deliveries`.
