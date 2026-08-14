# sealos-notify 中文部署说明

完整项目说明请参阅 [README.md](README.md)。API、配置和完整 Kubernetes 部署参考分别见 [API Reference](docs/api-reference.md)、[Configuration Reference](docs/configuration.md) 和 [Kubernetes Deployment](docs/deployment.md)。本文保留中文部署摘要和 Secret 管理流程。

## Kubernetes 部署

### Helm 或集群镜像部署

Helm Chart 不会自动创建业务凭据 Secret。安装 Chart 或运行集群镜像入口脚本前，需要先在目标 namespace 创建这些 Secret。仓库提供了示例文件和安装脚本：

```bash
cp deploy/secrets.example.yaml deploy/secrets.yaml
# 填写已启用渠道的凭据，不要提交 deploy/secrets.yaml
$EDITOR deploy/secrets.yaml
./deploy/install.sh
```

安装脚本会创建 namespace、应用 `deploy/secrets.yaml`、渲染 Chart 检查 Secret 名称和 key，确认通过后执行 `helm upgrade --install`。如需使用其他 namespace，可设置 `RELEASE_NAMESPACE`。集群镜像使用相同的 Secret 名称，因此运行集群镜像前也必须先在 release namespace 准备好 Secret。

| Secret | Key | 使用条件 |
| --- | --- | --- |
| `sealos-notify-pg-conn-credential` | `password` | 始终需要，通常由 PostgreSQL Operator 创建。 |
| `sealos-notify-api-auth` | `apps.yaml` | 启用 API 鉴权时需要。 |
| `sealos-notify-feishu-webhook` | `webhook-url`、`webhook-secret` | 启用飞书 Webhook 时需要。 |
| `sealos-notify-smtp` | `username`、`password` | 启用邮件通知时需要。 |
| `sealos-notify-feishu` | `app-id`、`app-secret` | 启用飞书应用通知时需要。 |

不要把凭据值写入 Helm `values.yaml`。生产环境可以使用 Sealed Secrets 或 External Secrets 替代本地 Secret 文件，但 Secret 名称和 key 需要保持一致。

### 原生 Kubernetes Manifest

如果不使用 Helm，可以继续使用 `deploy/kubernetes/` 下的原生 Manifest。请先创建其中引用的 Secret，再执行：

```bash
kubectl apply -f deploy/kubernetes/
```
