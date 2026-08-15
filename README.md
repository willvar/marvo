<p align="center">
  <img src="frontend/public/favicon.svg" width="88" height="88" alt="Marvo 标志">
</p>

<h1 align="center">Marvo</h1>

<p align="center"><strong>Markdown Revolution</strong></p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-AGPL--3.0--only-1f6feb.svg" alt="AGPL-3.0-only"></a>
</p>

<p align="center">中文 | <a href="README.en.md">English</a></p>

Marvo（码窝）是一个可自行托管、以文件为核心的知识工作区。它把 Markdown 笔记、媒体、响应式编辑界面和可直接操作工作区的智能体整合在同一套系统中，同时为每个用户提供独立的数据空间、设备审批和智能体运行环境。

> Marvo 仍在快速迭代。在使用它存放重要数据前，请先阅读[部署说明](DEPLOY.md)，并准备经过验证的完整备份方案。

## 功能

- **普通文件存储**：每篇笔记都是包含 `index.md`、`meta.json` 和 `assets/` 的普通目录，正文不依赖数据库存储。
- **一套响应式界面**：桌面、平板和手机共用同一套 Vue 界面，支持横屏、竖屏、触摸和深浅色模式。
- **安全编辑**：以 SHA-256 哈希标识内容版本，结合条件写入、实例令牌、本地草稿和三路合并预览，避免覆盖智能体或其他页面刚写入的内容。
- **笔记管理**：支持查看与编辑、标签、全文搜索、媒体上传与转码、回收站和永久删除。
- **工作区智能体**：基于 OpenCode，支持独立会话、附件、图片、实时展示执行过程与文件变化、提供商连接、模型选择、全局提示词和个性化规则。
- **多用户隔离**：平台管理员创建用户；每个用户拥有独立的笔记、媒体、回收站、设置、凭据、会话和智能体容器。
- **设备审批**：新设备须先申请访问，再由空间所属用户在管理后台审批；后台登录支持密码和可选的 TOTP 二次验证，并可随时撤销设备。
- **Android APP**：通用 APK 内置前端产物，支持扫码绑定用户空间、符合 Android 习惯的返回操作、分享、保存图片和应用内更新。

## 架构

```text
浏览器 / Marvo Android APP
             │
             ▼
       Marvo Go API
       + 内嵌 Vue SPA
          │       │
          │       ├─ control/platform.sqlite
          │       └─ users/<userId>/...
          │
          │ HTTP / SSE + Bearer
          ▼
  marvo-runtime（Docker）
          │ Docker API
          ├─ marvo-agent-<userA>（OpenCode）
          ├─ marvo-agent-<userB>（OpenCode）
          └─ marvo-agent-<userC>（OpenCode）
```

Marvo 服务可以作为原生进程或容器运行。Runtime 网关始终位于 Docker 内，并按用户管理独立的 Agent 容器。Agent 容器没有宿主机端口，只挂载对应用户的工作区与 Agent Home；只有权限受限的 Runtime 网关可以访问 Docker Socket。

## 快速开始

### 依赖

- Go（版本见 [`go.mod`](go.mod)）
- Node.js 与 npm
- Docker
- ffmpeg 与 ffprobe（仅宿主机直接运行 Marvo 时需要）

### 启动开发环境

```bash
git clone https://github.com/willvar/marvo.git
cd marvo
cp config.example.yaml config.yaml
npm --prefix frontend ci
make dev
```

首次运行会构建 Agent 与 Runtime 镜像，并在网关健康后启动 Go API 和 Vite。默认地址：

| 服务         | 地址                    |
| ------------ | ----------------------- |
| Web 界面     | `http://localhost:5080` |
| Go API       | `http://127.0.0.1:5090` |
| Runtime 网关 | `http://127.0.0.1:4097` |

打开 `http://localhost:5080/admin/login`，使用 `config.yaml` 中的 `auth.password` 登录平台后台。示例配置的 `marvo` 仅供本机开发；服务监听非回环地址或允许非本机 Origin 时，必须使用至少 12 个字符的非默认密码。

平台管理员创建用户后，用户工作区位于 `/user/{userId}`。平台管理员只能管理用户和旧数据迁移；平台管理登录态不具备读取用户内容的权限。

## 常用命令

| 命令                                 | 用途                                                   |
| ------------------------------------ | ------------------------------------------------------ |
| `make dev`                           | 启动 Go、Vite 与 Runtime 开发环境                      |
| `make preview`                       | 构建前端并启动局域网预览环境                           |
| `make stop-runtime`                  | 停止 Runtime 网关及其管理的 Agent 容器，保留状态数据   |
| `make build`                         | 构建内嵌前端的 `dist/marvo`                            |
| `make test`                          | 运行 Go 与 Android 单元测试                            |
| `npm --prefix frontend run test:e2e` | 运行浏览器端响应式 E2E 测试                            |
| `make test-webkit`                   | 在 Playwright WebKit 环境验证竖屏流程                  |
| `make lint`                          | 运行 Go、前端和 Android 静态检查                       |
| `make audit`                         | 运行全部格式检查、静态分析、死代码检查、测试和构建任务 |

## 数据存储

所有持久状态都位于配置的 `server.state_dir`，容器可以重建，用户数据不会写入容器可写层：

```text
<state_dir>/
  control/
    platform.sqlite
    .session-secret
    .runtime-token
    android/
  users/<userId>/
    app/
    workspace/
      <笔记标题>/
        index.md
        meta.json
        assets/
    agent/home/
```

备份时应把整个 `state_dir` 视为一个整体。数据库、OpenCode 会话、用户凭据、媒体和笔记均需要保持一致。

## 部署

支持两种部署方式：

1. **Marvo 原生进程 + Docker Runtime**：部署结构简单，便于使用 systemd 管理。
2. **Marvo 全容器化**：通过 Compose 运行 Marvo 与 Runtime，并使用 Docker DNS 通信。

两种方式都只需要把 nginx 反向代理到一个 Marvo HTTP 端口，Vue SPA 已嵌入 Go 二进制。完整配置、systemd、Compose、nginx、备份和恢复流程见[部署说明](DEPLOY.md)。

## 项目结构

| 路径               | 内容                                           |
| ------------------ | ---------------------------------------------- |
| `cmd/`             | Marvo 服务与 Runtime 网关入口                  |
| `config/`          | 服务端配置加载与校验                           |
| `internal/`        | 用户、认证、笔记、媒体、智能体代理与运行时实现 |
| `frontend/`        | Vue 3 前端、Playwright E2E 与 Android APP      |
| `docker/opencode/` | 用于创建用户 Agent 容器的 OpenCode 镜像        |
| `docker/runtime/`  | Runtime 网关镜像与本地启动脚本                 |
| `deploy/`          | systemd 与 nginx 示例                          |

## 文档

- [开发说明](DEVELOPMENT.md)
- [部署说明](DEPLOY.md)
- [Android 构建与原生桥接](frontend/android/README.md)
- [Agent Runtime 镜像](docker/opencode/README.md)
- [参与贡献](CONTRIBUTING.md)（[English](CONTRIBUTING.en.md)）

## 参与贡献

欢迎提交问题、文档、测试和代码。涉及用户隔离、认证、文件布局、智能体运行边界或关键交互的改动，请先在 Issue 中说明目标与方案。提交前请阅读[贡献指南](CONTRIBUTING.md)。

## 开源协议

Marvo 根据 [GNU Affero General Public License v3.0 only](LICENSE) 开源。

Copyright (C) 2026 William Varmus。仓库中另有协议或版权声明的第三方代码与资产，继续遵循各自的许可条款。
