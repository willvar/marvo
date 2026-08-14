# Marvo Android

这个目录包含通用 Android APP 壳。APK 内置当前 Vue 生产产物，但 API、媒体和 SSE 仍连接构建时指定的 Marvo 服务。首次启动只绑定一个用户 ID；如需绑定其他空间，需要清除 APP 数据或重新安装。

应用 ID 固定为 `cn.willvar.marvo`。服务器 Origin 不写入源码，构建时必须显式提供；域名发生变化时发布新版 APK 即可，用户前台生成的二维码始终只包含 20 位用户 ID。

## 构建调试包

需要 JDK 17 或 21、Android SDK 36，以及已安装的前端依赖。构建脚本会在常见位置寻找兼容 JDK 和 SDK；无法自动找到时可设置 `ANDROID_JAVA_HOME=/path/to/jdk` 与 `ANDROID_HOME=/path/to/sdk`：

```bash
make android-debug SERVER_ORIGIN=https://marvo.example.com
```

输出为 `dist/android/Marvo-debug.apk`。调试包使用 `cn.willvar.marvo.debug`，不能上传到平台发布页。

## 构建正式包

1. 修改 `version.properties`。每次发布都必须增加 `VERSION_CODE`；`VERSION_NAME` 是展示给用户的版本号。
2. 将 `signing.example.properties` 复制为被 Git 忽略的 `signing.properties`，填写固定发布密钥。密码也可通过 `storePasswordFile` 和 `keyPasswordFile` 从仓库外的文件读取；密钥和密码不得进入仓库或构建日志。
3. 构建：

```bash
make android-apk SERVER_ORIGIN=https://marvo.example.com
```

输出为 `dist/android/Marvo-<版本号>.apk`。随后由平台管理员在 `/admin/android` 上传；用户可在工作区的“Android APP”入口下载，已安装 APP 也会通过公开版本接口检查更新。

同一应用后续版本必须继续使用同一发布密钥，否则 Android 无法覆盖安装。发布页只接受应用 ID 为 `cn.willvar.marvo` 且版本代码递增的 Marvo APK。
