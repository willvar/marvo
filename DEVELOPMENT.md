# Marvo 开发说明

Marvo 现在只有一套 Vue 响应式用户界面：`frontend/`。`/`、`/note/:title`、`/agent` 和 `/trash` 在同一个应用中适配桌面、平板横屏与手机竖屏；不存在 `/mobile` 应用或路由。

## 依赖

- Go（版本见 `go.mod`）
- Node.js 与 npm
- Docker（运行 OpenCode）
- ffmpeg 与 ffprobe（后端处理 HEIC/HEIF、MOV/HEVC 时需要；开发机可不上传这些格式）
- 已完成 `opencode auth login`，默认认证文件为 `~/.local/share/opencode/auth.json`

## 首次配置

~~~bash
cp config.example.yaml config.yaml
npm --prefix frontend install
~~~

默认地址：

| 服务 | 地址 |
|---|---|
| 前端 Vite | `http://localhost:5080` |
| Go API | `http://127.0.0.1:5090` |
| OpenCode | `http://127.0.0.1:4096` |

`config.yaml` 的 `cors_origins` 必须包含实际前端来源。非回环地址部署时，后端会拒绝默认密码、短 session secret 等不安全配置。

## 启动

一键启动：

~~~bash
make dev
~~~

它会启动 Go、OpenCode Docker 和 Vite。若要分别查看日志，可使用三个终端：

~~~bash
./docker/opencode/start.sh
go run . -c config.yaml
npm --prefix frontend run dev
~~~

在平板、手机或其他局域网设备上进行生产形态验收时，使用：

~~~bash
make preview
~~~

它会先构建生产前端，再启动 Go、OpenCode Docker 和不含 HMR 的 Vite Preview 服务。`make dev` 与 `make preview` 使用相同端口，切换前需要先停止正在运行的实例。

第一次打开前端时需要提交设备申请，再从 `/admin/login` 用管理员密码批准。管理员会话本身不能读取笔记；浏览器仍须是已批准设备。

## 数据模型

~~~text
<data_dir>/
  <笔记标题>/
    index.md
    meta.json
    assets/
  .trash/
  theme.json
~~~

- 标题同时是笔记名称、目录名和 URL 身份；`meta.json` 只保存标签与创建时间，不重复保存另一套名称。修改标题会原子移动整个笔记目录，且不会覆盖同名笔记。
- 浏览器保存正文时携带 SHA-256 revision 与进程内 instance token；冲突返回 409，由前端显示三方合并预览。
- 未保存草稿在 IndexedDB 中按 `instanceToken + draftId` 保存，不会仅凭同名标题自动套用。
- 删除笔记会移动到 `.trash`，首版不会自动过期。
- 图片和视频经过设备鉴权的私有 API。上传先写入正文占位；删除占位会取消并清理上传/转码任务。

## 检查

~~~bash
go test ./...
go vet ./...
npm --prefix frontend run typecheck
npm --prefix frontend run build
npm --prefix frontend run test:e2e
~~~

提交前可直接运行完整静态审计：

~~~bash
make audit
~~~

它会检查 Go 格式、`go vet`、Staticcheck、Go 不可达代码，以及前端类型、Oxlint、Knip、Prettier 和生产构建。工具版本由 Go/npm 命令固定或锁定，无需全局安装 `golangci-lint`。

`test:e2e` 默认运行 Chromium。WebKit 验收应在 Playwright 官方支持的 Linux 环境或官方容器中运行；它用于确认响应式与 Safari/WebKit 设计兼容，不等同于 iPhone/iPad 真机验收。

Arch Linux 的滚动版 ICU/libxml2 ABI 与 Playwright WebKit 的 Ubuntu 构建不兼容，不要创建旧 soname 软链接。项目提供同版本官方容器入口，会复用 npm 锁定的 Playwright 并在隔离测试数据中运行竖屏核心流程：

~~~bash
make test-webkit
~~~

Marvo 1.0 不提供 Markdown 数学公式渲染，`$...$` / `$$...$$` 会作为普通正文保留。智能体对现有文件只能做带旧文本校验的局部编辑，不能整篇覆盖。
