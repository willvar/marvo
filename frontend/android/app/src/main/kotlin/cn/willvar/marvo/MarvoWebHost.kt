@file:Suppress("DEPRECATION", "OVERRIDE_DEPRECATION")

package cn.willvar.marvo

import android.annotation.SuppressLint
import android.Manifest
import android.content.Intent
import android.content.res.Configuration
import android.graphics.Color
import android.net.Uri
import android.net.http.SslError
import android.os.Message
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.view.ViewTreeObserver
import android.webkit.CookieManager
import android.webkit.RenderProcessGoneDetail
import android.webkit.PermissionRequest
import android.webkit.SslErrorHandler
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.FrameLayout
import android.widget.LinearLayout
import android.widget.TextView
import androidx.activity.ComponentActivity
import androidx.core.net.toUri
import androidx.core.view.WindowCompat
import androidx.core.view.isVisible
import androidx.webkit.WebSettingsCompat
import androidx.webkit.WebViewAssetLoader
import androidx.webkit.WebViewFeature
import com.google.android.material.button.MaterialButton
import org.json.JSONObject
import java.io.ByteArrayInputStream
import java.net.URI
import java.net.URLConnection
import java.nio.charset.StandardCharsets

internal class MarvoWebHost(
    private val activity: ComponentActivity,
    private val container: FrameLayout,
    private val userID: String,
    private val localDeviceID: String,
    private val deviceName: String,
    private val picker: AndroidFilePicker,
    private val permissionManager: AndroidPermissionManager,
    private val updateManager: AndroidUpdateManager,
    private val downloads: AndroidDownloadManager,
) {
    private val origin = URI(BuildConfig.SERVER_ORIGIN)
    private var webView: WebView? = null
    private var loading: MarvoLoadingIndicator? = null
    private var errorView: View? = null
    private var mainLoadFailed = false
    private var fullscreenView: View? = null
    private var fullscreenCallback: WebChromeClient.CustomViewCallback? = null
    private var backPending = false
    private var queuedBackResult: ((Boolean) -> Unit)? = null
    private var backRequestSequence = 0
    private var rendererRecoveryAttempted = false
    private var bridge: MarvoMessageBridge? = null
    private var startPending = false
    private var startOnPreDraw: ViewTreeObserver.OnPreDrawListener? = null
    private val systemServices = MarvoSystemServices(activity, updateManager, ::applyWindowColorScheme)
    private val startWebView =
        Runnable {
            startPending = false
            if (webView != null || activity.isFinishing || activity.isDestroyed) return@Runnable
            createWebView().loadUrl("${BuildConfig.SERVER_ORIGIN}/user/$userID")
        }

    fun start() {
        applyInitialColorScheme()
        if (!WebViewFeature.isFeatureSupported(WebViewFeature.WEB_MESSAGE_LISTENER)) {
            showError("Android System WebView 版本过旧，无法建立安全连接。请更新 WebView 后重试。")
            return
        }
        showLoading()
        if (webView != null || startPending) return
        startPending = true
        startOnPreDraw =
            object : ViewTreeObserver.OnPreDrawListener {
                override fun onPreDraw(): Boolean {
                    container.viewTreeObserver.takeIf(ViewTreeObserver::isAlive)?.removeOnPreDrawListener(this)
                    startOnPreDraw = null
                    // Finish this frame first so WebView initialization cannot hide the native loading state.
                    container.post(startWebView)
                    return true
                }
            }.also(container.viewTreeObserver::addOnPreDrawListener)
        container.invalidate()
    }

    fun onResume() {
        webView?.onResume()
    }

    fun onPause() {
        CookieManager.getInstance().flush()
        webView?.onPause()
    }

    fun onStop() {
        webView?.evaluateJavascript(
            "document.querySelectorAll('video,audio').forEach(e=>{e.pause();if(e.srcObject)e.srcObject.getTracks().forEach(t=>t.stop())})",
            null,
        )
        exitFullscreen()
    }

    fun handleBack(onResult: (Boolean) -> Unit) {
        if (exitFullscreen()) {
            onResult(true)
            return
        }
        if (backPending) {
            queuedBackResult = onResult
            return
        }
        val view = webView
        if (view == null) {
            onResult(false)
            return
        }
        backPending = true
        val requestSequence = ++backRequestSequence
        val fallback =
            Runnable {
                if (!backPending || requestSequence != backRequestSequence) return@Runnable
                completeBack(webView !== view, onResult)
            }
        container.postDelayed(fallback, APP_BACK_TIMEOUT_MS)
        view.evaluateJavascript(APP_BACK_SCRIPT) { handled ->
            if (!backPending || requestSequence != backRequestSequence) return@evaluateJavascript
            container.removeCallbacks(fallback)
            completeBack(webView !== view || handled == "true", onResult)
        }
    }

    private fun completeBack(handled: Boolean, onResult: (Boolean) -> Unit) {
        backPending = false
        onResult(handled)
        val queued = queuedBackResult ?: return
        queuedBackResult = null
        container.post { handleBack(queued) }
    }

    fun destroy() {
        exitFullscreen()
        picker.cancel()
        permissionManager.cancel()
        bridge?.detach()
        bridge = null
        webView?.let { view ->
            container.removeView(view)
            view.stopLoading()
            view.webChromeClient = null
            view.removeAllViews()
            view.destroy()
        }
        webView = null
        backPending = false
        queuedBackResult = null
        backRequestSequence += 1
        startOnPreDraw?.let { listener ->
            container.viewTreeObserver.takeIf(ViewTreeObserver::isAlive)?.removeOnPreDrawListener(listener)
        }
        startOnPreDraw = null
        container.removeCallbacks(startWebView)
        startPending = false
        loading = null
        errorView = null
        container.removeAllViews()
        systemServices.destroy()
    }

    @SuppressLint("SetJavaScriptEnabled")
    private fun createWebView(): WebView {
        val loader =
            WebViewAssetLoader.Builder()
                .setDomain(origin.rawAuthority)
                .setHttpAllowed(origin.scheme.equals("http", true))
                .addPathHandler("/user/", WebViewAssetLoader.PathHandler(::userRouteResponse))
                .addPathHandler("/assets/", WebViewAssetLoader.PathHandler(::assetResponse))
                .build()
        val client = MarvoWebViewClient(loader)
        val chrome = MarvoChromeClient()
        val view =
            WebView(activity).apply {
                layoutParams = FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT)
                setBackgroundColor(Color.TRANSPARENT)
                visibility = View.INVISIBLE
                settings.apply {
                    javaScriptEnabled = true
                    domStorageEnabled = true
                    databaseEnabled = true
                    allowFileAccess = false
                    allowContentAccess = true
                    allowFileAccessFromFileURLs = false
                    allowUniversalAccessFromFileURLs = false
                    mixedContentMode = WebSettings.MIXED_CONTENT_NEVER_ALLOW
                    mediaPlaybackRequiresUserGesture = true
                    builtInZoomControls = false
                    displayZoomControls = false
                    javaScriptCanOpenWindowsAutomatically = false
                    setSupportMultipleWindows(true)
                    setGeolocationEnabled(false)
                    userAgentString = "${userAgentString.trim()} MarvoAndroid/${BuildConfig.VERSION_NAME}"
                }
                webViewClient = client
                webChromeClient = chrome
            }
        bridge =
            MarvoMessageBridge(
                webView = view,
                origin = BuildConfig.SERVER_ORIGIN,
                sameOrigin = ::sameOrigin,
                allowedTopLevel = { allowedInternalPath(it.path.orEmpty()) },
                services = systemServices,
                downloads = downloads,
            ).also(MarvoMessageBridge::attach)
        CookieManager.getInstance().apply {
            setAcceptCookie(true)
            setAcceptThirdPartyCookies(view, false)
        }
        if (WebViewFeature.isFeatureSupported(WebViewFeature.SAFE_BROWSING_ENABLE)) {
            WebSettingsCompat.setSafeBrowsingEnabled(view.settings, true)
        }
        WebView.setWebContentsDebuggingEnabled(BuildConfig.DEBUG)
        view.setDownloadListener { url, userAgent, disposition, mimeType, _ ->
            downloads.download(view, url, userAgent, disposition, mimeType, BuildConfig.SERVER_ORIGIN)
        }
        container.addView(view, 0)
        webView = view
        return view
    }

    private fun applyInitialColorScheme() {
        val dark =
            activity.resources.configuration.uiMode and Configuration.UI_MODE_NIGHT_MASK ==
                Configuration.UI_MODE_NIGHT_YES
        val background = Color.parseColor(if (dark) DARK_BACKGROUND else LIGHT_BACKGROUND)
        container.setBackgroundColor(background)
        activity.window.statusBarColor = background
        activity.window.navigationBarColor = background
    }

    private fun applyWindowColorScheme(dark: Boolean) {
        val background = Color.parseColor(if (dark) DARK_BACKGROUND else LIGHT_BACKGROUND)
        container.setBackgroundColor(background)
        activity.window.statusBarColor = background
        activity.window.navigationBarColor = background
        WindowCompat.getInsetsController(activity.window, activity.window.decorView).apply {
            isAppearanceLightStatusBars = !dark
            isAppearanceLightNavigationBars = !dark
        }
        (activity as? MainActivity)?.syncNativeColorScheme(dark)
    }

    private fun userRouteResponse(path: String): WebResourceResponse? {
        if (path != userID && !path.startsWith("$userID/")) return null
        val input = appDocument?.let(::ByteArrayInputStream) ?: return null
        return WebResourceResponse(
            "text/html",
            "UTF-8",
            200,
            "OK",
            mapOf("Cache-Control" to "no-cache", "X-Content-Type-Options" to "nosniff"),
            input,
        )
    }

    private val appDocument: ByteArray? by lazy {
        runCatching {
            val source = activity.assets.open("index.html").bufferedReader(StandardCharsets.UTF_8).use { it.readText() }
            val marker = "<head>"
            require(source.contains(marker)) { "Embedded index.html is missing its head element" }
            source
                .replaceFirst(marker, marker + startupScript())
                .toByteArray(StandardCharsets.UTF_8)
        }.getOrNull()
    }

    private fun startupScript(): String {
        val root = "/user/$userID"
        val routeKey = "marvo.android.lastRoute.$userID"
        return """
            <script>
            (() => {
              try {
                localStorage.setItem('marvo_local_device_id', ${scriptString(localDeviceID)});
                localStorage.setItem('marvo_android_device_name', ${scriptString(deviceName)});
                const root = ${scriptString(root)};
                if (location.pathname !== root) return;
                const saved = localStorage.getItem(${scriptString(routeKey)});
                const url = saved ? new URL(saved, location.origin) : null;
                const suffix = url && url.origin === location.origin && url.pathname.startsWith(root + '/')
                  ? url.pathname.slice(root.length)
                  : '';
                if (suffix === '/agent' || suffix === '/trash' || suffix.startsWith('/note/')) {
                  history.replaceState(null, '', url.pathname + url.search + url.hash);
                }
              } catch (_) {}
            })();
            </script>
        """.trimIndent()
    }

    private fun scriptString(value: String) =
        JSONObject.quote(value)
            .replace("<", "\\u003c")
            .replace(">", "\\u003e")
            .replace("&", "\\u0026")

    private fun assetResponse(path: String): WebResourceResponse? {
        if (path.isBlank() || path.split('/').any { it == "." || it == ".." }) return null
        val input = runCatching { activity.assets.open("assets/$path") }.getOrNull() ?: return null
        val mime =
            when (path.substringAfterLast('.', "").lowercase()) {
                "js", "mjs" -> "application/javascript"
                "css" -> "text/css"
                "json" -> "application/json"
                "svg" -> "image/svg+xml"
                "woff" -> "font/woff"
                "woff2" -> "font/woff2"
                else -> URLConnection.guessContentTypeFromName(path) ?: "application/octet-stream"
            }
        return WebResourceResponse(
            mime,
            if (mime.startsWith("text/") || mime.contains("javascript") || mime.contains("json")) "UTF-8" else null,
            200,
            "OK",
            mapOf("Cache-Control" to "public, max-age=31536000, immutable", "X-Content-Type-Options" to "nosniff"),
            input,
        )
    }

    private fun sameOrigin(uri: Uri): Boolean {
        val port = if (uri.port == -1) defaultPort(uri.scheme) else uri.port
        val expectedPort = if (origin.port == -1) defaultPort(origin.scheme) else origin.port
        return uri.scheme.equals(origin.scheme, true) && uri.host.equals(origin.host, true) && port == expectedPort
    }

    private fun defaultPort(scheme: String?) = if (scheme.equals("http", true)) 80 else 443

    private fun allowedInternalPath(path: String): Boolean =
        path == "/user/$userID" || path.startsWith("/user/$userID/")

    private fun navigate(uri: Uri, mainFrame: Boolean): Boolean {
        if (!mainFrame) return !sameOrigin(uri) || !allowedInternalPath(uri.path.orEmpty())
        if (sameOrigin(uri) && allowedInternalPath(uri.path.orEmpty())) return false
        if (uri.scheme.equals("https", true)) openExternal(uri.toString())
        return true
    }

    private fun openExternal(raw: String) {
        val uri = runCatching { raw.toUri() }.getOrNull() ?: return
        if (!uri.scheme.equals("http", true) && !uri.scheme.equals("https", true)) return
        val intent = Intent(Intent.ACTION_VIEW, uri)
        if (intent.resolveActivity(activity.packageManager) != null) {
            runCatching { activity.startActivity(intent) }
        }
    }

    private fun showLoading() {
        errorView?.isVisible = false
        val indicator =
            loading ?: MarvoLoadingIndicator(activity).also { created ->
                container.addView(
                    created,
                    FrameLayout.LayoutParams(dp(24), dp(24), Gravity.CENTER),
                )
                loading = created
            }
        indicator.isVisible = true
        indicator.bringToFront()
    }

    private fun reveal(view: WebView) {
        loading?.isVisible = false
        errorView?.isVisible = false
        view.visibility = View.VISIBLE
        CookieManager.getInstance().flush()
    }

    private fun showError(detail: String) {
        loading?.isVisible = false
        webView?.visibility = View.INVISIBLE
        val existing = errorView
        if (existing != null) {
            existing.findViewWithTag<TextView>(ERROR_TEXT_TAG)?.text = detail
            existing.isVisible = true
            return
        }
        val layout =
            LinearLayout(activity).apply {
                orientation = LinearLayout.VERTICAL
                gravity = Gravity.CENTER
                setPadding(dp(28), dp(28), dp(28), dp(28))
            }
        val heading =
            TextView(activity).apply {
                text = "页面加载失败"
                textSize = 20f
                gravity = Gravity.CENTER
            }
        val message =
            TextView(activity).apply {
                tag = ERROR_TEXT_TAG
                text = detail
                textSize = 14f
                gravity = Gravity.CENTER
                setPadding(0, dp(10), 0, dp(18))
            }
        val retry =
            MaterialButton(activity).apply {
                text = "重新加载"
                setOnClickListener {
                    showLoading()
                    webView?.reload() ?: start()
                }
            }
        layout.addView(heading)
        layout.addView(message)
        layout.addView(retry)
        container.addView(layout, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
        errorView = layout
    }

    private fun enterFullscreen(view: View, callback: WebChromeClient.CustomViewCallback) {
        if (fullscreenView != null) {
            callback.onCustomViewHidden()
            return
        }
        fullscreenView = view
        fullscreenCallback = callback
        webView?.visibility = View.GONE
        container.addView(view, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
    }

    private fun exitFullscreen(): Boolean {
        val view = fullscreenView ?: return false
        container.removeView(view)
        fullscreenView = null
        fullscreenCallback?.onCustomViewHidden()
        fullscreenCallback = null
        webView?.visibility = View.VISIBLE
        return true
    }

    private fun dp(value: Int) = (value * activity.resources.displayMetrics.density).toInt()

    @SuppressLint("MissingOnRenderProcessGone") // Both clients implement the callback; Android Lint misses inner clients.
    private inner class MarvoWebViewClient(
        private val loader: WebViewAssetLoader,
    ) : WebViewClient() {
        override fun shouldInterceptRequest(view: WebView?, request: WebResourceRequest?): WebResourceResponse? =
            request?.url?.let(loader::shouldInterceptRequest)

        override fun shouldOverrideUrlLoading(view: WebView?, request: WebResourceRequest?): Boolean =
            request == null || navigate(request.url, request.isForMainFrame)

        override fun shouldOverrideUrlLoading(view: WebView?, url: String?): Boolean =
            url == null || navigate(url.toUri(), true)

        override fun onPageStarted(view: WebView, url: String?, favicon: android.graphics.Bitmap?) {
            mainLoadFailed = false
            showLoading()
        }

        override fun onPageFinished(view: WebView, url: String?) {
            if (!mainLoadFailed) reveal(view)
        }

        override fun onReceivedError(view: WebView?, request: WebResourceRequest?, error: WebResourceError?) {
            if (request?.isForMainFrame == true) {
                mainLoadFailed = true
                showError(error?.description?.toString() ?: "请检查网络后重试")
            }
        }

        override fun onReceivedHttpError(view: WebView?, request: WebResourceRequest?, response: WebResourceResponse?) {
            if (request?.isForMainFrame == true && response != null) {
                mainLoadFailed = true
                showError("HTTP ${response.statusCode}")
            }
        }

        override fun onReceivedSslError(view: WebView?, handler: SslErrorHandler?, error: SslError?) {
            handler?.cancel()
            mainLoadFailed = true
            showError("无法验证服务器安全证书")
        }

        override fun onRenderProcessGone(view: WebView, detail: RenderProcessGoneDetail): Boolean {
            bridge?.detach()
            bridge = null
            container.removeView(view)
            view.destroy()
            webView = null
            mainLoadFailed = true
            if (!rendererRecoveryAttempted) {
                rendererRecoveryAttempted = true
                showLoading()
                start()
            } else {
                showError("Android WebView 已停止运行，请重新加载")
            }
            return true
        }
    }

    @SuppressLint("MissingOnRenderProcessGone")
    private inner class ExternalWindowClient(
        private val parent: WebView,
    ) : WebViewClient() {
        override fun shouldOverrideUrlLoading(child: WebView?, request: WebResourceRequest?): Boolean {
            val uri = request?.url ?: return true
            if (sameOrigin(uri) && allowedInternalPath(uri.path.orEmpty())) {
                parent.evaluateJavascript(
                    "window.dispatchEvent(new CustomEvent('marvo:navigate',{detail:${JSONObject.quote(uri.toString())}}))",
                    null,
                )
            } else {
                openExternal(uri.toString())
            }
            child?.destroy()
            return true
        }

        override fun onRenderProcessGone(child: WebView, detail: RenderProcessGoneDetail): Boolean {
            child.destroy()
            return true
        }
    }

    private inner class MarvoChromeClient : WebChromeClient() {
        override fun onShowFileChooser(
            webView: WebView?,
            filePathCallback: ValueCallback<Array<Uri>>?,
            fileChooserParams: FileChooserParams?,
        ): Boolean {
            if (filePathCallback == null || fileChooserParams == null || webView?.url?.let(Uri::parse)?.let(::sameOrigin) != true) {
                filePathCallback?.onReceiveValue(null)
                return true
            }
            return picker.show(fileChooserParams, filePathCallback)
        }

        override fun onPermissionRequest(request: PermissionRequest) {
            val source = request.origin
            val top = webView?.url?.let(Uri::parse)
            if (!sameOrigin(source) || top == null || !sameOrigin(top) || !allowedInternalPath(top.path.orEmpty())) {
                request.deny()
                return
            }
            val resources = request.resources.toSet()
            val known =
                setOf(
                    PermissionRequest.RESOURCE_VIDEO_CAPTURE,
                    PermissionRequest.RESOURCE_AUDIO_CAPTURE,
                )
            if (resources.isEmpty() || resources.any { it !in known }) {
                request.deny()
                return
            }
            val permissions =
                buildList {
                    if (PermissionRequest.RESOURCE_VIDEO_CAPTURE in resources) add(Manifest.permission.CAMERA)
                    if (PermissionRequest.RESOURCE_AUDIO_CAPTURE in resources) add(Manifest.permission.RECORD_AUDIO)
                }
            permissionManager.request(permissions) { granted ->
                runCatching {
                    if (granted) request.grant(resources.toTypedArray()) else request.deny()
                }
            }
        }

        override fun onShowCustomView(view: View?, callback: CustomViewCallback?) {
            if (view == null || callback == null) return
            enterFullscreen(view, callback)
        }

        override fun onHideCustomView() {
            exitFullscreen()
        }

        override fun onCreateWindow(
            view: WebView?,
            isDialog: Boolean,
            isUserGesture: Boolean,
            resultMsg: Message?,
        ): Boolean {
            if (view == null || !isUserGesture || resultMsg == null) return false
            val temporary = WebView(activity)
            temporary.webViewClient = ExternalWindowClient(view)
            (resultMsg.obj as? WebView.WebViewTransport)?.webView = temporary
            resultMsg.sendToTarget()
            return true
        }
    }

    private companion object {
        val APP_BACK_SCRIPT =
            """
            (() => {
              try {
                return typeof window.marvo?.back === 'function' && window.marvo.back() === true;
              } catch (_) { return false; }
            })()
            """.trimIndent()

        const val ERROR_TEXT_TAG = "marvo-error-detail"
        const val DARK_BACKGROUND = "#1a1b1e"
        const val LIGHT_BACKGROUND = "#ffffff"
        const val APP_BACK_TIMEOUT_MS = 750L
    }
}
