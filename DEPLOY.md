# Marvo 部署说明

## 架构

```text
物理机 / Docker Host
│
├─ nginx / HTTPS
│    └─ Marvo（原生进程或 App 容器）
│         ├─ control/platform.sqlite
│         ├─ users/<userId>/workspace + agent/home
│         └─ HTTP/SSE ──> marvo-runtime 容器
│                              │ 受限调用 Docker API
│                              ├─ Agent Runtime A 容器
│                              ├─ Agent Runtime B 容器
│                              └─ Agent Runtime C 容器
│
└─ 宿主机 <state_dir>/users/<userId>/...
     └─ 通过 bind mount 分别挂载给 Marvo、网关和对应用户容器
```

Marvo 服务可以作为原生进程运行，也可以容器化；Runtime 网关始终运行在 Docker 内。原生模式通过固定的回环端口访问网关，容器模式则通过 Docker DNS `runtime:4097` 访问。每个用户的 Agent 容器只加入共享的 `marvo-runtime` 网络，不发布宿主机端口，并使用独立、随机且难以猜测的 Basic Auth 凭据；Agent 容器无法访问网关 Bearer Token 和 Docker Socket。

## 方案一：Marvo 原生进程 + Docker Runtime

```bash
npm --prefix frontend ci
make build
make start-runtime
./dist/marvo -c /etc/marvo/config.yaml
```

生产机可使用 `deploy/systemd/` 中的单元和脚本管理 Marvo 原生进程与 Runtime。这种方式下，Marvo 服务不进入容器；Runtime 网关仍通过 Docker 创建相互隔离的用户 Agent 容器。复制 `runtime.env.example` 到
`/etc/marvo/runtime.env`，填写 Marvo 用户、Docker Socket 的 UID/GID 和资源上限后启用：

```bash
systemctl enable --now marvo-runtime.service marvo.service
```

`MARVO_AGENT_IDLE_TIMEOUT` 默认为 1800 秒；只有设为 `0` 才会禁用空闲停止。

`make build` 生成包含 Vue SPA 的单个 `dist/marvo`。配置使用 `config.production.example.yaml`：

```yaml
server:
  host: 127.0.0.1
  port: 9989
  public_url: https://marvo.example.com
  state_dir: /var/lib/marvo
  data_dir: /var/lib/marvo/data
  cors_origins:
    - https://marvo.example.com

auth:
  password: "平台管理员强密码"

opencode:
  legacy_home_dir: /var/lib/marvo/opencode-state/home

runtime:
  url: http://127.0.0.1:4097
  token_file: /var/lib/marvo/control/.runtime-token
```

`server.public_url` 是必填项，用于用户从外通知返回活动页。必须填写可访问 Marvo 的 HTTP(S) Origin，不能包含路径、查询或片段；未配置时 Marvo 会拒绝启动。
正式 APK 构建也使用同一值：`make android-apk CONFIG_FILE=/path/to/production-config.yaml`。

启动 Runtime 时，必须让 `MARVO_STATE_DIR` 与 `server.state_dir` 指向宿主机上的同一个绝对路径。使用 Rootless Docker 时可以另行设置 `MARVO_RUNTIME_DOCKER_SOCKET`；网关仍只映射到回环地址。

## 方案二：Marvo 全容器化

复制并修改 `config.compose.example.yaml`，尤其是公网访问使用的 Origin 和平台密码。会话密钥由 Marvo 在私有状态目录中自动生成并持久化，无需手动配置。然后设置：

```bash
export MARVO_STATE_DIR=/srv/marvo/state
export MARVO_CONFIG_FILE=/srv/marvo/config.yaml
export MARVO_UID=$(id -u marvo)
export MARVO_GID=$(id -g marvo)
export MARVO_DOCKER_SOCKET=/var/run/docker.sock
export MARVO_DOCKER_GID=$(stat -c '%g' "$MARVO_DOCKER_SOCKET")

install -d -m 700 -o "$MARVO_UID" -g "$MARVO_GID" "$MARVO_STATE_DIR"
docker compose --profile images build
docker compose up -d runtime app
```

Compose 只把 Marvo 的 `9989` 端口发布到 `127.0.0.1`。Marvo 通过 Docker DNS 访问 Runtime 网关，用户 Agent 容器不发布任何端口。`MARVO_STATE_DIR` 必须是 Docker Host 可识别的绝对路径；网关会把该路径作为子容器 bind mount 的来源。

Docker Socket 只挂载给 Runtime 网关，不挂载给 Marvo 或 Agent。网关 API 在服务端固定镜像、挂载、网络和资源限制，客户端不能提交容器参数。若使用 Docker Socket Proxy，也必须开放 ping、network/image/container inspect、container list/create/start/stop/remove 这些最低限度的操作。

## nginx

前端由 Go 服务提供，因此所有请求都代理到同一个服务：

仓库内的 `deploy/nginx/marvo.conf.example` 包含 TLS 证书、HTTP/2、HTTP/3、SSE 和流式上传配置。部署时替换 `YOUR_MARVO_DOMAIN`，再安装到 nginx 的站点目录。

```nginx
server {
    listen 443 ssl;
    server_name marvo.example.com;

    client_max_body_size 0;

    location / {
        proxy_pass http://127.0.0.1:9989;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_buffering off;
        proxy_request_buffering off;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }
}
```

不要为笔记、媒体、搜索或 `state_dir` 单独配置绕过 API 的静态文件访问路径。

## 数据、备份和恢复

- 备份整个 `state_dir`，其中包含平台控制数据库，以及每个用户的笔记、设备、设置、提供商凭据和 Agent 会话。
- `control/android` 保存平台管理员发布的 Android APK 与版本元数据，也包含在完整备份中。
- 为保证备份一致性，应先停止 Marvo 与 Runtime；特别是 `platform.sqlite` 和用户的 `opencode.db` 可能使用 WAL。
- 不要单独轮换 `.session-secret`：它用于验证会话并加密 TOTP 密钥。丢失后需要重置所有用户凭据。
- `.runtime-token` 必须在 Marvo 与 Runtime 网关之间保持一致；丢失只会影响网关认证和由它生成的容器密码，不影响用户文件。
- Agent 容器和 Runtime 网关镜像不是备份对象，可以从源码重建。

## 上线检查

```bash
make audit
docker compose config --quiet
curl -fsS http://127.0.0.1:9989/api/health >/dev/null
```

至少验证：平台用户创建、用户 TOTP 登录、两个用户的同名笔记互不可见、设备批准与撤回、媒体转码、多个用户同时使用 Agent、服务重启后的会话恢复，以及 Chromium 横屏与竖屏布局和 Playwright WebKit 核心流程。
