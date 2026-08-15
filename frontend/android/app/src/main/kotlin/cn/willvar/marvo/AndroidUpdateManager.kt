package cn.willvar.marvo

import android.content.Intent
import android.provider.Settings
import android.text.format.Formatter
import android.view.Gravity
import android.view.ViewGroup
import android.widget.LinearLayout
import android.widget.TextView
import androidx.activity.ComponentActivity
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AlertDialog
import androidx.core.content.FileProvider
import androidx.core.net.toUri
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import com.google.android.material.progressindicator.LinearProgressIndicator
import okhttp3.Call
import okhttp3.Callback
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import org.json.JSONObject
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.util.concurrent.atomic.AtomicBoolean

internal class AndroidUpdateManager(
    private val activity: ComponentActivity,
    private val network: OkHttpClient,
) {
    private data class Release(
        val versionCode: Long,
        val versionName: String,
        val required: Boolean,
        val message: String,
    )

    private var checkCall: Call? = null
    private var downloadCall: Call? = null
    private var prompt: AlertDialog? = null
    private var progress: AlertDialog? = null
    private var progressIndicator: LinearProgressIndicator? = null
    private var progressLabel: TextView? = null
    private var pendingPermission: Release? = null
    private var installerOpened = false
    private var destroyed = false
    private val checking = AtomicBoolean(false)

    private val settingsLauncher =
        activity.registerForActivityResult(ActivityResultContracts.StartActivityForResult()) {
            val release = pendingPermission
            pendingPermission = null
            if (release != null && canInstallPackages()) {
                download(release)
            } else if (release?.required == true) {
                showPermissionRequired(release)
            }
        }

    fun checkAtStartup() {
        if (BuildConfig.DEBUG) return
        check(null)
    }

    fun checkNow(callback: (String) -> Unit) {
        check(callback)
    }

    private fun check(callback: ((String) -> Unit)?) {
        if (destroyed || !checking.compareAndSet(false, true)) {
            callback?.invoke("failed")
            return
        }
        val request =
            Request
                .Builder()
                .url(BuildConfig.SERVER_ORIGIN + "/api/app/android/release")
                .header("User-Agent", "MarvoAndroid/${BuildConfig.VERSION_NAME}")
                .get()
                .build()
        checkCall =
            network.newCall(request).also { call ->
                call.enqueue(
                    object : Callback {
                        override fun onFailure(
                            call: Call,
                            error: IOException,
                        ) {
                            checking.set(false)
                            checkCall = null
                            activity.runOnUiThread { callback?.invoke("failed") }
                        }

                        override fun onResponse(
                            call: Call,
                            response: Response,
                        ) {
                            response.use {
                                checking.set(false)
                                checkCall = null
                                if (response.code == 404) {
                                    activity.runOnUiThread { callback?.invoke("noUpdate") }
                                    return
                                }
                                if (!response.isSuccessful) {
                                    activity.runOnUiThread { callback?.invoke("failed") }
                                    return
                                }
                                val raw = response.body.string()
                                val release = parseRelease(raw)
                                if (release == null) {
                                    activity.runOnUiThread { callback?.invoke("failed") }
                                    return
                                }
                                if (release.versionCode <= BuildConfig.VERSION_CODE) {
                                    activity.runOnUiThread { callback?.invoke("noUpdate") }
                                    return
                                }
                                activity.runOnUiThread {
                                    showPrompt(release)
                                    callback?.invoke("available")
                                }
                            }
                        }
                    },
                )
            }
    }

    fun onResume() {
        if (!installerOpened || destroyed) return
        installerOpened = false
        activity.window.decorView.postDelayed(::checkAtStartup, 600)
    }

    fun destroy() {
        destroyed = true
        checkCall?.cancel()
        downloadCall?.cancel()
        checkCall = null
        downloadCall = null
        prompt?.dismiss()
        progress?.dismiss()
        prompt = null
        progress = null
        progressIndicator = null
        progressLabel = null
        pendingPermission = null
    }

    private fun parseRelease(raw: String): Release? =
        runCatching {
            val json = JSONObject(raw)
            val code = json.getLong("version_code")
            val name = json.getString("version_name").trim()
            if (code <= 0 || name.isBlank()) return null
            Release(
                versionCode = code,
                versionName = name,
                required = json.optBoolean("required", false),
                message = json.optString("message").trim(),
            )
        }.getOrNull()

    private fun showPrompt(release: Release) {
        if (destroyed || activity.isFinishing || prompt?.isShowing == true || progress?.isShowing == true) return
        val message =
            buildString {
                append("发现新版本 ")
                append(release.versionName)
                if (release.message.isNotBlank()) {
                    append("\n\n")
                    append(release.message)
                }
            }
        val builder =
            MaterialAlertDialogBuilder(activity)
                .setTitle(if (release.required) "需要更新 Marvo" else "Marvo 有新版本")
                .setMessage(message)
                .setPositiveButton("立即更新") { _, _ -> beginUpdate(release) }
        if (release.required) {
            builder.setNegativeButton("退出") { _, _ -> activity.finishAndRemoveTask() }
        } else {
            builder.setNegativeButton("稍后", null)
        }
        prompt =
            builder.create().apply {
                setCanceledOnTouchOutside(!release.required)
                setCancelable(!release.required)
                setOnDismissListener { prompt = null }
                show()
            }
    }

    private fun beginUpdate(release: Release) {
        if (!canInstallPackages()) {
            pendingPermission = release
            runCatching {
                settingsLauncher.launch(
                    Intent(
                        Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES,
                        "package:${activity.packageName}".toUri(),
                    ),
                )
            }.onFailure {
                pendingPermission = null
                updateFailed(release)
            }
            return
        }
        download(release)
    }

    private fun download(release: Release) {
        if (destroyed || downloadCall?.isExecuted() == true) return
        showProgress(release.required)
        val request =
            Request
                .Builder()
                .url(BuildConfig.SERVER_ORIGIN + "/api/app/android/apk")
                .header("User-Agent", "MarvoAndroid/${BuildConfig.VERSION_NAME}")
                .get()
                .build()
        downloadCall =
            network.newCall(request).also { call ->
                call.enqueue(
                    object : Callback {
                        override fun onFailure(
                            call: Call,
                            error: IOException,
                        ) {
                            if (call.isCanceled()) return
                            activity.runOnUiThread {
                                if (downloadCall === call) updateFailed(release)
                            }
                        }

                        override fun onResponse(
                            call: Call,
                            response: Response,
                        ) {
                            response.use {
                                if (!response.isSuccessful) {
                                    activity.runOnUiThread {
                                        if (downloadCall === call) updateFailed(release)
                                    }
                                    return
                                }
                                val body = response.body
                                val contentLength = body.contentLength()
                                if (contentLength > MAX_APK_BYTES) {
                                    activity.runOnUiThread {
                                        if (downloadCall === call) updateFailed(release)
                                    }
                                    return
                                }
                                val totalBytes = contentLength.takeIf { it > 0 }
                                val directory = File(activity.cacheDir, "update").apply { mkdirs() }
                                val target = File(directory, "Marvo-${release.versionCode}.apk")
                                directory.listFiles()?.filter { it != target }?.forEach(File::delete)
                                val copied =
                                    runCatching {
                                        updateDownloadProgress(call, 0, totalBytes)
                                        body.byteStream().use { input ->
                                            FileOutputStream(target).use { output ->
                                                val buffer = ByteArray(64 * 1024)
                                                var total = 0L
                                                var lastPercentage = -1
                                                var lastUnknownProgress = 0L
                                                while (true) {
                                                    val read = input.read(buffer)
                                                    if (read < 0) break
                                                    total += read
                                                    if (total > MAX_APK_BYTES) error("APK too large")
                                                    output.write(buffer, 0, read)
                                                    if (totalBytes != null) {
                                                        val percentage = ((total * 100) / totalBytes).toInt().coerceIn(0, 100)
                                                        if (percentage != lastPercentage) {
                                                            lastPercentage = percentage
                                                            updateDownloadProgress(call, total, totalBytes)
                                                        }
                                                    } else if (total - lastUnknownProgress >= UNKNOWN_PROGRESS_STEP_BYTES) {
                                                        lastUnknownProgress = total
                                                        updateDownloadProgress(call, total, null)
                                                    }
                                                }
                                                output.fd.sync()
                                                total
                                            }
                                        }
                                    }.getOrNull()
                                if (call.isCanceled() || downloadCall !== call) {
                                    target.delete()
                                    return
                                }
                                if (copied == null || copied <= 0) {
                                    target.delete()
                                    activity.runOnUiThread {
                                        if (downloadCall === call) updateFailed(release)
                                    }
                                    return
                                }
                                activity.runOnUiThread {
                                    if (downloadCall !== call || call.isCanceled()) return@runOnUiThread
                                    progress?.dismiss()
                                    progress = null
                                    progressIndicator = null
                                    progressLabel = null
                                    downloadCall = null
                                    install(target, release)
                                }
                            }
                        }
                    },
                )
            }
    }

    private fun showProgress(required: Boolean) {
        val indicator =
            LinearProgressIndicator(activity).apply {
                isIndeterminate = false
                max = 100
                progress = 0
                contentDescription = "下载进度 0%"
            }
        val label =
            TextView(activity).apply {
                text = "正在连接服务器…"
                gravity = Gravity.CENTER_HORIZONTAL
            }
        val spacing = (24 * activity.resources.displayMetrics.density).toInt()
        val labelSpacing = (12 * activity.resources.displayMetrics.density).toInt()
        val wrapper =
            LinearLayout(activity).apply {
                orientation = LinearLayout.VERTICAL
                setPadding(spacing, spacing, spacing, spacing)
                addView(
                    indicator,
                    LinearLayout.LayoutParams(
                        ViewGroup.LayoutParams.MATCH_PARENT,
                        ViewGroup.LayoutParams.WRAP_CONTENT,
                    ),
                )
                addView(
                    label,
                    LinearLayout
                        .LayoutParams(
                            ViewGroup.LayoutParams.MATCH_PARENT,
                            ViewGroup.LayoutParams.WRAP_CONTENT,
                        ).apply { topMargin = labelSpacing },
                )
            }
        progressIndicator = indicator
        progressLabel = label
        progress =
            MaterialAlertDialogBuilder(activity)
                .setTitle("正在下载更新…")
                .setView(wrapper)
                .create()
                .apply {
                    setCancelable(!required)
                    setCanceledOnTouchOutside(false)
                    setOnCancelListener {
                        downloadCall?.cancel()
                        downloadCall = null
                    }
                    setOnDismissListener {
                        progress = null
                        progressIndicator = null
                        progressLabel = null
                    }
                    show()
                }
    }

    private fun updateDownloadProgress(
        call: Call,
        downloadedBytes: Long,
        totalBytes: Long?,
    ) {
        activity.runOnUiThread {
            if (destroyed || downloadCall !== call || call.isCanceled()) return@runOnUiThread
            val indicator = progressIndicator ?: return@runOnUiThread
            val label = progressLabel ?: return@runOnUiThread
            val downloaded = Formatter.formatFileSize(activity, downloadedBytes)
            if (totalBytes != null) {
                val percentage = ((downloadedBytes * 100) / totalBytes).toInt().coerceIn(0, 100)
                indicator.isIndeterminate = false
                indicator.setProgressCompat(percentage, true)
                indicator.contentDescription = "下载进度 $percentage%"
                label.text = "$downloaded / ${Formatter.formatFileSize(activity, totalBytes)} · $percentage%"
            } else {
                indicator.isIndeterminate = true
                indicator.contentDescription = "正在下载更新"
                label.text = "已下载 $downloaded"
            }
        }
    }

    private fun install(
        file: File,
        release: Release,
    ) {
        val uri = FileProvider.getUriForFile(activity, "${activity.packageName}.files", file)
        val intent =
            Intent(Intent.ACTION_VIEW)
                .setDataAndType(uri, "application/vnd.android.package-archive")
                .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        if (intent.resolveActivity(activity.packageManager) == null) {
            updateFailed(release)
            return
        }
        installerOpened = true
        runCatching { activity.startActivity(intent) }.onFailure {
            installerOpened = false
            updateFailed(release)
        }
    }

    private fun updateFailed(release: Release?) {
        progress?.dismiss()
        progress = null
        progressIndicator = null
        progressLabel = null
        downloadCall = null
        if (destroyed || activity.isFinishing) return
        val builder =
            MaterialAlertDialogBuilder(activity)
                .setTitle("更新失败")
                .setMessage("无法下载或打开安装包，请检查网络和安装权限后重试。")
        if (release != null) {
            builder.setPositiveButton("重试") { _, _ -> beginUpdate(release) }
            if (release.required) {
                builder.setNegativeButton("退出") { _, _ -> activity.finishAndRemoveTask() }
            } else {
                builder.setNegativeButton("稍后", null)
            }
        } else {
            builder.setPositiveButton("知道了", null)
        }
        builder.setCancelable(release?.required != true).show()
    }

    private fun showPermissionRequired(release: Release) {
        MaterialAlertDialogBuilder(activity)
            .setTitle("需要安装权限")
            .setMessage("请允许 Marvo 安装更新，才能继续使用当前版本。")
            .setPositiveButton("前往设置") { _, _ -> beginUpdate(release) }
            .setNegativeButton("退出") { _, _ -> activity.finishAndRemoveTask() }
            .setCancelable(false)
            .show()
    }

    private fun canInstallPackages(): Boolean = activity.packageManager.canRequestPackageInstalls()

    private companion object {
        const val MAX_APK_BYTES = 256L shl 20
        const val UNKNOWN_PROGRESS_STEP_BYTES = 512L shl 10
    }
}
