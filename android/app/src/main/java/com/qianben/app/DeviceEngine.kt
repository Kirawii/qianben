package com.qianben.app

import android.annotation.SuppressLint
import android.os.Handler
import android.os.Looper
import android.webkit.JavascriptInterface
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import org.json.JSONArray
import org.json.JSONObject

/** Runs packaged Go code in an isolated WebAssembly VM. No HTTP listener or remote origin. */
object DeviceEngine {
    private val main = Handler(Looper.getMainLooper())
    private var view: WebView? = null
    private var boot: CountDownLatch? = null
    private var bootError = ""
    private const val KEY = "ondevice-state-v1"

    @SuppressLint("SetJavaScriptEnabled")
    private fun prepare(s: Settings) {
        val latch = CountDownLatch(1)
        boot = latch
        main.post {
            try {
                val web = WebView(s.context)
                view = web
                web.settings.javaScriptEnabled = true
                web.settings.allowFileAccess = false
                web.settings.allowContentAccess = false
                web.settings.blockNetworkLoads = true
                web.addJavascriptInterface(
                    object {
                        @JavascriptInterface
                        fun ready() {
                            latch.countDown()
                        }

                        @JavascriptInterface
                        fun failed(message: String) {
                            bootError = "本机账务引擎启动失败，请检查系统 WebView：$message"
                            latch.countDown()
                        }
                    },
                    "DeviceReady",
                )
                web.webViewClient =
                    object : WebViewClient() {
                        override fun shouldInterceptRequest(
                            v: WebView,
                            request: WebResourceRequest,
                        ): WebResourceResponse {
                            val name = request.url.path.orEmpty().removePrefix("/")
                            if (
                                request.url.host != "qianben.invalid" ||
                                    name !in setOf("device.html", "wasm_exec.js", "engine.wasm")
                            )
                                return WebResourceResponse(
                                    "text/plain",
                                    "UTF-8",
                                    java.io.ByteArrayInputStream(byteArrayOf()),
                                )
                            return WebResourceResponse(
                                if (name.endsWith("wasm")) "application/wasm"
                                else if (name.endsWith("js")) "text/javascript" else "text/html",
                                "UTF-8",
                                s.context.assets.open("ondevice/$name"),
                            )
                        }

                        override fun shouldOverrideUrlLoading(
                            v: WebView,
                            request: WebResourceRequest,
                        ) = true
                    }
                web.loadUrl("https://qianben.invalid/device.html")
            } catch (e: Exception) {
                bootError = "无法启动本机账务引擎：${e.message}"
                latch.countDown()
            }
        }
    }

    @Synchronized
    fun request(s: Settings, path: String, body: JSONObject?): String {
        check(Looper.myLooper() != Looper.getMainLooper()) { "本机账务操作须在后台执行" }
        if (boot == null) prepare(s)
        check(boot!!.await(30, TimeUnit.SECONDS)) { "本机账务引擎启动超时，请重试" }
        check(bootError.isBlank()) { bootError }
        val uri = android.net.Uri.parse("https://qianben.invalid$path")
        val parts = uri.path.orEmpty().split('/').filter { it.isNotEmpty() }
        val ledger = parts.getOrNull(2).orEmpty()
        val resource = parts.getOrNull(3).orEmpty()
        val op =
            if (parts.size == 2) {
                if (body == null) "ledgers" else "create-ledger"
            } else
                when (resource) {
                    "accounts" -> if (body == null) "accounts" else "create-account"
                    "events" -> if (body == null) "events" else "save-event"
                    else -> resource
                }
        val args = body ?: JSONObject()
        uri.queryParameterNames.forEach { args.put(it, uri.getQueryParameter(it)) }
        val request =
            JSONObject()
                .put("operation", op)
                .put("ledger_id", ledger)
                .put(
                    "command_id",
                    args.optString(
                        "command_id",
                        if (op == "delete") java.util.UUID.randomUUID().toString() else "",
                    ),
                )
                .put("body", args)
        val q = LocalDB.get(s.context).queue()
        val previous = q.cache(KEY)?.let { Vault.open(it.encrypted) }.orEmpty()
        var reply = ""
        val done = CountDownLatch(1)
        main.post {
            view!!.evaluateJavascript(
                "qianbenInvoke(${JSONObject.quote(previous)}, ${JSONObject.quote(request.toString())})"
            ) { raw ->
                reply = raw
                done.countDown()
            }
        }
        check(done.await(30, TimeUnit.SECONDS)) { "本机账务操作超时，尚未提交，请重试" }
        val result = JSONObject(JSONArray("[$reply]").getString(0))
        if (result.getInt("status") != 200)
            throw ApiFailure(result.getInt("status"), result.getString("error"))
        val next = result.getJSONObject("state").toString()
        // SQLite commits the encrypted complete state, including the command
        // receipt, before any response is visible. Failure leaves old state intact.
        if (next != previous)
            q.cache(
                CachedView(KEY, "@ondevice", Vault.seal(next), java.time.Instant.now().toString())
            )
        s.offline = false
        return result.get("response").toString()
    }
}
