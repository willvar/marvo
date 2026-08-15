package cn.willvar.marvo

import android.content.ContentValues
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Environment
import android.os.Vibrator
import android.provider.MediaStore
import android.util.Base64
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.core.content.FileProvider
import androidx.core.content.pm.PackageInfoCompat
import androidx.webkit.WebViewCompat
import org.json.JSONObject
import java.io.File
import java.io.FileOutputStream
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors

internal class MarvoSystemServices(
    private val activity: ComponentActivity,
    private val updateManager: AndroidUpdateManager,
    private val applyColorScheme: (NativeColorSchemePreference, Boolean) -> Unit,
) {
    private val worker: ExecutorService = Executors.newSingleThreadExecutor()
    private var destroyed = false

    fun execute(
        call: MarvoBridgeCall,
        reply: (String) -> Unit,
    ) {
        if (destroyed) {
            reply(
                MarvoBridgeContract.failure(
                    call.id,
                    MarvoBridgeErrorCode.BRIDGE_UNAVAILABLE,
                    "Native services are no longer available",
                ),
            )
            return
        }
        try {
            when (call.method) {
                "toast" -> {
                    val body = call.payload!!
                    val duration = if (body.optString("duration") == "long") Toast.LENGTH_LONG else Toast.LENGTH_SHORT
                    Toast.makeText(activity, body.getString("message"), duration).show()
                    reply(MarvoBridgeContract.success(call.id))
                }

                "colorScheme" -> {
                    val body = call.payload!!
                    val preference = NativeColorSchemePreference.fromWire(body.getString("preference"))!!
                    applyColorScheme(preference, body.getString("resolved") == "dark")
                    reply(MarvoBridgeContract.success(call.id))
                }

                "statusBar" -> {
                    val dark = call.payload!!.getString("style") == "dark"
                    applyColorScheme(
                        if (dark) NativeColorSchemePreference.DARK else NativeColorSchemePreference.LIGHT,
                        dark,
                    )
                    reply(MarvoBridgeContract.success(call.id))
                }

                "env" -> {
                    reply(MarvoBridgeContract.success(call.id, environment()))
                }

                "capabilities" -> {
                    reply(MarvoBridgeContract.success(call.id, capabilities()))
                }

                "haptic" -> {
                    val vibrator = activity.getSystemService(Vibrator::class.java)
                    if (vibrator?.hasVibrator() != true) {
                        throw MarvoBridgeException(MarvoBridgeErrorCode.UNSUPPORTED, "Haptic feedback is unavailable")
                    }
                    activity.window.decorView.performHapticFeedback(android.view.HapticFeedbackConstants.CONTEXT_CLICK)
                    reply(MarvoBridgeContract.success(call.id))
                }

                "share" -> {
                    share(call, reply)
                }

                "saveImage" -> {
                    saveImage(call, reply)
                }

                "backToHome" -> {
                    activity.moveTaskToBack(true)
                    reply(MarvoBridgeContract.success(call.id))
                }

                "exitApp" -> {
                    reply(MarvoBridgeContract.success(call.id))
                    activity.finishAndRemoveTask()
                }

                "checkUpdate" -> {
                    updateManager.checkNow { result ->
                        reply(MarvoBridgeContract.success(call.id, result))
                    }
                }

                else -> {
                    throw MarvoBridgeException(
                        MarvoBridgeErrorCode.INVALID_ARGUMENT,
                        "Unknown bridge method: ${call.method}",
                    )
                }
            }
        } catch (error: Exception) {
            reply(MarvoBridgeContract.failure(call.id, error))
        }
    }

    fun destroy() {
        destroyed = true
        worker.shutdownNow()
    }

    private fun environment(): Map<String, Any> {
        val info = activity.packageManager.getPackageInfo(activity.packageName, 0)
        return mapOf(
            "appName" to "Marvo",
            "appVersion" to (info.versionName ?: BuildConfig.VERSION_NAME),
            "buildNumber" to PackageInfoCompat.getLongVersionCode(info).toString(),
            "platform" to "android",
            "systemVersion" to Build.VERSION.RELEASE,
            "webEngineVersion" to (WebViewCompat.getCurrentWebViewPackage(activity)?.versionName ?: "unavailable"),
            "bridgeVersion" to "1",
        )
    }

    private fun capabilities(): Map<String, Any> {
        val manager = activity.packageManager

        fun resolves(intent: Intent) = intent.resolveActivity(manager) != null
        val camera = manager.hasSystemFeature(PackageManager.FEATURE_CAMERA_ANY)
        val microphone = manager.hasSystemFeature(PackageManager.FEATURE_MICROPHONE)
        return mapOf(
            "toast" to true,
            "colorScheme" to true,
            "statusBar" to true,
            "haptic" to (activity.getSystemService(Vibrator::class.java)?.hasVibrator() == true),
            "saveImage" to (Build.VERSION.SDK_INT >= 29),
            "shareText" to resolves(Intent(Intent.ACTION_SEND).setType("text/plain")),
            "shareFile" to resolves(Intent(Intent.ACTION_SEND).setType("application/octet-stream")),
            "backToHome" to true,
            "exitApp" to true,
            "update" to true,
            "filePicker" to resolves(Intent(Intent.ACTION_OPEN_DOCUMENT).setType("*/*")),
            "cameraCapture" to (camera && resolves(Intent(MediaStore.ACTION_IMAGE_CAPTURE))),
            "videoCapture" to (camera && resolves(Intent(MediaStore.ACTION_VIDEO_CAPTURE))),
            "audioCapture" to
                (microphone && resolves(Intent(MediaStore.Audio.Media.RECORD_SOUND_ACTION))),
            "camera" to camera,
            "microphone" to microphone,
            "downloadHttp" to true,
            "downloadData" to true,
            "downloadBlob" to true,
        )
    }

    private fun share(
        call: MarvoBridgeCall,
        reply: (String) -> Unit,
    ) {
        val body = call.payload!!
        val text = body.optString("text").takeIf(String::isNotBlank)
        val file = body.optJSONObject("file")
        if (file == null) {
            val intent =
                Intent(Intent.ACTION_SEND)
                    .setType("text/plain")
                    .putExtra(Intent.EXTRA_TEXT, text)
            launchChooser(intent, "分享")
            reply(MarvoBridgeContract.success(call.id))
            return
        }
        worker.execute {
            try {
                val mime = file.getString("mimeType")
                val bytes = decodeFileData(file.getString("data"), MAX_SHARE_BYTES)
                val directory = File(activity.cacheDir, "bridge-share").apply { mkdirs() }
                directory.listFiles()?.forEach(File::delete)
                val target = File(directory, safeFilename(file.getString("filename")))
                FileOutputStream(target).use { it.write(bytes) }
                val uri = FileProvider.getUriForFile(activity, "${activity.packageName}.files", target)
                activity.runOnUiThread {
                    try {
                        val intent =
                            Intent(Intent.ACTION_SEND)
                                .setType(mime)
                                .putExtra(Intent.EXTRA_STREAM, uri)
                                .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
                        text?.let { intent.putExtra(Intent.EXTRA_TEXT, it) }
                        launchChooser(intent, "分享")
                        reply(MarvoBridgeContract.success(call.id))
                    } catch (error: Exception) {
                        reply(MarvoBridgeContract.failure(call.id, error))
                    }
                }
            } catch (error: Exception) {
                activity.runOnUiThread { reply(MarvoBridgeContract.failure(call.id, error)) }
            }
        }
    }

    private fun saveImage(
        call: MarvoBridgeCall,
        reply: (String) -> Unit,
    ) {
        if (Build.VERSION.SDK_INT < 29) {
            throw MarvoBridgeException(
                MarvoBridgeErrorCode.UNSUPPORTED,
                "Saving images requires Android 10 or newer",
            )
        }
        val body = call.payload!!
        worker.execute {
            try {
                val parsed = parseData(body.getString("data"), MAX_SHARE_BYTES)
                if (!parsed.mimeType.startsWith("image/")) {
                    throw MarvoBridgeException(MarvoBridgeErrorCode.INVALID_ARGUMENT, "saveImage requires image data")
                }
                val values =
                    ContentValues().apply {
                        put(MediaStore.Images.Media.DISPLAY_NAME, safeFilename(body.getString("filename")))
                        put(MediaStore.Images.Media.MIME_TYPE, parsed.mimeType)
                        put(MediaStore.Images.Media.RELATIVE_PATH, "${Environment.DIRECTORY_PICTURES}/Marvo")
                        put(MediaStore.Images.Media.IS_PENDING, 1)
                    }
                val resolver = activity.contentResolver
                val uri =
                    resolver.insert(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, values)
                        ?: throw MarvoBridgeException(MarvoBridgeErrorCode.IO_ERROR, "Cannot create image")
                try {
                    resolver.openOutputStream(uri)?.use { it.write(parsed.bytes) }
                        ?: throw MarvoBridgeException(MarvoBridgeErrorCode.IO_ERROR, "Cannot write image")
                    resolver.update(
                        uri,
                        ContentValues().apply { put(MediaStore.Images.Media.IS_PENDING, 0) },
                        null,
                        null,
                    )
                } catch (error: Exception) {
                    resolver.delete(uri, null, null)
                    throw error
                }
                activity.runOnUiThread {
                    Toast.makeText(activity, "图片已保存", Toast.LENGTH_SHORT).show()
                    reply(MarvoBridgeContract.success(call.id))
                }
            } catch (error: Exception) {
                activity.runOnUiThread { reply(MarvoBridgeContract.failure(call.id, error)) }
            }
        }
    }

    private fun launchChooser(
        intent: Intent,
        title: String,
    ) {
        if (intent.resolveActivity(activity.packageManager) == null) {
            throw MarvoBridgeException(MarvoBridgeErrorCode.UNSUPPORTED, "No compatible application is installed")
        }
        activity.startActivity(Intent.createChooser(intent, title))
    }

    private fun decodeFileData(
        raw: String,
        maxBytes: Int,
    ): ByteArray = parseData(raw, maxBytes).bytes

    private fun parseData(
        raw: String,
        maxBytes: Int,
    ): ParsedData {
        val comma = raw.indexOf(',')
        val dataURL = raw.startsWith("data:", ignoreCase = true) && comma > 5
        val metadata = if (dataURL) raw.substring(5, comma) else "application/octet-stream;base64"
        if (!metadata.split(';').any { it.equals("base64", true) }) {
            throw MarvoBridgeException(MarvoBridgeErrorCode.INVALID_ARGUMENT, "Only base64 data is supported")
        }
        val encoded = if (dataURL) raw.substring(comma + 1) else raw
        val bytes =
            try {
                Base64.decode(encoded, Base64.DEFAULT)
            } catch (_: IllegalArgumentException) {
                throw MarvoBridgeException(MarvoBridgeErrorCode.INVALID_ARGUMENT, "File data is not valid base64")
            }
        if (bytes.isEmpty() || bytes.size > maxBytes) {
            throw MarvoBridgeException(MarvoBridgeErrorCode.INVALID_ARGUMENT, "File is empty or too large")
        }
        return ParsedData(metadata.substringBefore(';').ifBlank { "application/octet-stream" }, bytes)
    }

    private fun safeFilename(raw: String): String =
        raw
            .substringAfterLast('/')
            .substringAfterLast('\\')
            .replace(Regex("[\\u0000-\\u001F<>:\"|?*]"), "_")
            .trim()
            .trim('.')
            .take(160)
            .ifBlank { "marvo-file" }

    private data class ParsedData(
        val mimeType: String,
        val bytes: ByteArray,
    )

    private companion object {
        const val MAX_SHARE_BYTES = 32 * 1024 * 1024
    }
}
