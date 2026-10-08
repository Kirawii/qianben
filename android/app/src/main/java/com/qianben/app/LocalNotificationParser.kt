package com.qianben.app

import org.json.JSONObject

/** Conservative hints only: notification clocks and default cards are not transaction evidence. */
object LocalNotificationParser {
    private val amount =
        Regex(
            "(?:人民币|金额|支付|支出|收入|退款|扣款|到账|消费|付款|收款|[¥￥])\\s*[:：]?\\s*(\\d{1,12}(?:\\.\\d{1,2})?)(?![\\d.])\\s*(?:元|CNY)?"
        )
    private val foreign =
        Regex("USD|EUR|JPY|HKD|GBP|美元|欧元|日元|港币|英镑|\\$|€|£", RegexOption.IGNORE_CASE)
    private val cny = Regex("人民币|元|CNY|¥|￥", RegexOption.IGNORE_CASE)
    private val supported = Regex("支付成功|付款成功|交易成功|消费人民币|扣款成功|退款成功|退款到账|收款成功|到账人民币|账户支出|账户收入")

    fun summarize(title: String, text: String, big: String, hash: String): JSONObject {
        val content = "$title ${big.ifEmpty { text }}"
        val values = amount.findAll(content).map { it.groupValues[1] }.toSet()
        val cents =
            if (supported.containsMatchIn(content) && values.size == 1) {
                values.single().toBigDecimal().movePointRight(2).longValueExact()
            } else 0L
        val kind =
            when {
                cents <= 0 -> "UNKNOWN"
                content.contains("退款") ||
                    content.contains("收款成功") ||
                    content.contains("到账人民币") ||
                    content.contains("账户收入") -> "CASH_IN"
                else -> "CASH_OUT"
            }
        return JSONObject()
            .put("version", "local-notification-v1")
            .put("amount_minor", cents.toString())
            .put(
                "currency",
                if (!foreign.containsMatchIn(content) && cny.containsMatchIn(content)) "CNY"
                else "UNKNOWN",
            )
            .put("kind", kind)
            .put("raw_hash", hash)
    }
}
