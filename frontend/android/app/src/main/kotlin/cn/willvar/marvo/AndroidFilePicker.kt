package cn.willvar.marvo

import android.Manifest
import android.app.Activity
import android.content.ClipData
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.provider.MediaStore
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import androidx.activity.ComponentActivity
import androidx.activity.result.ActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.core.content.ContextCompat
import androidx.core.content.FileProvider
import java.io.File

internal class AndroidFilePicker(
    private val activity: ComponentActivity,
) {
    private val captureDirectory =
        File(activity.cacheDir, "capture").apply {
            mkdirs()
            listFiles()?.filter(File::isFile)?.forEach(File::delete)
        }
    private val completedCaptures = linkedSetOf<File>()
    private var callback: ValueCallback<Array<Uri>>? = null
    private var captureUri: Uri? = null
    private var captureFile: File? = null
    private var pendingIntent: Intent? = null
    private var permissionFallback: Intent? = null

    private val resultLauncher =
        activity.registerForActivityResult(ActivityResultContracts.StartActivityForResult(), ::finish)
    private val permissionLauncher =
        activity.registerForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
            val intent = if (granted) pendingIntent else permissionFallback
            pendingIntent = null
            permissionFallback = null
            if (!granted) clearCapture()
            if (intent != null && callback != null) {
                runCatching { resultLauncher.launch(intent) }.onFailure { cancel() }
            } else {
                cancel()
            }
        }

    fun show(
        params: WebChromeClient.FileChooserParams,
        result: ValueCallback<Array<Uri>>,
    ): Boolean {
        cancel()
        callback = result
        val document = documentIntent(params)
        val capture = runCatching { captureIntent(params.acceptTypes) }.getOrNull()
        val directCapture = params.isCaptureEnabled && capture != null
        val launch =
            when {
                directCapture -> capture
                capture != null ->
                    Intent.createChooser(document, chooserTitle(capture)).apply {
                        putExtra(Intent.EXTRA_INITIAL_INTENTS, arrayOf(capture))
                    }
                else -> document
            }
        val permission = capturePermission(capture)
        if (permission != null && ContextCompat.checkSelfPermission(activity, permission) != PackageManager.PERMISSION_GRANTED) {
            pendingIntent = launch
            permissionFallback = if (directCapture) null else document
            permissionLauncher.launch(permission)
        } else {
            runCatching { resultLauncher.launch(launch) }.onFailure { cancel() }
        }
        return true
    }

    fun cancel() {
        callback?.onReceiveValue(null)
        callback = null
        pendingIntent = null
        permissionFallback = null
        clearCapture()
    }

    fun dispose() {
        cancel()
        completedCaptures.forEach(File::delete)
        completedCaptures.clear()
    }

    private fun capturePermission(intent: Intent?): String? =
        when (intent?.action) {
            MediaStore.ACTION_IMAGE_CAPTURE, MediaStore.ACTION_VIDEO_CAPTURE -> Manifest.permission.CAMERA
            MediaStore.Audio.Media.RECORD_SOUND_ACTION -> Manifest.permission.RECORD_AUDIO
            else -> null
        }

    private fun chooserTitle(intent: Intent): String =
        when (intent.action) {
            MediaStore.ACTION_IMAGE_CAPTURE -> "选择图片"
            MediaStore.ACTION_VIDEO_CAPTURE -> "选择视频"
            MediaStore.Audio.Media.RECORD_SOUND_ACTION -> "选择音频"
            else -> "选择文件"
        }

    private fun captureIntent(types: Array<String>): Intent? {
        val normalized = types.filter(String::isNotBlank).map(String::lowercase)
        val type = normalized.firstOrNull().orEmpty()
        val action =
            when {
                normalized.any { it.startsWith("image/") } || type.startsWith("image/") ->
                    MediaStore.ACTION_IMAGE_CAPTURE
                normalized.any { it.startsWith("video/") } || type.startsWith("video/") ->
                    MediaStore.ACTION_VIDEO_CAPTURE
                normalized.any { it.startsWith("audio/") } || type.startsWith("audio/") ->
                    MediaStore.Audio.Media.RECORD_SOUND_ACTION
                else -> return null
            }
        val manager = activity.packageManager
        val hardware =
            when (action) {
                MediaStore.ACTION_IMAGE_CAPTURE, MediaStore.ACTION_VIDEO_CAPTURE ->
                    manager.hasSystemFeature(PackageManager.FEATURE_CAMERA_ANY)
                else -> manager.hasSystemFeature(PackageManager.FEATURE_MICROPHONE)
            }
        if (!hardware) return null
        val intent = Intent(action)
        if (intent.resolveActivity(manager) == null) return null
        if (action != MediaStore.Audio.Media.RECORD_SOUND_ACTION) {
            val extension = if (action == MediaStore.ACTION_IMAGE_CAPTURE) ".jpg" else ".mp4"
            val file = File.createTempFile("marvo-", extension, captureDirectory)
            val uri = FileProvider.getUriForFile(activity, "${activity.packageName}.files", file)
            captureFile = file
            captureUri = uri
            intent.putExtra(MediaStore.EXTRA_OUTPUT, uri)
            intent.clipData = ClipData.newRawUri("Marvo capture", uri)
            intent.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_WRITE_URI_PERMISSION)
        }
        return intent
    }

    private fun documentIntent(params: WebChromeClient.FileChooserParams): Intent {
        val wildcard = "*/*"
        val types = params.acceptTypes.filter(String::isNotBlank).ifEmpty { listOf(wildcard) }
        return Intent(Intent.ACTION_OPEN_DOCUMENT)
            .addCategory(Intent.CATEGORY_OPENABLE)
            .setType(types.singleOrNull() ?: wildcard)
            .putExtra(
                Intent.EXTRA_ALLOW_MULTIPLE,
                params.mode == WebChromeClient.FileChooserParams.MODE_OPEN_MULTIPLE,
            )
            .apply {
                if (types.size > 1) putExtra(Intent.EXTRA_MIME_TYPES, types.toTypedArray())
            }
    }

    private fun finish(result: ActivityResult) {
        val target = callback ?: return
        callback = null
        pendingIntent = null
        permissionFallback = null
        if (result.resultCode != Activity.RESULT_OK) {
            target.onReceiveValue(null)
            clearCapture()
            return
        }
        val data = result.data
        val values =
            when {
                data?.clipData != null ->
                    Array(data.clipData!!.itemCount) { index -> data.clipData!!.getItemAt(index).uri }
                data?.data != null -> arrayOf(data.data!!)
                captureUri != null -> arrayOf(captureUri!!)
                else -> WebChromeClient.FileChooserParams.parseResult(result.resultCode, data)
            }
        target.onReceiveValue(values)
        if (data?.data != null || data?.clipData != null) {
            clearCapture()
        } else {
            val file = captureFile
            captureFile = null
            captureUri = null
            if (file != null) completedCaptures += file
        }
    }

    private fun clearCapture() {
        captureFile?.delete()
        captureFile = null
        captureUri = null
    }
}
