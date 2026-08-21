# 通知中心全工作流程

本文档说明 `sealos-notify` 从启动、配置、鉴权、模板管理、通知创建、任务调度、渠道投递、重试、状态查询到 Grafana 接入的完整工作流程。

## 1. 总体架构

`sealos-notify` 的核心设计是把一次通知请求拆成三层：

1. 通知请求：记录一次业务发送动作，写入 `notifications`。
2. 收件人：记录本次通知涉及的目标地址，写入 `notification_recipients`。
3. 投递任务：按收件人与 channel 的匹配关系生成实际发送任务，写入 `delivery_tasks`。

后台 dispatcher 会持续扫描 `delivery_tasks`，加载模板、渲染内容、调用具体 adapter，并把每次投递结果写入 `delivery_attempts`。

整体链路如下：

```text
Client / Grafana
  -> HTTP API
  -> Auth middleware
  -> Engine validates request
  -> notifications
  -> notification_recipients
  -> delivery_tasks
  -> Dispatcher polling
  -> render template
  -> channel adapter
  -> external provider
  -> delivery_attempts
  -> notification status refresh
```

## 2. 服务启动流程

入口在 `main.go`。

启动时依次执行：

1. 读取命令行参数。
2. 加载配置，顺序是默认值、YAML 文件、环境变量。
3. 校验配置。
4. 初始化日志。
5. 创建 `server.Server`。
6. 如果指定了配置文件，启动配置热加载。
7. 初始化 HTTP Server、数据库、存储层、adapter、engine、dispatcher、auth manager。
8. 启动 HTTP 服务。

配置加载入口是 `config.LoadGlobalConfig`。provider 配置中的字符串支持环境变量展开，例如：

```yaml
providers:
  feishu-webhook-default:
    type: feishu_webhook
    webhook: "${FEISHU_WEBHOOK_URL}"
    secret: "${FEISHU_WEBHOOK_SECRET}"
```

## 3. 配置模型

通知中心的关键配置分为几类：

| 配置 | 作用 |
| --- | --- |
| `server` | HTTP 监听地址和超时 |
| `database` | PostgreSQL 连接信息 |
| `logging` | 日志级别、格式、debug 开关 |
| `dispatcher` | 后台任务扫描间隔、批大小、租约超时 |
| `auth` | API 鉴权配置 |
| `defaults` | 默认重试次数和退避时间 |
| `channels` | 业务 channel 到 provider 的映射 |
| `providers` | 具体发送 provider 的配置 |

一个 channel 只描述“这类通知是否启用、用哪个 provider 发送”：

```yaml
channels:
  feishu_webhook:
    enabled: true
    provider: feishu-webhook-default
```

provider 描述“如何连接外部发送系统”：

```yaml
providers:
  feishu-webhook-default:
    type: feishu_webhook
    webhook: "${FEISHU_WEBHOOK_URL}"
    secret: "${FEISHU_WEBHOOK_SECRET}"
    msgType: "interactive"
    timeoutSeconds: 10
```

当前代码已经实现的 adapter 包括：

| Provider type | Channel | 说明 |
| --- | --- | --- |
| `volcengine_sms` | `sms` | 火山引擎短信，使用手机号和 provider 模板 |
| `aliyun_sms` | `sms` | 阿里云短信，使用手机号和 provider 模板 |
| `feishu_webhook` | `feishu_webhook` | 飞书自定义机器人 webhook，适合群告警卡片 |
| `feishu_app` | `feishu_app` | 飞书应用消息，可支持加急 |

短信 provider 示例：

```yaml
channels:
  sms:
    enabled: true
    provider: volcengine-sms-default

providers:
  volcengine-sms-default:
    type: volcengine_sms
    endpoint: sms.volcengineapi.com
    region: cn-north-1
    accessKey: "${VOLCENGINE_SMS_ACCESS_KEY}"
    secretKey: "${VOLCENGINE_SMS_SECRET_KEY}"
    smsAccount: "YOUR_SMS_ACCOUNT"
    sign: "SEALOS"
    timeoutSeconds: 10
```

阿里云短信也可以作为 `sms` channel 的 provider：

```yaml
channels:
  sms:
    enabled: true
    provider: aliyun-sms-default

providers:
  aliyun-sms-default:
    type: aliyun_sms
    endpoint: dysmsapi.aliyuncs.com
    accessKeyId: "${ALIYUN_SMS_ACCESS_KEY_ID}"
    accessKeySecret: "${ALIYUN_SMS_ACCESS_KEY_SECRET}"
    signName: "SEALOS"
    timeoutSeconds: 10
```

短信模板的 `templateCode` 是 provider 侧模板 ID，通知请求中的 `params` 会
作为 `TemplateParam` JSON 发送。收件人类型必须是 `phone`，例如：

```json
{
  "channels": {
    "sms": {
      "template": "incident-sms",
      "params": {"severity": "P1", "incident": "database unavailable"}
    }
  },
  "recipients": [{"type": "phone", "value": "+8613800000000"}]
}
```

## 4. 数据库初始化

服务启动时会执行 `db.InitSchema`，自动创建需要的表、索引和触发器。

核心表如下：

| 表 | 作用 |
| --- | --- |
| `templates` | 存储通知模板 |
| `notifications` | 存储一次通知请求 |
| `notification_recipients` | 存储本次通知的收件人地址 |
| `delivery_tasks` | 存储每个实际投递任务 |
| `delivery_attempts` | 存储每次发送尝试的请求、响应和错误 |
| `config_change_audits` | 配置变更审计 |

其中最重要的是 `delivery_tasks`。它是 dispatcher 的工作队列，包含 channel、provider、template、模板参数、状态、重试次数、租约等字段。

## 5. API 鉴权

所有 `/api/v1` 路由都会经过 `authMiddleware`。

支持两种鉴权方式。

第一种是请求头：

```http
X-App-Id: notify-console
X-App-Secret: your-secret
```

第二种是 Bearer token：

```http
Authorization: Bearer notify-console:your-secret
```

Grafana ContactPoint 通常使用第二种，因为 Grafana webhook 配置可以注入 `authorization_credentials`。

鉴权成功后，服务会把 `appId` 写入请求上下文。创建通知时，这个值会保存到 `notifications.sender_app_id`，用于追踪调用方。

## 6. 模板管理流程

模板通过 REST API 管理，存储在 `templates` 表中。

常用接口：

```text
POST   /api/v1/templates
GET    /api/v1/templates
GET    /api/v1/templates/:name
PUT    /api/v1/templates/:name
DELETE /api/v1/templates/:name
```

模板字段包括：

| 字段 | 说明 |
| --- | --- |
| `name` | 模板名，通知请求通过它引用模板 |
| `channel` | 模板所属 channel |
| `description` | 模板描述 |
| `subject` | 标题模板 |
| `body` | 正文模板 |
| `templateCode` | 第三方短信或语音模板编码 |
| `msgType` | 消息类型，如 `text`、`post`、`interactive` |
| `params` | 模板参数说明 |

模板渲染使用 Go `text/template`。请求传入的参数会作为顶层变量使用：

```text
{{ .status }}
{{ .alertname }}
{{ .severity }}
```

如果模板引用了不存在的变量，当前渲染策略是 `missingkey=zero`，不会直接报错，而是渲染为空值。

## 7. 普通通知发送流程

普通发送入口：

```text
POST /api/v1/notifications
```

请求结构：

```json
{
  "idempotencyKey": "incident-001",
  "channels": {
    "feishu_webhook": {
      "template": "feishu-grafana-webhook-alert",
      "params": {
        "status": "firing",
        "alertname": "HighCPU",
        "severity": "critical"
      }
    }
  },
  "recipients": [
    {
      "type": "feishu_user_id",
      "value": "feishu-webhook"
    }
  ]
}
```

### 7.1 请求校验

engine 会校验：

1. `idempotencyKey` 必须存在。
2. `channels` 不能为空。
3. `recipients` 不能为空。
4. 每个 channel 必须存在于配置中。
5. channel 必须启用。
6. 每个 channel 必须指定模板名。
7. 模板必须存在。
8. 模板所属 channel 必须与请求 channel 一致。

### 7.2 幂等处理

`notifications.idempotency_key` 有唯一约束。重复请求使用相同 `idempotencyKey` 时，不会创建新的通知链路，而是返回已有通知。

这可以避免调用方重试导致重复发送。

### 7.3 创建通知与收件人

校验通过后，engine 创建：

1. `notifications`：记录通知 ID、幂等键、调用方、初始状态 `pending`。
2. `notification_recipients`：每个 recipient 一条记录，`params` 中保存 `type` 和 `value`。

示例：

```json
{
  "type": "feishu_user_id",
  "value": "ou_xxx"
}
```

对于 `feishu_webhook`，真正的飞书群地址来自 provider 的 webhook 配置，所以 recipient 可以是固定占位值：

```json
{
  "type": "feishu_user_id",
  "value": "feishu-webhook"
}
```

### 7.4 生成投递任务

engine 会遍历所有 recipient 和所有 channel，按 recipient type 判断是否能投递到该 channel。

当前匹配规则：

| Channel | 支持的 recipient type |
| --- | --- |
| `email` | `email` |
| `sms` | `phone` |
| `voice` | `phone` |
| `inapp` | `user_id` |
| `feishu_app` | `feishu_user_id`、`email` |
| `feishu_webhook` | `feishu_user_id`、`email` |

匹配成功后会生成 `delivery_tasks`：

```text
notification_id
recipient_id
channel
provider
template_name
template_params
status = pending
retry_count = 0
max_retry = defaults.maxRetry
```

这里有一个重要边界：模板参数保存在 `delivery_tasks.template_params` 中，而不是收件人记录中。也就是说，同一个 channel 下所有收件人共享同一份渲染参数。

## 8. Dispatcher 工作流程

dispatcher 是后台任务消费者。

启动后按 `dispatcher.interval` 定时执行批处理：

1. 过期长时间卡在 `processing` 的任务。
2. 获取 `pending` 任务。
3. 获取到达重试时间的 `failed` 任务。
4. 给任务加租约。
5. 并发处理任务。

### 8.1 任务租约

获取任务时使用数据库行锁：

```text
FOR UPDATE SKIP LOCKED
```

这使多个服务实例可以同时运行 dispatcher，而不会重复抢同一批任务。

被抢到的任务会被更新为：

```text
status = processing
lease_owner = 当前 dispatcher 实例 UUID
lease_expire_at = now + dispatcher.leaseTimeout
```

更新成功或失败时，也会检查 `lease_owner`。如果租约已经不属于当前实例，状态更新会被拒绝。

### 8.2 处理单个任务

单个任务处理步骤：

1. 根据 `provider` 获取 adapter。
2. 根据 `recipient_id` 加载收件人。
3. 根据 `template_name` 加载模板。
4. 从收件人中取 `value` 作为投递地址。
5. 使用 `delivery_tasks.template_params` 渲染模板。
6. 构造 `adapter.SendRequest`。
7. 调用 adapter 的 `Send`。
8. 写入 `delivery_attempts`。
9. 更新 `delivery_tasks` 状态。
10. 刷新 `notifications` 总状态。

### 8.3 模板渲染

dispatcher 加载模板后调用 render 包：

```text
render.Template(tpl, renderParams)
```

渲染结果包含：

```text
Subject
Body
```

随后构造发送请求：

```text
RecipientValue
Subject
Body
TemplateCode
Variables
MsgType
Metadata
```

其中 `Variables` 是模板参数转成字符串后的 map，主要给短信、语音类 provider 使用。

## 9. 投递状态和重试

`delivery_tasks.status` 的主要状态：

| 状态 | 含义 |
| --- | --- |
| `pending` | 刚创建，等待 dispatcher 处理 |
| `processing` | 已被某个 dispatcher 实例抢到并正在处理 |
| `success` | 投递成功 |
| `failed` | 投递失败，但可能还有重试机会 |
| `dead` | 已超过最大重试次数，不再重试 |

失败时 dispatcher 会：

1. 写入 `delivery_attempts`，记录错误。
2. 增加 `retry_count`。
3. 写入 `last_error`。
4. 如果还有重试次数，设置 `status=failed` 和 `next_retry_at`。
5. 如果超过最大重试次数，设置 `status=dead`。

重试退避时间来自：

```yaml
defaults:
  maxRetry: 3
  retryBackoffSeconds: [30, 120, 300]
```

如果 `processing` 任务的租约过期，dispatcher 会把它视为失败并进入同样的重试流程。

## 10. 通知总状态刷新

每次任务成功或失败后，dispatcher 会调用 `RefreshStatusFromDeliveryTasks` 刷新通知总状态。

通知状态存储在 `notifications.status`：

| 状态 | 含义 |
| --- | --- |
| `pending` | 已创建但尚未全部处理 |
| `processing` | 有任务正在处理 |
| `success` | 相关投递任务全部成功 |
| `failed` | 有任务最终失败或死亡 |

具体状态由该通知下的 delivery task 状态汇总得出。

## 11. Delivery Attempt 记录

每次 adapter 调用都会产生一条 `delivery_attempts`。

字段包括：

| 字段 | 说明 |
| --- | --- |
| `task_id` | 对应 delivery task |
| `attempt_no` | 第几次尝试 |
| `request_payload` | 发给外部 provider 的请求内容 |
| `response_payload` | 外部 provider 返回内容 |
| `result` | `success` 或 `failed` |
| `error_message` | 失败原因 |
| `started_at` | 开始时间 |
| `finished_at` | 结束时间 |

这张表是排查问题最重要的数据来源。

## 12. Feishu Webhook 投递流程

`feishu_webhook` 用于飞书自定义机器人。

流程：

```text
delivery task
  -> render template
  -> feishu_webhook adapter
  -> build payload
  -> optional signature
  -> POST Feishu bot webhook
  -> parse Feishu response
```

支持的 `msgType`：

| msgType | 行为 |
| --- | --- |
| `text` | 发送纯文本 |
| `post` | body 必须是飞书 post JSON |
| `interactive` | body 可以是完整飞书 card JSON，也可以是普通文本 |

当 `msgType=interactive` 时：

1. 如果 body 是合法 JSON，会按飞书卡片发送。
2. 如果 body 是普通文本，会自动包装成飞书卡片。
3. 卡片标题来自模板 `subject`。
4. 卡片颜色会根据文本中的 `firing`、`resolved` 自动判断。

飞书 webhook 的真实发送地址来自 provider：

```yaml
providers:
  feishu-webhook-default:
    type: feishu_webhook
    webhook: "${FEISHU_WEBHOOK_URL}"
    secret: "${FEISHU_WEBHOOK_SECRET}"
```

因此 `feishu_webhook` channel 的 recipient 只是为了满足通知中心通用模型，可以使用占位值。

## 13. Feishu App 投递流程

`feishu_app` 用于飞书应用消息，适合需要发送给具体用户并触发加急的场景。

它依赖飞书应用：

```yaml
providers:
  feishu-app-urgent:
    type: feishu_app
    appId: "YOUR_APP_ID"
    appSecret: "YOUR_APP_SECRET"
    receiveIdType: "open_id"
    msgType: "text"
    urgentType: "app"
```

recipient 的 `value` 通常是飞书 Open ID：

```json
{
  "type": "feishu_user_id",
  "value": "ou_xxx"
}
```

如果配置了 `urgentType`，adapter 会在发送普通消息后尝试触发对应加急能力。

## 14. Grafana 专用入口

Grafana webhook 的 payload 格式是固定的，和通知中心内部的 `/api/v1/notifications` 请求格式不同。

为避免在 Grafana ContactPoint 里维护复杂 payload 模板，当前提供专用入口：

```text
POST /api/v1/grafana/webhook
```

Grafana 只需要发送默认 webhook JSON。notify 服务端负责把 Grafana 字段转换成普通通知请求。

默认转换目标：

```text
channel = feishu_webhook
template = feishu-grafana-webhook-alert
recipientType = feishu_user_id
recipient = feishu-webhook
```

可以通过 query 参数覆盖：

```text
/api/v1/grafana/webhook?channel=feishu_app&template=feishu-grafana-alert&recipient=ou_xxx
```

Grafana 字段映射：

| 模板参数 | Grafana webhook 字段 |
| --- | --- |
| `status` | `status`，为空时取首个 alert 的 `status` |
| `alertname` | `commonLabels.alertname`，为空时取首个 alert 的 `labels.alertname` |
| `severity` | `commonLabels.severity`，为空时取首个 alert 的 `labels.severity` |
| `summary` | `commonAnnotations.summary`，为空时取首个 alert 的 `annotations.summary` |
| `description` | `commonAnnotations.description`，为空时取首个 alert 的 `annotations.description` |
| `groupKey` | `groupKey` |
| `externalURL` | `externalURL` |
| `fingerprint` | 首个 alert 的 `fingerprint` |
| `startsAt` | 首个 alert 的 `startsAt` |
| `endsAt` | 首个 alert 的 `endsAt` |
| `generatorURL` | 首个 alert 的 `generatorURL` |
| `silenceURL` | 首个 alert 的 `silenceURL` |
| `dashboardURL` | 首个 alert 的 `dashboardURL` |
| `panelURL` | 首个 alert 的 `panelURL` |
| `imageURL` | 首个 alert 的 `imageURL` |
| `labels` | `commonLabels` 格式化文本 |
| `annotations` | `commonAnnotations` 格式化文本 |
| `alerts` | alert 列表格式化文本 |
| `alertsCount` | alert 数量 |
| `truncatedAlerts` | Grafana 截断数量 |

Grafana 专用入口会生成稳定的幂等键：

```text
grafana-<status>-<hash>
```

hash 优先基于首个 alert 的 `fingerprint`，没有 fingerprint 时依次使用 `groupKey`、`receiver`、`alertname`、`title` 等字段。

## 15. Grafana 到飞书群的完整示例链路

典型链路：

```text
Grafana Alert Rule
  -> GrafanaContactPoint: sealos-notify-feishu-webhook
  -> POST /api/v1/grafana/webhook
  -> convert Grafana payload
  -> channel feishu_webhook
  -> template feishu-grafana-webhook-alert
  -> delivery task
  -> dispatcher
  -> Feishu webhook adapter
  -> Feishu custom bot
  -> group card message
```

ContactPoint URL：

```text
http://sealos-notify.notify-system.svc.cluster.local:8080/api/v1/grafana/webhook
```

鉴权：

```http
Authorization: Bearer notify-console:your-secret
```

模板示例：

```json
{
  "name": "feishu-grafana-webhook-alert",
  "channel": "feishu_webhook",
  "subject": "[Grafana {{ .status }}] {{ .alertname }}",
  "body": "**告警状态**: {{ .status }}\n**告警名称**: {{ .alertname }}\n**告警级别**: {{ .severity }}\n**摘要**: {{ .summary }}\n**描述**: {{ .description }}\n**分组**: {{ .groupKey }}\n[查看 Grafana]({{ .externalURL }})",
  "msgType": "interactive"
}
```

## 16. 查询与排障流程

### 16.1 查询通知状态

```text
GET /api/v1/notifications/:id
```

返回通知总状态。

### 16.2 查询投递任务

```text
GET /api/v1/notifications/:id/deliveries
```

查看该通知下的 delivery tasks，包括状态、重试次数和最后错误。

### 16.3 查看日志

常用日志关键字：

```text
Notification created
Processing task
Task completed successfully
Task failed
Failed to render template
Adapter not found
```

### 16.4 常见问题定位

| 现象 | 重点检查 |
| --- | --- |
| API 返回 401 | `Authorization` 或 `X-App-Id` / `X-App-Secret` 是否正确 |
| API 返回模板不存在 | 模板是否已创建，模板 `channel` 是否匹配请求 channel |
| 没有生成任务 | recipient type 是否能匹配 channel |
| 任务一直 pending | dispatcher 是否启用，数据库连接是否正常 |
| 任务一直 processing | lease 是否过期，dispatcher 是否异常退出 |
| 任务 failed | 查看 `delivery_tasks.last_error` 和 `delivery_attempts.error_message` |
| 飞书未收到消息 | webhook URL、签名 secret、飞书机器人安全设置、飞书返回码 |
| Grafana 测试失败 | ContactPoint URL、Bearer token、Service DNS、模板是否存在 |

## 17. 状态流转图

Delivery task 状态：

```text
pending
  -> processing
  -> success

pending
  -> processing
  -> failed
  -> processing
  -> success

pending
  -> processing
  -> failed
  -> processing
  -> dead
```

Notification 状态由 task 汇总：

```text
pending / processing
  -> success
  -> failed
```

## 18. 设计边界

当前通知中心有几个明确边界：

1. 模板由 notify 管理，调用方只传模板名和参数。
2. provider 凭据由服务端配置管理，调用方不直接传外部平台密钥。
3. 通知请求只负责创建任务，真正发送由 dispatcher 异步完成。
4. Grafana 固定 webhook 格式由 `/api/v1/grafana/webhook` 适配，不要求 Grafana 构造 notify 内部 payload。
5. `delivery_attempts` 是外部发送行为的审计记录，应优先用于排障。
