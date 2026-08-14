package cn.willvar.marvo

import android.net.Uri
import android.webkit.WebView
import androidx.webkit.JavaScriptReplyProxy
import androidx.webkit.ScriptHandler
import androidx.webkit.WebMessageCompat
import androidx.webkit.WebViewCompat
import androidx.webkit.WebViewFeature
import java.util.concurrent.ConcurrentHashMap
import org.json.JSONObject

internal class MarvoMessageBridge(
    private val webView: WebView,
    private val origin: String,
    private val sameOrigin: (Uri) -> Boolean,
    private val allowedTopLevel: (Uri) -> Boolean,
    private val services: MarvoSystemServices,
    private val downloads: AndroidDownloadManager,
) {
    private val activeIDs = ConcurrentHashMap.newKeySet<String>()
    private var script: ScriptHandler? = null

    fun attach() {
        WebViewCompat.addWebMessageListener(
            webView,
            OBJECT_NAME,
            setOf(origin),
            object : WebViewCompat.WebMessageListener {
                override fun onPostMessage(
                    view: WebView,
                    message: WebMessageCompat,
                    sourceOrigin: Uri,
                    isMainFrame: Boolean,
                    replyProxy: JavaScriptReplyProxy,
                ) {
                    receive(view, message, sourceOrigin, isMainFrame, replyProxy)
                }
            },
        )
        if (WebViewFeature.isFeatureSupported(WebViewFeature.DOCUMENT_START_SCRIPT)) {
            script =
                WebViewCompat.addDocumentStartJavaScript(
                    webView,
                    MarvoBridgeScripts.client(origin),
                    setOf(origin),
                )
        }
    }

    fun detach() {
        script?.remove()
        script = null
        activeIDs.clear()
        runCatching { WebViewCompat.removeWebMessageListener(webView, OBJECT_NAME) }
    }

    private fun receive(
        view: WebView,
        message: WebMessageCompat,
        sourceOrigin: Uri,
        isMainFrame: Boolean,
        reply: JavaScriptReplyProxy,
    ) {
        val raw = message.data ?: return
        val requestID = MarvoBridgeContract.requestID(raw)
        val top = view.url?.let(Uri::parse)
        if (!isMainFrame || !sameOrigin(sourceOrigin) || top == null || !sameOrigin(top) || !allowedTopLevel(top)) {
            reply.postMessage(
                MarvoBridgeContract.failure(
                    requestID,
                    MarvoBridgeErrorCode.UNTRUSTED_ORIGIN,
                    "Bridge calls are only available to the trusted top-level Marvo page",
                ),
            )
            return
        }
        val method = runCatching { JSONObject(raw).optString("method") }.getOrDefault("")
        if (method.startsWith("__download")) {
            downloads.handleInternal(raw) { response -> runCatching { reply.postMessage(response) } }
            return
        }
        val validatedCall =
            try {
                MarvoBridgeContract.validate(raw)
            } catch (error: Throwable) {
                reply.postMessage(MarvoBridgeContract.failure(requestID, error))
                return
            }
        if (!activeIDs.add(validatedCall.id)) {
            reply.postMessage(
                MarvoBridgeContract.failure(
                    validatedCall.id,
                    MarvoBridgeErrorCode.INVALID_ARGUMENT,
                    "Bridge request id is already active",
                ),
            )
            return
        }
        services.execute(validatedCall) { response ->
            activeIDs.remove(validatedCall.id)
            if (webView === view) runCatching { reply.postMessage(response) }
        }
    }

    private companion object {
        const val OBJECT_NAME = "__marvoNative"
    }
}
