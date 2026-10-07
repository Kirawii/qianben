package com.qianben.app

import android.app.Notification
import android.service.notification.NotificationListenerService
import android.service.notification.StatusBarNotification
import java.security.MessageDigest
import java.time.Instant
import java.util.UUID
import org.json.JSONObject

class NotificationCollector : NotificationListenerService() {
    companion object {
        @Volatile var connected = false
    }

    private val allowed =
        setOf(
            "com.tencent.mm",
            "com.eg.android.AlipayGphone",
            "com.unionpay",
            "com.icbc",
            "com.chinamworld.main",
            "com.chinamworld.bocmbci",
            "com.android.bankabc",
            "cmb.pb",
            "com.bankcomm.Bankcomm",
            "com.psbc.mbank",
        )
    private val gate = Regex("支付成功|付款成功|交易成功|消费成功|扣款|支出|收入|到账|退款|还款|转出|转入")

    override fun onListenerConnected() {
        connected = true
        activeNotifications?.forEach { capture(it, "RECONNECT_ACTIVE") }
    }

    override fun onListenerDisconnected() {
        connected = false
        super.onListenerDisconnected()
    }

    override fun onDestroy() {
        connected = false
        super.onDestroy()
    }

    override fun onNotificationPosted(sbn: StatusBarNotification) {
        capture(sbn, "POSTED")
    }

    override fun onNotificationRemoved(sbn: StatusBarNotification) {
        QianBenApp.io.execute { LocalDB.get(this).queue().removed(sbn.key) }
    }

    private fun capture(sbn: StatusBarNotification, reason: String) {
        val s = Settings(this)
        if (
            !s.collecting ||
                s.ledger.isBlank() ||
                sbn.packageName !in allowed ||
                (sbn.notification.flags and Notification.FLAG_GROUP_SUMMARY) != 0
        )
            return
        val extras = sbn.notification.extras
        val title = extras.getCharSequence(Notification.EXTRA_TITLE)?.toString().orEmpty()
        val text = extras.getCharSequence(Notification.EXTRA_TEXT)?.toString().orEmpty()
        val big = extras.getCharSequence(Notification.EXTRA_BIG_TEXT)?.toString().orEmpty()
        if (!gate.containsMatchIn("$title $text $big")) return
        if (sbn.packageName == "com.tencent.mm" && title !in setOf("微信支付", "微信收款助手", "微信支付凭证"))
            return
        if (
            sbn.packageName == "com.eg.android.AlipayGphone" &&
                title !in setOf("支付宝", "支付助手", "收款到账", "收钱到账")
        )
            return
        if (title.length + text.length + big.length > 6000) return
        val ledger = s.ledger
        QianBenApp.io.execute {
            try {
                val db = LocalDB.get(this)
                db.runInTransaction {
                    val q = db.queue()
                    val hash =
                        MessageDigest.getInstance("SHA-256")
                            .digest(
                                "${sbn.packageName}\u0000$title\u0000$text\u0000$big\u0000${sbn.postTime}"
                                    .toByteArray()
                            )
                            .joinToString("") { "%02x".format(it) }
                    val old = q.episode(sbn.key)
                    if (old?.hash == hash) return@runInTransaction
                    val episode = old?.episode ?: UUID.randomUUID().toString()
                    val seq = (old?.sequence ?: 0) + 1
                    val id = UUID.randomUUID().toString()
                    val payload =
                        JSONObject()
                            .put("delivery_id", id)
                            .put("device_id", s.device)
                            .put("source_identity", "android.notification:${sbn.packageName}")
                            .put("source_object_key", episode)
                            .put("snapshot_key", "$episode:$seq")
                            .put("capture_sequence", seq)
                            .put("package", sbn.packageName)
                            .put("title", title)
                            .put("text", text)
                            .put("big_text", big)
                            .put("group_summary", false)
                            .put("capture_reason", reason)
                            .put("observed_at", Instant.now().toString())
                            .put(
                                "notification_posted_at",
                                Instant.ofEpochMilli(sbn.postTime).toString(),
                            )
                            .put("content_availability", "AVAILABLE")
                    q.add(Pending(id, ledger, Vault.seal(payload.toString()), s.url))
                    q.episode(Episode(sbn.key, episode, hash, seq))
                }
                s.lastCapture = Instant.now().toString()
                s.captureError = ""
                UploadWorker.enqueue(this)
            } catch (_: Exception) {
                s.captureError = "本地证据保存或上传安排失败，请检查存储与设置"
            }
        }
    }
}
