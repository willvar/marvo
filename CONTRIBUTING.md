# 参与贡献

中文 | [English](CONTRIBUTING.en.md)

感谢你愿意改进 Marvo。我们欢迎问题反馈、交互建议、文档、测试和代码贡献。

Marvo 同时涉及用户数据、认证、文件系统、Docker Runtime、浏览器界面和 Android WebView。因此，确保改动可验证，与实现功能本身同样重要。

## 开始之前

- 小范围缺陷修复、测试和文档改进可以直接提交 Pull Request。
- 新功能、数据格式变化、认证变化、依赖替换或明显改变交互的方案，请先创建 Issue，说明目标并确认边界。
- 涉及安全问题时，不要在公开 Issue 中粘贴真实密码、Cookie、Token、提供商密钥、用户内容或完整状态目录。
- 不要把本地配置、生产数据、发布签名材料和生成产物提交进仓库。

## 开发环境

需要安装：

- Go（版本见 [`go.mod`](go.mod)）
- Node.js 与 npm
- Docker
- ffmpeg 与 ffprobe
- 修改 Android 时需要 JDK 17 或 21、Android SDK 36

初始化并启动：

```bash
git clone https://github.com/willvar/marvo.git
cd marvo
cp config.example.yaml config.yaml
npm --prefix frontend ci
make dev
```

更完整的运行结构、端口、初始权限流程和文件布局见[开发说明](DEVELOPMENT.md)。

## 仓库结构

| 路径                       | 责任                                  |
| -------------------------- | ------------------------------------- |
| `cmd/`                     | Marvo 服务与 Runtime 网关入口         |
| `config/`                  | 配置默认值、路径解析和安全校验        |
| `internal/control/`        | 平台用户、密码、TOTP 与控制数据库     |
| `internal/store/`          | 文件笔记、设备、品牌和智能体设置      |
| `internal/handler/`        | HTTP API、认证边界和 OpenCode 代理    |
| `internal/media/`          | 图片与视频上传、状态和转码            |
| `internal/runtimegateway/` | 按用户隔离的 Agent 容器管理与请求代理 |
| `frontend/src/`            | Vue 页面、组件、状态与 SDK            |
| `frontend/e2e/`            | Chromium 横竖屏、WebKit 和多用户 E2E  |
| `frontend/android/`        | Android 壳、JS Bridge 与发布构建      |
| `docker/`                  | Marvo、Runtime 和 OpenCode 镜像       |

## 工作流程

1. Fork 仓库并从最新 `master` 创建主题分支。
2. 每次改动只解决一个明确的问题，避免混入无关的格式化或重构。
3. 为缺陷增加能在修复前失败的回归测试；为新行为增加对应层级的测试。
4. 运行与改动范围匹配的检查。
5. 使用项目提交格式创建单行提交。
6. 推送分支并创建 Pull Request，说明行为变化和验证结果。

示例：

```bash
git switch -c fix/note-conflict
# 修改并验证
git add <files>
git commit -m "FIX: 修复笔记冲突处理"
git push -u origin fix/note-conflict
```

## 提交规范

提交信息只包含一行，不添加正文。格式为：

```text
TYPE: 简明描述
```

使用以下类型：

| 类型       | 用途                           |
| ---------- | ------------------------------ |
| `ADD`      | 新增用户可见的功能或能力       |
| `FIX`      | 修复已有行为中的缺陷           |
| `OPT`      | 优化已有功能的体验、性能或实现 |
| `REFACTOR` | 不改变预期行为的结构调整       |
| `TEST`     | 新增或调整自动化测试           |
| `DOCS`     | 仅修改文档                     |
| `CHORE`    | 版本、构建、依赖和维护工作     |

每个提交只应包含一组相关改动，便于日后追溯。不要使用含糊的 `update`、`changes`，也不要把多个无关目标合在同一个提交中。

## 质量检查

### Go

```bash
gofmt -w <修改的 Go 文件>
go test ./...
make lint-go
```

修改使用 `marvo_web` 构建标签的前端嵌入代码时，还应运行：

```bash
make build
```

### Vue / TypeScript

```bash
npm --prefix frontend run check
npm --prefix frontend run test:e2e
```

只运行相关 E2E 时，可以把文件或项目传给 Playwright：

```bash
npm --prefix frontend run test:e2e -- e2e/core-flow.spec.ts --project=chromium-landscape
```

影响响应式布局的改动至少应检查横屏、手机竖屏、触摸交互以及深浅色模式。需要检查 WebKit 设计兼容性时，运行：

```bash
make test-webkit
```

### Android

```bash
make lint-android
make test-android
```

Kotlin 自动格式化使用 `make format-android`。正式包必须使用存放在仓库外的固定签名配置；贡献者不应提交 `signing.properties`、JKS、Keystore 或 APK。

### 完整检查

准备提交跨层改动或发布相关改动时，运行完整检查：

```bash
make audit
```

它覆盖 Go 格式检查、`go vet`、Staticcheck、前端类型检查与 lint、Android 静态检查、死代码检查、单元测试和生产前端构建。

## 需要保持的产品与架构边界

- **单一响应式前端**：不要新增 `/mobile` 或维护第二套移动端业务界面。
- **用户路由隔离**：用户业务页面与 API 位于 `/user/{userId}`；服务端必须从已验证的用户上下文取得数据目录，不能接受浏览器提交任意文件路径。
- **普通文件存储**：笔记标题就是当前目录名和存储身份；正文、标签和媒体继续使用普通文件。平台的 SQLite 数据库只保存控制数据。
- **条件写入**：正文和元数据更新必须携带内容版本与实例令牌；发生冲突时返回最新状态，由前端决定预览、合并或保留草稿。
- **认证分层**：平台管理会话、用户管理会话和已批准设备凭据用途不同，不能互相替代。
- **用户级 Agent Runtime**：智能体可以操作所属用户的整个工作区，但不能读取其他用户目录、Runtime Token、Docker Socket 或宿主机路径。
- **Runtime 网关约束**：只有网关接触 Docker API；客户端不得指定镜像、挂载、网络或容器参数。
- **统一的界面组件**：优先复用现有 Ark UI、图标依赖和 `frontend/src/components/x/`。不要引入原生 `alert`、`confirm` 或 `prompt`，也不要为相同交互另写一套组件。
- **Android Bridge 仅开放白名单能力**：网页只能使用 `nativeApp.ts` 声明且由原生端再次验证的能力，不能添加任意命令执行入口。
- **Marvo 1.0 范围**：Markdown 数学公式不在当前范围内。

修改这些边界时，请在 Pull Request 中明确说明威胁模型、迁移方式和对应测试。

## Pull Request 要求

Pull Request 应包含：

- 问题或目标，以及为什么需要修改。
- 用户可见行为和主要实现方案。
- 已运行的检查及结果。
- UI 改动的截图或录屏，至少覆盖受影响的屏幕尺寸和颜色模式。
- 对配置、磁盘格式、迁移、部署或 Android 版本的影响。
- 已知限制和未覆盖的真实设备环境。

请控制改动规模，确保评审者可以逐项核对。评审开始后避免不必要的强制推送；根据反馈继续提交即可。

## 问题反馈

提交缺陷时，请提供：

- Marvo 版本或提交哈希。
- 浏览器、系统、设备类型、屏幕尺寸和部署方式。
- 最小复现步骤、预期结果和实际结果。
- 经过脱敏的浏览器控制台输出，以及 Marvo、Runtime 或 Agent 日志。
- 问题是否涉及单个用户、特定笔记、媒体文件、提供商或 Android APP。

任何日志与截图都必须先移除密码、Cookie、Token、API Key、TOTP 密钥、私密笔记内容和真实设备标识。
