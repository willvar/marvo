# Marvo Android

这个目录包含通用 Android APP 壳。APK 内置当前 Vue 生产产物，但 API、媒体和 SSE 仍连接构建时指定的 Marvo 服务。首次启动只绑定一个用户 ID；如需绑定其他空间，需要清除 APP 数据或重新安装。

应用 ID 固定为 `cn.willvar.marvo`。服务器 Origin 不写入源码，构建时必须显式提供；域名发生变化时发布新版 APK 即可，用户前台生成的二维码始终只包含 20 位用户 ID。

## 构建调试包

需要 JDK 17 或 21、Android SDK 36，以及已安装的前端依赖。构建脚本会在常见位置寻找兼容 JDK 和 SDK；无法自动找到时可设置 `ANDROID_JAVA_HOME=/path/to/jdk` 与 `ANDROID_HOME=/path/to/sdk`：

```bash
make android-debug SERVER_ORIGIN=https://marvo.example.com
```

输出为 `dist/android/Marvo-debug.apk`。调试包使用 `cn.willvar.marvo.debug`，不能上传到平台发布页。
本机联调时也可传入 `http://localhost` 或 `10.x`、`172.16-31.x`、`192.168.x`、`127.x` 私网地址；只有调试包允许这类明文连接，正式包仍强制使用 HTTPS。

## 构建正式包

1. 修改 `version.properties`。每次发布都必须增加 `VERSION_CODE`；`VERSION_NAME` 是展示给用户的版本号。
2. 将 `signing.example.properties` 复制为被 Git 忽略的 `signing.properties`，填写固定发布密钥。密码也可通过 `storePasswordFile` 和 `keyPasswordFile` 从仓库外的文件读取；密钥和密码不得进入仓库或构建日志。
3. 构建：

```bash
make android-apk SERVER_ORIGIN=https://marvo.example.com
```

输出为 `dist/android/Marvo-<版本号>.apk`。随后由平台管理员在 `/admin/android` 上传；用户可在工作区的“Android APP”入口下载，已安装 APP 也会通过公开版本接口检查更新。

同一应用后续版本必须继续使用同一发布密钥，否则 Android 无法覆盖安装。发布页只接受应用 ID 为 `cn.willvar.marvo` 且版本代码递增的 Marvo APK。

## 代码质量检查

项目根目录的统一门禁会运行 Android Lint、Detekt、ktlint、Kotlin 编译器严格警告和 JVM 单测：

```bash
make lint
make test
```

只检查 Android 可分别运行 `make lint-android` 与 `make test-android`；自动整理 Kotlin 格式使用 `make format-android`。检查构建固定使用回环地址，不依赖真实部署域名。

## 网页与原生能力

APP 只在已绑定用户空间的顶层页面注入 `window.marvo`，普通浏览器和 iframe 不会获得原生能力。网页侧使用：

```ts
await window.marvo.ready()
const environment = await window.marvo.call('env')
const capabilities = await window.marvo.call('capabilities')
```

当前白名单包含 `toast`、`colorScheme`、`statusBar`、`haptic`、`saveImage`、`share`、`backToHome`、`exitApp` 和 `checkUpdate`。`colorScheme` 会同步用户的“跟随系统 / 浅色 / 深色”选择及当前实际颜色，`statusBar` 仅作为旧前端兼容入口保留。参数结构以 `frontend/src/sdk/nativeApp.ts` 为准；Android 会再次校验方法、来源和参数，不提供任意原生命令入口。

硬件返回键会同步调用 `window.marvo.back()`。网页关闭最上层浮层或返回业务父页时返回 `true`；已经位于工作区根页时返回 `false`，Android 随即把任务移到后台。不要使用 WebView 历史栈实现业务返回。
