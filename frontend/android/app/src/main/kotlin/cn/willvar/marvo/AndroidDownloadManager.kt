package cn.willvar.marvo

import android.Manifest
import android.content.ContentValues
import android.os.Build
import android.os.Environment
import android.provider.MediaStore
import android.util.Base64
import android.webkit.CookieManager
import android.webkit.MimeTypeMap
import android.webkit.URLUtil
import android.webkit.WebView
import android.widget.Toast
import androidx.activity.ComponentActivity
import okhttp3.Call
import okhttp3.Callback
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import org.json.JSONObject
import org.json.JSONTokener
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicInteger

internal class AndroidDownloadManager(
    private val activity: ComponentActivity,
    network: OkHttpClient,
    private val permissions: AndroidPermissionManager,
) {
    private val client =
        network.newBuilder()
            .followRedirects(false)
            .followSslRedirects(false)
            .build()
    private val worker: ExecutorService = Executors.newSingleThreadExecutor()
    private val calls = ConcurrentHashMap.newKeySet<Call>()
    private val blobs = ConcurrentHashMap<String, BlobSession>()
    private val counter = AtomicInteger()
    private val token = UUID.randomUUID().toString()
    @Volatile private var destroyed = false

    fun download(
        view: WebView,
        url: String,
        userAgent: String?,
        contentDisposition: String?,
        mimeType: String?,
        trustedOrigin: String,
    ) {
        if (destroyed || view.url?.let(::originOf) != trustedOrigin) return
        resolveFilename(view, url, contentDisposition, mimeType) { filename ->
            if (destroyed) return@resolveFilename
            when {
                url.startsWith("blob:", ignoreCase = true) ->
                    withStoragePermission { startBlob(view, url, filename, mimeType) }
                url.startsWith("data:", ignoreCase = true) ->
                    withStoragePermission {
                        worker.execute {
                            runDownload(filename) {
                                dataInput(url).use { publish(filename, mimeType ?: dataMime(url), it) }
                            }
                        }
                    }
                isAllowedNetworkURL(url, trustedOrigin) ->
                    withStoragePermission {
                        downloadHTTP(
                            view,
                            url,
                            userAgent ?: view.settings.userAgentString,
                            filename,
                            mimeType,
                            trustedOrigin,
                        )
                    }
                else -> toast("已阻止不安全的下载地址")
            }
        }
    }

    fun handleInternal(raw: String, reply: (String) -> Unit) {
        worker.execute {
            val request = runCatching { JSONObject(raw) }.getOrNull()
            val id = request?.optString("id").orEmpty().ifBlank { "invalid" }
            val response =
                try {
                    val method = request?.getString("method")
                        ?: throw MarvoBridgeException(MarvoBridgeErrorCode.INVALID_ARGUMENT, "Malformed download request")
                    val payload = request.getJSONObject("payload")
                    if (payload.optString("__token") != token) {
                        throw MarvoBridgeException(MarvoBridgeErrorCode.UNTRUSTED_ORIGIN, "Invalid download token")
                    }
                    when (method) {
                        "__downloadStart" -> startBlobSession(payload)
                        "__downloadChunk" -> appendBlobChunk(payload)
                        "__downloadFinish" -> finishBlobSession(payload)
                        "__downloadAbort" -> abortBlobSession(payload)
                        else -> throw MarvoBridgeException(
                            MarvoBridgeErrorCode.INVALID_ARGUMENT,
                            "Unknown internal download method",
                        )
                    }
                    MarvoBridgeContract.success(id)
                } catch (error: Throwable) {
                    MarvoBridgeContract.failure(id, error)
                }
            activity.runOnUiThread { reply(response) }
        }
    }

    fun destroy() {
        destroyed = true
        calls.forEach(Call::cancel)
        calls.clear()
        blobs.keys.toList().forEach(::abort)
        worker.shutdownNow()
    }

    private fun downloadHTTP(
        view: WebView,
        url: String,
        userAgent: String,
        fallbackName: String,
        fallbackMime: String?,
        trustedOrigin: String,
    ) {
        val authenticated = originOf(url) == trustedOrigin
        val cookie = if (authenticated) CookieManager.getInstance().getCookie(url) else null
        val referer = view.url?.takeIf { originOf(it) == trustedOrigin }
        toast("正在下载 $fallbackName")
        enqueueHTTP(url, userAgent, fallbackName, fallbackMime, trustedOrigin, cookie, referer, 0)
    }

    private fun enqueueHTTP(
        url: String,
        userAgent: String,
        fallbackName: String,
        fallbackMime: String?,
        trustedOrigin: String,
        cookie: String?,
        referer: String?,
        redirectCount: Int,
    ) {
        if (destroyed) return
        val authenticated = originOf(url) == trustedOrigin
        val request =
            Request.Builder()
                .url(url)
                .header("User-Agent", userAgent)
                .apply {
                    if (authenticated) {
                        cookie?.let { header("Cookie", it) }
                        referer?.let { header("Referer", it) }
                    }
                }
                .build()
        val call = client.newCall(request)
        calls += call
        call.enqueue(
            object : Callback {
                override fun onFailure(call: Call, error: IOException) {
                    calls -= call
                    if (!call.isCanceled()) toast("下载失败")
                }

                override fun onResponse(call: Call, response: Response) {
                    calls -= call
                    try {
                        response.use {
                            if (response.code in HTTP_REDIRECT_CODES) {
                                val location = response.header("Location")
                                val next = location?.let(response.request.url::resolve)
                                if (
                                    next == null ||
                                    !isAllowedNetworkURL(next.toString(), trustedOrigin) ||
                                    redirectCount >= MAX_HTTP_REDIRECTS
                                ) {
                                    throw IOException("Unsafe or excessive redirect")
                                }
                                enqueueHTTP(
                                    next.toString(),
                                    userAgent,
                                    fallbackName,
                                    fallbackMime,
                                    trustedOrigin,
                                    cookie,
                                    referer,
                                    redirectCount + 1,
                                )
                                return
                            }
                            if (!response.isSuccessful || !isAllowedNetworkURL(response.request.url.toString(), trustedOrigin)) {
                                throw IOException("HTTP ${response.code}")
                            }
                            val body = response.body
                            val mime =
                                response.header("Content-Type")?.substringBefore(';')
                                    ?: fallbackMime
                                    ?: "application/octet-stream"
                            val responseDisposition = response.header("Content-Disposition")
                            val name =
                                cleanFilename(
                                    if (responseDisposition.isNullOrBlank()) {
                                        fallbackName
                                    } else {
                                        URLUtil.guessFileName(response.request.url.toString(), responseDisposition, mime)
                                    },
                                    mime,
                                )
                            body.byteStream().use { publish(name, mime, it) }
                            toast("下载完成：$name")
                        }
                    } catch (_: Throwable) {
                        if (!call.isCanceled()) toast("下载失败")
                    }
                }
            },
        )
    }

    private fun startBlob(
        view: WebView,
        url: String,
        filename: String,
        mimeType: String?,
    ) {
        val session = "blob-${System.currentTimeMillis()}-${counter.incrementAndGet()}"
        val script =
            """
            (async () => {
              const session = ${JSONObject.quote(session)};
              const token = ${JSONObject.quote(token)};
              const send = async (method, payload) => {
                const id = session + '-' + Math.random().toString(36).slice(2);
                const raw = await window.__marvoTransport.send(JSON.stringify({
                  id, method, payload: Object.assign({}, payload, { __token: token })
                }));
                const response = JSON.parse(raw);
                if (!response.ok) throw response.error;
              };
              try {
                const response = await fetch(${JSONObject.quote(url)});
                if (!response.ok) throw new Error('HTTP ' + response.status);
                if (!response.body || typeof response.body.getReader !== 'function') {
                  throw new Error('Streaming blob reads are unavailable');
                }
                await send('__downloadStart', {
                  session,
                  filename: ${JSONObject.quote(filename)},
                  mimeType: response.headers.get('Content-Type') || ${JSONObject.quote(mimeType ?: "application/octet-stream")}
                });
                const reader = response.body.getReader();
                while (true) {
                  const part = await reader.read();
                  if (part.done) break;
                  for (let offset = 0; offset < part.value.length; offset += 32768) {
                    const binary = String.fromCharCode.apply(null, part.value.subarray(offset, offset + 32768));
                    await send('__downloadChunk', { session, data: btoa(binary) });
                  }
                }
                await send('__downloadFinish', { session });
              } catch (error) {
                try {
                  await send('__downloadAbort', {
                    session,
                    message: String(error && error.message ? error.message : error)
                  });
                } catch (_) {}
              }
            })();
            """.trimIndent()
        view.evaluateJavascript(script, null)
    }

    private fun startBlobSession(payload: JSONObject) {
        if (destroyed) throw MarvoBridgeException(MarvoBridgeErrorCode.BRIDGE_UNAVAILABLE, "Downloads are unavailable")
        val id = payload.requiredString("session")
        if (blobs.containsKey(id)) {
            throw MarvoBridgeException(MarvoBridgeErrorCode.INVALID_ARGUMENT, "Duplicate download session")
        }
        val mime = payload.optString("mimeType", "application/octet-stream").substringBefore(';')
        val filename = cleanFilename(payload.requiredString("filename"), mime)
        val directory = File(activity.cacheDir, "bridge-download").apply { mkdirs() }
        val file = File.createTempFile("blob-", ".part", directory)
        blobs[id] = BlobSession(file, FileOutputStream(file), filename, mime)
        toast("正在下载 $filename")
    }

    private fun appendBlobChunk(payload: JSONObject) {
        val session = blobs[payload.requiredString("session")]
            ?: throw MarvoBridgeException(MarvoBridgeErrorCode.INVALID_ARGUMENT, "Unknown download session")
        val bytes =
            try {
                Base64.decode(payload.requiredString("data"), Base64.DEFAULT)
            } catch (_: IllegalArgumentException) {
                throw MarvoBridgeException(MarvoBridgeErrorCode.INVALID_ARGUMENT, "Invalid download chunk")
            }
        if (session.file.parentFile?.usableSpace?.let { it < bytes.size + STORAGE_MARGIN } == true) {
            abort(payload.getString("session"))
            throw MarvoBridgeException(MarvoBridgeErrorCode.IO_ERROR, "Insufficient storage")
        }
        session.output.write(bytes)
    }

    private fun finishBlobSession(payload: JSONObject) {
        val id = payload.requiredString("session")
        val session = blobs.remove(id)
            ?: throw MarvoBridgeException(MarvoBridgeErrorCode.INVALID_ARGUMENT, "Unknown download session")
        session.output.close()
        try {
            session.file.inputStream().use { publish(session.filename, session.mimeType, it) }
            toast("下载完成：${session.filename}")
        } finally {
            session.file.delete()
        }
    }

    private fun abortBlobSession(payload: JSONObject) {
        abort(payload.requiredString("session"))
        payload.optString("message").takeIf(String::isNotBlank)?.let { toast("下载失败") }
    }

    private fun runDownload(filename: String, operation: () -> Unit) {
        toast("正在下载 $filename")
        try {
            operation()
            toast("下载完成：$filename")
        } catch (_: Throwable) {
            toast("下载失败")
        }
    }

    private fun publish(filename: String, mimeType: String, input: InputStream) {
        val destination = destination(filename, mimeType)
        try {
            destination.output.use { output ->
                val buffer = ByteArray(DEFAULT_BUFFER_SIZE)
                while (!destroyed) {
                    val read = input.read(buffer)
                    if (read < 0) break
                    if (activity.cacheDir.usableSpace < read + STORAGE_MARGIN) {
                        throw IOException("Insufficient storage")
                    }
                    output.write(buffer, 0, read)
                }
                if (destroyed) throw IOException("Download cancelled")
                output.flush()
            }
            destination.finish(true)
        } catch (error: Throwable) {
            runCatching { destination.output.close() }
            destination.finish(false)
            throw error
        }
    }

    private fun destination(requestedName: String, mimeType: String): Destination {
        val name = cleanFilename(requestedName, mimeType)
        if (Build.VERSION.SDK_INT >= 29) {
            val resolver = activity.contentResolver
            val values =
                ContentValues().apply {
                    put(MediaStore.Downloads.DISPLAY_NAME, name)
                    put(MediaStore.Downloads.MIME_TYPE, mimeType)
                    put(MediaStore.Downloads.RELATIVE_PATH, Environment.DIRECTORY_DOWNLOADS)
                    put(MediaStore.Downloads.IS_PENDING, 1)
                }
            val uri = resolver.insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, values)
                ?: throw IOException("Cannot create download")
            val output = resolver.openOutputStream(uri)
                ?: run {
                    resolver.delete(uri, null, null)
                    throw IOException("Cannot open download")
                }
            return Destination(output) { success ->
                if (success) {
                    resolver.update(
                        uri,
                        ContentValues().apply { put(MediaStore.Downloads.IS_PENDING, 0) },
                        null,
                        null,
                    )
                } else {
                    resolver.delete(uri, null, null)
                }
            }
        }
        val directory = Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOWNLOADS).apply { mkdirs() }
        val file = uniqueFile(directory, name)
        return Destination(FileOutputStream(file)) { success ->
            if (success) {
                android.media.MediaScannerConnection.scanFile(activity, arrayOf(file.absolutePath), arrayOf(mimeType), null)
            } else {
                file.delete()
            }
        }
    }

    private fun withStoragePermission(block: () -> Unit) {
        if (Build.VERSION.SDK_INT >= 29) {
            block()
            return
        }
        activity.runOnUiThread {
            permissions.request(listOf(Manifest.permission.WRITE_EXTERNAL_STORAGE)) { granted ->
                if (granted) block() else toast("没有存储权限，无法下载")
            }
        }
    }

    private fun dataInput(url: String): InputStream {
        val comma = url.indexOf(',')
        if (comma < 5 || url.length - comma > MAX_DATA_URL_LENGTH) {
            throw MarvoBridgeException(MarvoBridgeErrorCode.INVALID_ARGUMENT, "Data URL is invalid or too large")
        }
        val metadata = url.substring(5, comma)
        val data = url.substring(comma + 1)
        if (!metadata.split(';').any { it.equals("base64", true) }) {
            throw MarvoBridgeException(MarvoBridgeErrorCode.UNSUPPORTED, "Only base64 data downloads are supported")
        }
        return Base64.decode(data, Base64.DEFAULT).inputStream()
    }

    private fun dataMime(url: String) =
        url.substringAfter("data:").substringBefore(',').substringBefore(';').ifBlank { "application/octet-stream" }

    private fun resolveFilename(
        view: WebView,
        url: String,
        contentDisposition: String?,
        mimeType: String?,
        callback: (String) -> Unit,
    ) {
        val fallback =
            if (url.startsWith("data:", true) || url.startsWith("blob:", true)) {
                "download"
            } else {
                URLUtil.guessFileName(url, contentDisposition, mimeType)
            }
        view.evaluateJavascript(
            "window.__marvoTakeDownloadFilename?.() ?? null",
        ) { raw ->
            val hinted =
                if (raw.isNullOrBlank() || raw == "null") {
                    null
                } else {
                    runCatching { JSONTokener(raw).nextValue() as? String }.getOrNull()?.takeIf(String::isNotBlank)
                }
            callback(cleanFilename(hinted ?: fallback, mimeType))
        }
    }

    private fun cleanFilename(value: String, mime: String?): String {
        var name =
            value.substringAfterLast('/').substringAfterLast('\\')
                .replace(Regex("[\\u0000-\\u001F<>:\"|?*]"), "_")
                .trim().trim('.').take(160).ifBlank { "download" }
        val extension = mime?.substringBefore(';')?.let { MimeTypeMap.getSingleton().getExtensionFromMimeType(it) }
        if (!extension.isNullOrBlank() && !name.endsWith(".$extension", ignoreCase = true)) {
            name = "${name.substringBeforeLast('.', name)}.$extension"
        }
        return name
    }

    private fun uniqueFile(directory: File, name: String): File {
        val base = name.substringBeforeLast('.', name)
        val extension = name.substringAfterLast('.', "").let { if (it.isBlank()) "" else ".$it" }
        var candidate = File(directory, name)
        var index = 1
        while (candidate.exists()) candidate = File(directory, "$base (${index++})$extension")
        return candidate
    }

    private fun JSONObject.requiredString(name: String): String =
        optString(name).takeIf(String::isNotBlank)
            ?: throw MarvoBridgeException(MarvoBridgeErrorCode.INVALID_ARGUMENT, "$name is required")

    private fun abort(id: String) {
        blobs.remove(id)?.let {
            runCatching { it.output.close() }
            it.file.delete()
        }
    }

    private fun toast(message: String) {
        activity.runOnUiThread { Toast.makeText(activity, message, Toast.LENGTH_SHORT).show() }
    }

    private fun isAllowedNetworkURL(url: String, trustedOrigin: String): Boolean {
        val parsed = url.toHttpUrlOrNull() ?: return false
        return parsed.isHttps || (parsed.scheme == "http" && originOf(url) == trustedOrigin)
    }

    private fun originOf(url: String): String? =
        runCatching {
            val parsed = url.toHttpUrlOrNull() ?: return null
            val defaultPort = if (parsed.isHttps) 443 else 80
            "${parsed.scheme}://${parsed.host}${if (parsed.port == defaultPort) "" else ":${parsed.port}"}"
        }.getOrNull()

    private data class BlobSession(
        val file: File,
        val output: OutputStream,
        val filename: String,
        val mimeType: String,
    )

    private data class Destination(
        val output: OutputStream,
        val finish: (Boolean) -> Unit,
    )

    private companion object {
        const val STORAGE_MARGIN = 4L * 1024 * 1024
        const val MAX_DATA_URL_LENGTH = 48 * 1024 * 1024
        const val MAX_HTTP_REDIRECTS = 5
        val HTTP_REDIRECT_CODES = setOf(300, 301, 302, 303, 307, 308)
    }
}
