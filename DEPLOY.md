# Marvo 部署说明

## 架构

生产环境由一套响应式前端、Go API 和只监听回环地址的 OpenCode 组成：

~~~text
浏览器 ─HTTPS─> nginx
                  ├─ /api/*  ─> Marvo Go
                  └─ /*      ─> frontend/dist (SPA)

Marvo Go ─> OpenCode 127.0.0.1:4096
         └> data_dir（笔记、私有媒体、回收站）
~~~

没有公开阅读端、公开搜索索引、公开附件路径或 `/mobile` 部署。所有笔记、搜索、媒体、SSE 和 Agent API 都要求已批准设备。

## 构建

~~~bash
npm --prefix frontend ci
npm --prefix frontend run build
make build
~~~

产物：

~~~text
frontend/dist/   # 响应式 SPA
dist/marvo       # Go 服务
~~~

部署 OpenCode：

~~~bash
cd docker/opencode
./start.sh
~~~

`start.sh` 只把 OpenCode 暴露到 `127.0.0.1:4096`，并把 `AGENTS.md` 同步到笔记工作区。模型或 provider 由服务器管理员在 `docker/opencode/opencode.json` 管理。

## 后端配置

以 `config.production.example.yaml` 为起点。必须替换管理员密码和至少 32 字符的随机 session secret：

~~~yaml
server:
  host: 127.0.0.1
  port: 9989
  data_dir: /var/lib/marvo/data
  session_secret: "REPLACE_WITH_A_LONG_RANDOM_SECRET"
  cors_origins:
    - https://marvo.example.com

auth:
  password: "REPLACE_WITH_A_STRONG_ADMIN_PASSWORD"

opencode:
  url: http://127.0.0.1:4096
  global_instructions_file: /var/lib/marvo/opencode-state/home/.config/opencode/AGENTS.md
~~~

如果 Go 与 nginx 不在同一主机，使用受防火墙保护的私网监听地址，并仍只允许明确的前端 origin。

服务器需要安装 `ffmpeg`/`ffprobe`。媒体没有产品层面的体积、时长或分辨率上限，因此应为 `data_dir` 规划充足磁盘；Marvo 会保留安全余量、流式写入并停止无进展的转码。

## nginx 示例

~~~nginx
server {
    listen 443 ssl;
    server_name marvo.example.com;

    root /srv/marvo/frontend;
    index index.html;

    # 产品不设置媒体大小上限。仍由 Marvo 进行磁盘余量和停滞保护。
    client_max_body_size 0;

    location /api/ {
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

    location / {
        try_files $uri $uri/ /index.html;
    }
}
~~~

不要为 `assets/`、搜索或数据目录增加绕过 `/api` 的静态 location，否则会破坏设备访问控制。

## systemd 示例

~~~ini
[Unit]
Description=Marvo
After=network.target docker.service

[Service]
Type=simple
User=marvo
Group=marvo
WorkingDirectory=/opt/marvo
ExecStart=/opt/marvo/marvo -c /etc/marvo/config.yaml
Restart=on-failure
RestartSec=3
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
~~~

上线前至少执行 Go 测试、前端类型检查与构建，并用 Chromium 和 Playwright WebKit 跑核心流程。WebKit 结果只代表设计兼容，不能替代 iPhone 真机验收。
