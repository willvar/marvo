# Marvo Agent Runtime Image

这个目录只负责构建每用户 Agent Runtime 镜像。镜像不会自行发布宿主机端口，也不再由这里的脚本启动；`marvo-runtime` 网关通过 Docker API 按用户创建和启动容器。

~~~bash
make build-agent
~~~

每个用户容器只挂载自己的两个宿主机目录：

~~~text
<state_dir>/users/<userId>/workspace  -> /workspace
<state_dir>/users/<userId>/agent/home -> /home/marvo
~~~

容器使用只读根文件系统、非 root UID/GID、`cap-drop=ALL`、`no-new-privileges`、PID/内存/CPU 限制，并且没有宿主机端口。Provider 凭据、OpenCode SQLite 会话与日志只写入该用户的 `agent/home`；笔记和媒体只写入该用户的 `workspace`。

`AGENTS.md`、`opencode.json` 和 `marvo-personalization` 固化在镜像内。启动时会把 `/workspace/AGENTS.md` 指向只读系统规则，并在用户 Agent Home 中初始化运行配置。用户设置中的全局提示词位于该用户的 `agent/home/.config/opencode/AGENTS.md`，个性化规则仍由 `marvo-personalization` 管理。

镜像当前包含 OpenCode 1.18.15，以及 `curl`、`wget`、Git、ffmpeg/ffprobe、ImageMagick、HEIF/WebP、ExifTool、Poppler、Python、jq、rg 和压缩工具。
