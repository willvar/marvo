@file:Suppress("DEPRECATION", "OVERRIDE_DEPRECATION")

package cn.willvar.marvo

import android.annotation.SuppressLint
import android.content.Intent
import android.graphics.Color
import android.net.Uri
import android.net.http.SslError
import android.os.Message
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.webkit.CookieManager
import android.webkit.RenderProcessGoneDetail
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
import androidx.webkit.JavaScriptReplyProxy
import androidx.webkit.WebSettingsCompat
import androidx.webkit.WebMessageCompat
import androidx.webkit.WebViewAssetLoader
import androidx.webkit.WebViewCompat
import androidx.webkit.WebViewFeature
import com.google.android.material.button.MaterialButton
import com.google.android.material.progressindicator.CircularProgressIndicator
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
) {
    private val origin = URI(BuildConfig.SERVER_ORIGIN)
    private var webView: WebView? = null
    private var loading: CircularProgressIndicator? = null
    private var errorView: View? = null
    private var mainLoadFailed = false
    private var fullscreenView: View? = null
    private var fullscreenCallback: WebChromeClient.CustomViewCallback? = null
    private var backPending = false

    fun start() {
        createWebView().loadUrl("${BuildConfig.SERVER_ORIGIN}/.marvo-app/start")
    }

    fun onResume() {
        webView?.onResume()
    }

    fun onPause() {
        webView?.onPause()
    }

    fun handleBack(onUnhandled: () -> Unit) {
        if (exitFullscreen() || backPending) return
        val view = webView
        if (view == null) {
            onUnhandled()
            return
        }
        backPending = true
        view.evaluateJavascript(BACK_LAYER_SCRIPT) { handled ->
            backPending = false
            if (webView !== view || handled == "true") return@evaluateJavascript
            if (view.canGoBack()) view.goBack() else onUnhandled()
        }
    }

    fun destroy() {
        exitFullscreen()
        picker.cancel()
        webView?.let { view ->
            container.removeView(view)
            view.stopLoading()
            view.webChromeClient = null
            view.removeAllViews()
            view.destroy()
        }
        webView = null
        backPending = false
        loading = null
        errorView = null
        container.removeAllViews()
    }

    @SuppressLint("SetJavaScriptEnabled")
    private fun createWebView(): WebView {
        val loader =
            WebViewAssetLoader.Builder()
                .setDomain(origin.host)
                .setHttpAllowed(false)
                .addPathHandler("/.marvo-app/", WebViewAssetLoader.PathHandler(::bootstrapResponse))
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
        installThemeBridge(view)
        CookieManager.getInstance().apply {
            setAcceptCookie(true)
            setAcceptThirdPartyCookies(view, false)
        }
        if (WebViewFeature.isFeatureSupported(WebViewFeature.SAFE_BROWSING_ENABLE)) {
            WebSettingsCompat.setSafeBrowsingEnabled(view.settings, true)
        }
        WebView.setWebContentsDebuggingEnabled(BuildConfig.DEBUG)
        view.setDownloadListener { url, _, _, _, _ -> openExternal(url) }
        container.removeAllViews()
        container.addView(view)
        loading =
            CircularProgressIndicator(activity).also { indicator ->
                indicator.isIndeterminate = true
                container.addView(
                    indicator,
                    FrameLayout.LayoutParams(dp(42), dp(42), Gravity.CENTER),
                )
            }
        webView = view
        return view
    }

    private fun installThemeBridge(view: WebView) {
        if (!WebViewFeature.isFeatureSupported(WebViewFeature.WEB_MESSAGE_LISTENER)) return
        WebViewCompat.addWebMessageListener(
            view,
            THEME_BRIDGE_NAME,
            setOf(BuildConfig.SERVER_ORIGIN),
            object : WebViewCompat.WebMessageListener {
                override fun onPostMessage(
                    view: WebView,
                    message: WebMessageCompat,
                    sourceOrigin: Uri,
                    isMainFrame: Boolean,
                    replyProxy: JavaScriptReplyProxy,
                ) {
                    if (!isMainFrame || !sameOrigin(sourceOrigin)) return
                    when (message.data) {
                        "dark" -> applyWindowColorScheme(true)
                        "light" -> applyWindowColorScheme(false)
                    }
                }
            },
        )
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
    }

    private fun bootstrapResponse(path: String): WebResourceResponse? {
        if (path != "start") return null
        val script =
            """
            <!doctype html>
            <html><head><meta charset="utf-8"></head><body>
            <script>
            try {
              localStorage.setItem('marvo_local_device_id', ${JSONObject.quote(localDeviceID)});
              localStorage.setItem('marvo_android_device_name', ${JSONObject.quote(deviceName)});
            } catch (_) {}
            location.replace(${JSONObject.quote("/user/$userID")});
            </script>
            </body></html>
            """.trimIndent()
        return response(
            mime = "text/html",
            body = script.toByteArray(StandardCharsets.UTF_8),
            cache = "no-store",
        )
    }

    private fun userRouteResponse(path: String): WebResourceResponse? {
        if (path != userID && !path.startsWith("$userID/")) return null
        val input = runCatching { activity.assets.open("index.html") }.getOrNull() ?: return null
        return WebResourceResponse(
            "text/html",
            "UTF-8",
            200,
            "OK",
            mapOf("Cache-Control" to "no-cache", "X-Content-Type-Options" to "nosniff"),
            input,
        )
    }

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

    private fun response(mime: String, body: ByteArray, cache: String) =
        WebResourceResponse(
            mime,
            "UTF-8",
            200,
            "OK",
            mapOf("Cache-Control" to cache, "X-Content-Type-Options" to "nosniff"),
            ByteArrayInputStream(body),
        )

    private fun sameOrigin(uri: Uri): Boolean {
        val port = if (uri.port == -1) 443 else uri.port
        val expectedPort = if (origin.port == -1) 443 else origin.port
        return uri.scheme.equals("https", true) && uri.host.equals(origin.host, true) && port == expectedPort
    }

    private fun allowedInternalPath(path: String): Boolean =
        path == "/user/$userID" || path.startsWith("/user/$userID/") || path.startsWith("/.marvo-app/")

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
        loading?.isVisible = true
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
            container.removeView(view)
            view.destroy()
            webView = null
            mainLoadFailed = true
            showError("Android WebView 已停止运行，请重新加载")
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
                parent.loadUrl(uri.toString())
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
        val BACK_LAYER_SCRIPT =
            """
            (() => {
              const openLayer = document.querySelector([
                '[data-scope="dialog"][data-part="content"][data-state="open"]',
                '[data-scope="menu"][data-part="positioner"][data-state="open"]',
                '[data-scope="select"][data-part="positioner"][data-state="open"]',
                '[data-scope="popover"][data-part="positioner"][data-state="open"]'
              ].join(','));
              if (openLayer) {
                const target = document.activeElement instanceof HTMLElement ? document.activeElement : document;
                target.dispatchEvent(new KeyboardEvent('keydown', {
                  key: 'Escape', code: 'Escape', bubbles: true, cancelable: true
                }));
                return true;
              }
              const sidebarOverlay = document.querySelector('.dsh-overlay');
              if (sidebarOverlay && getComputedStyle(sidebarOverlay).display !== 'none') {
                sidebarOverlay.click();
                return true;
              }
              return false;
            })()
            """.trimIndent()

        const val ERROR_TEXT_TAG = "marvo-error-detail"
        const val THEME_BRIDGE_NAME = "marvoAndroidTheme"
        const val DARK_BACKGROUND = "#1a1b1e"
        const val LIGHT_BACKGROUND = "#ffffff"
    }
}
