# Marvo 部署说明

## 架构

~~~text
物理机 / Docker Host
│
├─ nginx / HTTPS
│    └─ Marvo（原生进程 或 app 容器）
│         ├─ control/platform.sqlite
│         ├─ users/<userId>/app + workspace
│         └─ HTTP/SSE ──> marvo-runtime 容器
│                              │ 受限调用 Docker API
│                              ├─ Agent Runtime A 容器
│                              ├─ Agent Runtime B 容器
│                              └─ Agent Runtime C 容器
│
└─ 宿主机 <state_dir>/users/<userId>/...
     └─ 分别 bind mount 给 Marvo、网关和对应用户容器
~~~

外层 Marvo 可以原生运行，也可以容器化；Runtime 网关始终在 Docker 内。原生模式通过固定回环端口访问网关，容器模式通过 Docker DNS `runtime:4097` 访问。每用户 Agent 仅加入共享的 `marvo-runtime` 网络、没有宿主机端口，并使用各自不可预测的 Basic 凭据；Agent 容器拿不到网关 Bearer token 和 Docker socket。

## 方案一：原生 Marvo + Docker Runtime

~~~bash
npm --prefix frontend ci
make build
make start-runtime
./dist/marvo -c /etc/marvo/config.yaml
~~~

`make build` 生成包含 Vue SPA 的单个 `dist/marvo`。配置使用 `config.production.example.yaml`：

~~~yaml
server:
  host: 127.0.0.1
  port: 9989
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
~~~

启动 Runtime 时让 `MARVO_STATE_DIR` 与 `server.state_dir` 指向同一绝对宿主机目录。Rootless Docker 可另外设置 `MARVO_RUNTIME_DOCKER_SOCKET`；网关仍只映射回环端口。

## 方案二：Marvo 全容器化

复制并修改 `config.compose.example.yaml`，尤其是公网 Origin 和平台密码。会话密钥由 Marvo 在私有状态目录自动生成并持久化，无需配置。然后设置：

~~~bash
export MARVO_STATE_DIR=/srv/marvo/state
export MARVO_CONFIG_FILE=/srv/marvo/config.yaml
export MARVO_UID=$(id -u marvo)
export MARVO_GID=$(id -g marvo)
export MARVO_DOCKER_SOCKET=/var/run/docker.sock
export MARVO_DOCKER_GID=$(stat -c '%g' "$MARVO_DOCKER_SOCKET")

install -d -m 700 -o "$MARVO_UID" -g "$MARVO_GID" "$MARVO_STATE_DIR"
docker compose --profile images build
docker compose up -d runtime app
~~~

Compose 只把 Marvo 的 `9989` 发布到 `127.0.0.1`。Runtime 网关通过 Docker DNS 访问，用户 Agent 容器完全不发布端口。`MARVO_STATE_DIR` 必须是 Docker Host 可识别的绝对路径；网关将同一路径作为子容器 bind mount 的来源。

Docker socket 只挂给 Runtime 网关，不挂给 Marvo 或 Agent。网关 API 只允许固定镜像、固定挂载、固定网络和资源限制，客户端不能提交容器参数。若使用 Docker Socket Proxy，也必须允许 ping、network/image/container inspect、container list/create/start/stop/remove 这些最小操作。

## nginx

前端已经由 Go 提供，因此所有请求代理到同一个服务：

~~~nginx
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
~~~

不要给笔记、媒体、搜索或 `state_dir` 配置绕过 API 的静态路径。

## 数据、备份和恢复

- 备份整个 `state_dir`，其中包含平台控制库、每用户笔记、设备、设置、Provider 凭据和 Agent 会话。
- 一致性备份前停止 Marvo 与 Runtime；特别是 `platform.sqlite` 和用户 `opencode.db` 可能使用 WAL。
- 不要单独旋转 `.session-secret`：它用于验证会话并加密 TOTP secret。丢失后需要重置所有用户凭据。
- `.runtime-token` 必须与 Marvo 和 Runtime 网关保持一致；丢失只影响网关认证和派生的容器密码，不影响用户文件。
- Agent 容器和 Runtime 网关镜像不是备份对象，可以从源码重建。

## 上线检查

~~~bash
make audit
docker compose config --quiet
curl -fsS http://127.0.0.1:9989/api/health >/dev/null
~~~

至少验证：平台用户创建、用户 TOTP 登录、两用户同名笔记互不可见、设备批准与撤回、媒体转码、Agent 并发、服务重启后的会话恢复，以及 Chromium 横屏/竖屏和 Playwright WebKit 核心流程。
