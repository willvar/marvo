# Marvo 开发说明

Marvo 只有一套 Vue 响应式界面。平台入口是 `/admin`；每个用户空间使用 `/user/{userId}`，笔记、智能体、回收站和设备管理都位于该前缀下。不存在 `/mobile` 或无用户前缀的业务 API。

## 运行结构

```text
Vite :5080 ──> Marvo Go :5090 ──Bearer──> marvo-runtime :4097
                                              │ Docker API
                                              ├─ marvo-agent-<userA> :4096
                                              └─ marvo-agent-<userB> :4096
```

`marvo-runtime` 始终运行在 Docker 内。开发时只有网关映射到 `127.0.0.1:4097`；每个用户的 Agent 容器都没有宿主机端口，Go API 通过网关访问。多个用户同时使用智能体时，系统会为他们分别运行容器；同一用户的请求复用其容器。容器默认在空闲 30 分钟后停止，并在收到新请求时自动启动，宿主机数据不会被删除。

## 依赖与首次配置

- Go（版本见 `go.mod`）
- Node.js 与 npm
- Docker
- 宿主机运行 Go 时需要 ffmpeg 和 ffprobe；容器化 Marvo 已内置

```bash
cp config.example.yaml config.yaml
npm --prefix frontend ci
make dev
```

`make dev` 会检查 Agent 与 Runtime 网关镜像，并在源码变化时使用 Docker 缓存重新构建；Runtime 网关通过健康检查后，才会启动 Go 和 Vite。默认地址：

| 服务         | 地址                    |
| ------------ | ----------------------- |
| Vite         | `http://localhost:5080` |
| Go API       | `http://127.0.0.1:5090` |
| Runtime 网关 | `http://127.0.0.1:4097` |

结束开发进程后，如果还需要释放 Runtime 端口，请执行 `make stop-runtime`。该命令只会停止并移除无状态网关，同时停止由 Runtime 管理的用户 Agent 容器；用户状态和全部宿主机数据都会保留。

需要在局域网中以接近生产构建的方式验收时，请使用 `make preview`。它会构建前端并启动 Vite Preview；在 `dev` 与 `preview` 之间切换前，应先停止当前进程。正式执行 `make build` 时，同一份前端会嵌入 `dist/marvo`，不再需要 Vite 或独立的静态站点。

Android 通用 APK 位于 `frontend/android`。调试构建使用 `make android-debug`；正式发布、版本递增和签名配置见 `frontend/android/README.md`。构建默认读取 `config.yaml` 中的 `server.public_url`，也可使用 `CONFIG_FILE=/path/to/config.yaml` 选择另一份配置；不再单独接收服务地址。

## 首次进入与权限

1. 平台管理员在 `/admin/login` 登录，并创建用户。
2. 把生成的 `/user/{userId}` 链接和初始密码交给对应用户。
3. 用户从空间登录页进入“管理设备”，使用初始密码登录用户后台，并可按需绑定 TOTP 身份验证器。
4. 新设备在用户空间提交访问申请，再由该用户在管理后台批准。

平台管理员只能创建或停用用户、重置用户凭据和执行旧数据迁移，不能读取用户内容。用户管理 Cookie 与设备 Cookie 相互独立；管理会话也不能代替已批准设备读取笔记。

## 宿主机数据

```text
<state_dir>/
  control/
    platform.sqlite          # 用户与认证控制数据
    .session-secret
    .runtime-token
    android/                 # 平台发布的最新 Android APK 与版本元数据
  users/<userId>/
    .legacy-migration.json   # 可选的旧数据迁移记录
    workspace/               # 笔记、媒体、回收站、主题和隐藏的用户空间设置
    agent/home/              # 提供商与工具凭据、OpenCode 会话及用户全局提示词
```

`.session-secret` 由 Marvo 自动生成并以 `0600` 权限持久化，不属于用户配置，也不会从旧版单用户目录继承。

这些宿主机目录以 bind mount 方式提供给容器，容器本身可以随时重建。用户内容既不在容器可写层中，也不在平台的 SQLite 数据库中。

笔记仍以标题作为目录名和存储身份：

```text
workspace/<笔记标题>/
  index.md
  meta.json
  assets/
```

浏览器保存正文时会携带以 SHA-256 哈希表示的内容版本和笔记实例令牌；发生冲突时返回 409，并由前端显示合并预览。草稿在 IndexedDB 中按笔记实例和草稿 ID 隔离。删除的笔记进入 `.trash`，Marvo 1.0 不会自动清理。媒体上传会先在正文中建立占位；删除占位后，对应的上传或转码任务也会放弃。

## 旧单用户数据迁移

`server.data_dir` 与 `opencode.legacy_home_dir` 只作为旧版迁移源。平台用户页会检测旧笔记、回收站、设置、设备以及 Agent 会话和凭据，并允许将它们显式迁移到指定用户。

- 迁移前停止旧版 `marvo-opencode` 与旧 Go 服务，避免 SQLite 数据库仍在写入。
- 如果目标位置存在同名但内容不同的文件，整个迁移操作会因冲突终止，不会覆盖现有内容。
- 符号链接和特殊文件会被拒绝。
- 旧目录始终保留，不会自动删除；迁移可安全重试。

## 检查

```bash
go test ./...
npm --prefix frontend run check
npm --prefix frontend run test:e2e
make audit
```

`make audit` 包含 Go 格式检查、`go vet`、Staticcheck、不可达代码检查、前端类型检查、lint、死代码检查、Prettier、测试和生产构建。`make test-webkit` 在 Playwright 官方容器中验证 WebKit 竖屏流程；该结果只代表设计兼容，不能替代 iPhone 或 iPad 真机验收。

Marvo 1.0 不渲染 Markdown 数学公式。智能体修改已有文件时仍必须使用带旧文本校验的局部编辑，不能整篇覆盖。
