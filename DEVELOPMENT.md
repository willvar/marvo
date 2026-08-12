# Marvo 开发说明

Marvo 只有一套 Vue 响应式界面。平台入口是 `/admin`；每个用户空间使用 `/user/{userId}`，其笔记、智能体、回收站和设备管理分别位于该前缀下。不存在 `/mobile` 或无用户前缀的业务 API。

## 运行结构

~~~text
Vite :5080 ──> Marvo Go :5090 ──Bearer──> marvo-runtime :4097
                                              │ Docker API
                                              ├─ marvo-agent-<userA> :4096
                                              └─ marvo-agent-<userB> :4096
~~~

`marvo-runtime` 始终运行在 Docker 内。开发时只有网关映射到 `127.0.0.1:4097`；每用户 Agent 容器没有宿主机端口，Go API 通过网关访问。多个用户同时使用智能体时会同时运行多个容器；同一用户的请求复用其容器，默认空闲 30 分钟后停止、再次请求时自动启动，宿主机数据不会删除。

## 依赖与首次配置

- Go（版本见 `go.mod`）
- Node.js 与 npm
- Docker
- 宿主机运行 Go 时需要 ffmpeg/ffprobe；容器化 Marvo 已内置

~~~bash
cp config.example.yaml config.yaml
npm --prefix frontend install
make dev
~~~

`make dev` 会使用 Docker 缓存重新构建 Agent 与 Runtime 网关镜像，等待网关健康后再启动 Go 和 Vite。默认地址：

| 服务 | 地址 |
|---|---|
| Vite | `http://localhost:5080` |
| Go API | `http://127.0.0.1:5090` |
| Runtime 网关 | `http://127.0.0.1:4097` |

结束开发进程后若需要同时释放 Runtime 端口，执行 `make stop-runtime`。它只停止并移除无状态网关、停止受管用户 Agent 容器，用户容器状态和全部宿主机数据都会保留。

局域网生产形态验收使用 `make preview`。它构建前端并启动 Vite Preview；切换 `dev`/`preview` 前应先停止当前进程。正式 `make build` 会把同一份前端嵌入 `dist/marvo`，无需 Vite 或单独静态站点。

## 首次进入与权限

1. 平台管理员在 `/admin/login` 登录，并创建用户。
2. 把生成的 `/user/{userId}` 链接和初始密码交给对应用户。
3. 用户从空间登录页进入“管理设备”，验证密码并首次绑定 TOTP 身份验证器。
4. 普通设备在用户空间提交申请，由该用户自己的管理员会话批准。

平台管理员只能创建、停用、重置用户凭据和执行旧数据迁移，不能读取用户内容。用户管理 Cookie 与设备 Cookie 分开；管理会话也不能替代已批准设备读取笔记。

## 宿主机数据

~~~text
<state_dir>/
  control/
    platform.sqlite          # 用户与认证控制数据
    .session-secret
    .runtime-token
  users/<userId>/
    app/                     # 设备、智能体设置、迁移记录
    workspace/               # 笔记、媒体、回收站、主题、个性化规则
    agent/home/              # Provider 凭据、OpenCode 会话与用户全局提示词
~~~

这些都是宿主机 bind mount；容器可随时重建。用户内容不在容器可写层，也不在平台 SQLite 中。

笔记仍以标题作为目录名和存储身份：

~~~text
workspace/<笔记标题>/
  index.md
  meta.json
  assets/
~~~

浏览器保存正文时携带 SHA-256 revision 与进程实例 token；冲突返回 409，并由前端显示合并预览。草稿在 IndexedDB 中按实例与草稿 ID 隔离。删除进入 `.trash`，首版不自动过期。媒体上传先建立正文占位，删除占位会放弃后续上传或转码。

## 旧单用户数据迁移

`server.data_dir` 与 `opencode.legacy_home_dir` 只作为旧版迁移源。平台用户页会检测旧笔记、回收站、设置、设备以及 Agent 会话/凭据，并允许显式迁移到一个用户。

- 迁移前停止旧版 `marvo-opencode` 与旧 Go 服务，避免 SQLite 正在写入。
- 同名目标内容不一致时整次操作报冲突，不覆盖。
- 符号链接和特殊文件会被拒绝。
- 旧目录始终保留，不会自动删除；迁移可安全重试。

## 检查

~~~bash
go test ./...
npm --prefix frontend run check
npm --prefix frontend run test:e2e
make audit
~~~

`make audit` 包含 Go 格式、Vet、Staticcheck、不可达代码、前端类型/Lint/死代码/Prettier、测试和生产构建。`make test-webkit` 在 Playwright 官方容器中验证竖屏 WebKit；它代表设计兼容，不替代 iPhone/iPad 真机验收。

Marvo 1.0 不渲染 Markdown 数学公式。智能体修改已有文件时仍必须使用带旧文本校验的局部编辑，不能整篇覆盖。
