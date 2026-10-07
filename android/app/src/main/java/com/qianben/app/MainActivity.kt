package com.qianben.app

import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.graphics.Color
import android.os.Bundle
import android.provider.Settings as AndroidSettings
import android.text.InputType
import android.widget.*
import java.time.Instant
import java.time.ZoneId
import java.time.ZonedDateTime
import java.util.UUID
import org.json.JSONArray
import org.json.JSONObject

class MainActivity : Activity() {
    private lateinit var settings: Settings
    private lateinit var content: LinearLayout
    private var accounts = JSONArray()
    private var ledgers = JSONArray()
    private var eventCache = JSONArray()
    private var page = "首页"
    private var csvAccount = ""
    private var eventCursor = ""
    private var reportMonth = java.time.YearMonth.now()
    private val ink = Color.rgb(30, 49, 43)

    override fun onCreate(state: Bundle?) {
        super.onCreate(state)
        window.decorView.systemUiVisibility =
            android.view.View.SYSTEM_UI_FLAG_LIGHT_STATUS_BAR or
                android.view.View.SYSTEM_UI_FLAG_LIGHT_NAVIGATION_BAR
        settings = Settings(this)
        render()
        if (settings.token.isNotBlank()) reload()
    }

    private fun id() = UUID.randomUUID().toString()

    private fun dp(value: Int) = (value * resources.displayMetrics.density).toInt()

    private fun minor(value: String) =
        value.toBigDecimal().movePointRight(2).longValueExact().toString()

    private val timeFormat = java.time.format.DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm:ss")

    private fun displayTime(value: String) =
        try {
            Instant.parse(value).atZone(ZoneId.systemDefault()).format(timeFormat)
        } catch (_: Exception) {
            ZonedDateTime.now().format(timeFormat)
        }

    private fun instant(value: String) =
        java.time.LocalDateTime.parse(value, timeFormat)
            .atZone(ZoneId.systemDefault())
            .toInstant()
            .toString()

    private fun label(s: String, size: Float = 16f) {
        content.addView(
            TextView(this).apply {
                text = s
                textSize = size
                setTextColor(ink)
                setPadding(0, 14, 0, 12)
            }
        )
    }

    private fun button(s: String, action: () -> Unit) {
        content.addView(
            Button(this).apply {
                text = s
                setOnClickListener { action() }
            }
        )
    }

    private fun field(title: String, value: String = "", numeric: Boolean = false): EditText {
        label(title, 13f)
        return EditText(this).apply {
            setText(value)
            setSingleLine()
            if (numeric)
                inputType = InputType.TYPE_CLASS_NUMBER or InputType.TYPE_NUMBER_FLAG_DECIMAL
            content.addView(this)
        }
    }

    private fun spinner(title: String, values: List<String>): Spinner {
        label(title, 13f)
        return Spinner(this).apply {
            adapter =
                ArrayAdapter(
                    this@MainActivity,
                    android.R.layout.simple_spinner_dropdown_item,
                    values,
                )
            content.addView(this)
        }
    }

    private fun money(value: String): String =
        try {
            java.math.BigDecimal(value).movePointLeft(2).setScale(2).toPlainString()
        } catch (_: Exception) {
            "—"
        }

    private fun eventStatus(ev: JSONObject): String {
        val state =
            when (ev.getString("status")) {
                "ACTIVE" -> "已确认并入账"
                "MERGED" -> "已合并"
                "SPLIT" -> "已拆分"
                "HISTORICAL_ONLY" -> "启用时间前的历史记录"
                else -> if (ev.optBoolean("posted")) "已更新余额，仍待确认" else "未入账，待确认"
            }
        val reason =
            when (ev.optString("review_reason")) {
                "PURPOSE_REQUIRED" -> "请确认用途"
                "EVENT_TIME_REQUIRED" -> "请补实际发生时间"
                "AMOUNT_REQUIRED" -> "请核对金额"
                "FUNDING_ACCOUNT_UNKNOWN" -> "请选择实际账户"
                "ACCOUNT_NOT_INITIALIZED" -> "账户尚未设置期初"
                "CNY_SETTLEMENT_REQUIRED" -> "需要实际人民币结算信息"
                "CUTOVER_TIME_AMBIGUOUS" -> "请明确与启用时点的先后关系"
                "NEW_EVIDENCE_REVIEW" -> "收到新证据，请核对"
                "REFUND_ORIGINAL_REQUIRED" -> "请关联退款原消费"
                "ASSET_CONFIRMATION_REQUIRED" -> "请确认资产名称"
                "REPAYMENT_TARGET_REQUIRED" -> "请选择还款目标"
                "FINAL_FINANCIAL_STATE_REQUIRED" -> "请确认最终交易状态"
                else -> ""
            }
        return "$state  $reason"
    }

    private fun task(work: () -> Unit) {
        QianBenApp.io.execute {
            try {
                work()
            } catch (e: Exception) {
                runOnUiThread {
                    AlertDialog.Builder(this)
                        .setTitle("操作未完成")
                        .setMessage(e.message ?: "请检查连接后重试")
                        .setPositiveButton("知道了", null)
                        .show()
                }
            }
        }
    }

    private fun api(resource: String, body: JSONObject? = null) =
        if (body?.has("command_id") == true)
            Api.command(this, settings, "/v1/ledgers/${settings.ledger}/$resource", body)
        else Api.request(settings, "/v1/ledgers/${settings.ledger}/$resource", body)

    private fun render() {
        val root =
            LinearLayout(this).apply {
                orientation = LinearLayout.VERTICAL
                setPadding(dp(20), dp(30), dp(20), dp(8))
                setOnApplyWindowInsetsListener { view, insets ->
                    view.setPadding(
                        dp(20),
                        insets.systemWindowInsetTop + dp(16),
                        dp(20),
                        insets.systemWindowInsetBottom + dp(8),
                    )
                    insets
                }
                setBackgroundColor(Color.rgb(247, 247, 239))
            }
        root.addView(
            TextView(this).apply {
                text = "钱本"
                textSize = 30f
                setTextColor(ink)
            }
        )
        root.addView(
            TextView(this).apply {
                text = "把消费、费用和现金流分开看"
                setTextColor(ink)
                setPadding(0, 4, 0, 18)
            }
        )
        val tabs = LinearLayout(this)
        listOf("首页", "账户", "记账", "待确认", "设置").forEach { name ->
            tabs.addView(
                Button(this).apply {
                    text = name
                    textSize = 12f
                    setOnClickListener {
                        page = name
                        render()
                    }
                },
                LinearLayout.LayoutParams(0, dp(52), 1f),
            )
        }
        root.addView(tabs)
        content = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL }
        root.addView(
            ScrollView(this).apply { addView(content) },
            LinearLayout.LayoutParams(-1, 0, 1f),
        )
        setContentView(root)
        if (settings.token.isBlank()) {
            connection()
            return
        }
        if (settings.ledger.isBlank() && page != "设置") {
            setupLedger()
            return
        }
        when (page) {
            "首页" -> home()
            "账户" -> accountPage()
            "记账" -> eventForm(null)
            "待确认" -> review()
            else -> connection()
        }
    }

    private fun connection() {
        label("连接你的账本", 22f)
        val url = field("API 地址", settings.url)
        val token = field("访问令牌", settings.token)
        token.inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_PASSWORD
        button("保存并连接") {
            task {
                require(url.text.toString().startsWith("https://") || BuildConfig.DEBUG) {
                    "正式版需要 HTTPS"
                }
                settings.url = url.text.toString()
                settings.token = token.text.toString()
                reload()
            }
        }
        label("通知仅在你启用后采集。聊天消息不会作为记账来源；系统隐藏的内容无法恢复。", 14f)
        val collect =
            Switch(this).apply {
                text = "启用支付通知采集"
                isChecked = settings.collecting
                setOnCheckedChangeListener { _, on -> settings.collecting = on }
            }
        content.addView(collect)
        label(
            "通知监听：${if(NotificationCollector.connected) "已连接" else "未连接"}\n最近采集：${settings.lastCapture.ifBlank{"尚无记录"}}",
            13f,
        )
        if (settings.captureError.isNotBlank()) label(settings.captureError, 14f)
        button("打开系统通知使用权") {
            startActivity(Intent(AndroidSettings.ACTION_NOTIFICATION_LISTENER_SETTINGS))
        }
        button("重试上传") {
            UploadWorker.enqueue(this)
            Toast.makeText(this, "已安排上传", Toast.LENGTH_SHORT).show()
        }
        task {
            val q = LocalDB.get(this).queue()
            val n = q.count()
            val commands = q.commandCount()
            runOnUiThread { if (page == "设置") label("本地待处理证据：$n 条\n待处理操作：$commands 条（含需检查的拒绝项）") }
        }
        button("切换账本") {
            if (ledgers.length() > 0) {
                val names =
                    (0 until ledgers.length())
                        .map { ledgers.getJSONObject(it).getString("name") }
                        .toTypedArray()
                AlertDialog.Builder(this)
                    .setTitle("选择账本")
                    .setItems(names) { _, i ->
                        settings.ledger = ledgers.getJSONObject(i).getString("id")
                        reload()
                    }
                    .show()
            }
        }
        button("新建账本") {
            settings.ledger = ""
            page = "首页"
            render()
        }
        if (settings.ledger.isNotBlank()) {
            button("导入 CSV") {
                val available =
                    (0 until accounts.length())
                        .map { accounts.getJSONObject(it) }
                        .filter { it.getString("code").startsWith("user.") }
                AlertDialog.Builder(this)
                    .setTitle("选择账单账户")
                    .setItems(available.map { it.getString("name") }.toTypedArray()) { _, i ->
                        csvAccount = available[i].getString("id")
                        startActivityForResult(
                            Intent(Intent.ACTION_OPEN_DOCUMENT).apply {
                                type = "text/*"
                                addCategory(Intent.CATEGORY_OPENABLE)
                            },
                            41,
                        )
                    }
                    .show()
            }
            button("查看固定资产原值") {
                task {
                    val assets = JSONArray(api("assets"))
                    runOnUiThread {
                        val text =
                            (0 until assets.length()).joinToString("\n") {
                                val a = assets.getJSONObject(it)
                                "${a.getString("title")} · ¥ ${money(a.getString("original_minor"))}"
                            }
                        AlertDialog.Builder(this)
                            .setTitle("固定资产")
                            .setMessage(text.ifBlank { "尚无资产记录" })
                            .setPositiveButton("关闭", null)
                            .show()
                    }
                }
            }
            button("管理商户分类规则") { rulePage() }
            button("删除当前账本") {
                val input = EditText(this).apply { hint = "输入完整账本名称" }
                AlertDialog.Builder(this)
                    .setTitle("删除账本及其证据")
                    .setMessage("此操作清除服务器账本和本地待上传队列，不能撤销。备份中的数据需单独处理。")
                    .setView(input)
                    .setNegativeButton("取消", null)
                    .setPositiveButton("删除") { _, _ ->
                        task {
                            api("delete", JSONObject().put("confirm_name", input.text.toString()))
                            LocalDB.get(this).queue().clear(settings.ledger)
                            LocalDB.get(this).queue().clearCommands(settings.ledger)
                            settings.ledger = ""
                            settings.collecting = false
                            reload()
                        }
                    }
                    .show()
            }
        }
    }

    @Deprecated("Uses system document picker")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode == 41 && resultCode == RESULT_OK) {
            val uri = data?.data ?: return
            task {
                val bytes =
                    contentResolver.openInputStream(uri)?.use {
                        val output = java.io.ByteArrayOutputStream()
                        val buffer = ByteArray(8192)
                        while (output.size() <= 800000) {
                            val n = it.read(buffer, 0, minOf(buffer.size, 800001 - output.size()))
                            if (n < 0) break
                            output.write(buffer, 0, n)
                        }
                        output.toByteArray()
                    } ?: throw IllegalStateException("文件无法读取")
                require(bytes.size <= 800000) { "CSV 超过大小限制" }
                api(
                    "csv",
                    JSONObject()
                        .put("command_id", id())
                        .put("account_id", csvAccount)
                        .put("content", String(bytes, Charsets.UTF_8)),
                )
                reload()
            }
        }
    }

    private fun reload() {
        task {
            ledgers = JSONArray(Api.request(settings, "/v1/ledgers"))
            if (settings.ledger.isBlank() && ledgers.length() > 0)
                settings.ledger = ledgers.getJSONObject(0).getString("id")
            if (settings.ledger.isNotBlank()) {
                accounts = JSONArray(api("accounts"))
                eventCache = JSONArray(api("events"))
            }
            runOnUiThread { render() }
        }
    }

    private fun setupLedger() {
        label("正式记账从统一时点开始", 22f)
        label("所有账户使用同一个期初时间，之前的流水不会重复改变余额。", 14f)
        val name = field("账本名称", "我的账本")
        val cutoff = field("启用时间（年-月-日 时:分:秒）", displayTime(Instant.now().toString()))
        val ledgerID = id()
        button("创建账本") {
            task {
                val result =
                    JSONObject(
                        Api.request(
                            settings,
                            "/v1/ledgers",
                            JSONObject()
                                .put("id", ledgerID)
                                .put("name", name.text.toString())
                                .put("cutover_time", instant(cutoff.text.toString())),
                        )
                    )
                settings.ledger = result.getString("id")
                page = "账户"
                reload()
            }
        }
    }

    private fun home() {
        label("${reportMonth} 财务概览", 22f)
        button("上个月") {
            reportMonth = reportMonth.minusMonths(1)
            render()
        }
        button("下个月") {
            reportMonth = reportMonth.plusMonths(1)
            render()
        }
        val zone = ZoneId.systemDefault()
        val selectedMonth = reportMonth
        val from = selectedMonth.atDay(1).atStartOfDay(zone).toInstant()
        val to = selectedMonth.plusMonths(1).atDay(1).atStartOfDay(zone).toInstant()
        task {
            val r =
                JSONObject(
                    api(
                        "reports?from=${java.net.URLEncoder.encode(from.toString(),"UTF-8")}&to=${java.net.URLEncoder.encode(to.toString(),"UTF-8")}"
                    )
                )
            runOnUiThread {
                if (page != "首页" || reportMonth != selectedMonth) return@runOnUiThread
                label("期末账面净资产   ¥ ${money(r.getString("net_worth_minor"))}", 24f)
                label(
                    "资产：${money(r.getString("assets_minor"))} · 负债：${money(r.getString("liabilities_minor"))}",
                    18f,
                )
                label("包含待查、应收与转账在途；未与银行核对。未录入账户和期初会影响完整性。", 14f)
                label(
                    "会计收入   ¥ ${money(r.getString("income_minor"))}\n会计结余   ¥ ${money(r.getString("surplus_minor"))}",
                    20f,
                )
                label("生活消费   ¥ ${money(r.getString("consumption_minor"))}", 24f)
                label("会计费用   ¥ ${money(r.getString("expense_minor"))}", 20f)
                label("现金变动   ¥ ${money(r.getString("cash_change_minor"))}", 20f)
                label(
                    "外部现金流：${money(r.getString("external_cash_minor"))}\n内部划转净额：${money(r.getString("internal_cash_minor"))}\n未分类现金净额：${money(r.getString("unresolved_cash_minor"))}",
                    14f,
                )
                label(
                    "待查金额   ¥ ${money(r.getString("unresolved_balance_minor"))}\n待确认事件   ${r.getLong("review_count")} 笔",
                    18f,
                )
                label(
                    "转账在途净额   ¥ ${money(r.getString("transfer_clearing_minor"))}\n未关联转账端   ${r.getLong("unlinked_transfer_count")} 笔",
                    14f,
                )
                label(
                    "账本版本 ${r.getLong("source_version")} · ${r.getString("algorithm_version")}",
                    12f,
                )
                label("生活消费分类（退款按发生月冲减）", 18f)
                val totals = r.getJSONArray("categories")
                for (i in 0 until totals.length()) {
                    val total = totals.getJSONObject(i)
                    label(
                        "${total.getString("category")}   ¥ ${money(total.getString("consumption_minor"))}",
                        16f,
                    )
                }
                button("刷新") { reload() }
            }
        }
    }

    private fun rulePage() {
        content.removeAllViews()
        label("商户分类规则", 22f)
        label("只影响之后的新证据，不重写历史。撤销后旧规则不会自动恢复。", 14f)
        button("返回设置") { render() }
        task {
            val rules = JSONArray(api("rules"))
            runOnUiThread {
                if (page != "设置") return@runOnUiThread
                if (rules.length() == 0) label("尚无分类规则；确认事件时可勾选记住商户分类。")
                for (i in 0 until rules.length()) {
                    val rule = rules.getJSONObject(i)
                    label(
                        "${rule.getString("merchant")} → ${rule.getString("category")} · v${rule.getLong("version")} · ${if(rule.getBoolean("active")) "生效" else "已失效"}"
                    )
                    if (rule.getBoolean("active"))
                        button("撤销该规则") {
                            AlertDialog.Builder(this)
                                .setTitle("撤销 ${rule.getString("merchant")} 的分类规则？")
                                .setMessage("历史事件保持原分类，之后的新证据不再使用此规则。")
                                .setNegativeButton("取消", null)
                                .setPositiveButton("撤销") { _, _ ->
                                    task {
                                        api(
                                            "revoke-rule",
                                            JSONObject()
                                                .put("command_id", id())
                                                .put("rule_id", rule.getString("id"))
                                                .put("expected_version", rule.getLong("version")),
                                        )
                                        runOnUiThread { rulePage() }
                                    }
                                }
                                .show()
                        }
                }
            }
        }
    }

    private fun accountPage() {
        label("账户与期初", 22f)
        for (i in 0 until accounts.length()) {
            val a = accounts.getJSONObject(i)
            if (!a.getString("code").startsWith("user.")) continue
            label("${a.getString("name")}   ¥ ${money(a.getString("balance_minor"))}", 18f)
            if (!a.getBoolean("initialized")) button("设置 ${a.getString("name")} 的期初") { opening(a) }
            else button("检查 ${a.getString("name")} 的实际余额") { balanceCheck(a) }
        }
        button("查看余额检查记录") {
            task {
                val checks = JSONArray(api("balance-checks"))
                runOnUiThread {
                    val text =
                        (0 until checks.length()).joinToString("\n\n") {
                            val c = checks.getJSONObject(it)
                            val name =
                                (0 until accounts.length())
                                    .map { n -> accounts.getJSONObject(n) }
                                    .firstOrNull { it.getString("id") == c.getString("account_id") }
                                    ?.getString("name") ?: "账户"
                            "$name · ${displayTime(c.getString("as_of"))}\n实际 ${money(c.getString("actual_minor"))} · 账面 ${money(c.getString("book_minor"))} · 差额 ${money(c.getString("difference_minor"))}\n记录版本 ${c.getLong("source_version")}，之后的修订可能使结果过时。"
                        }
                    AlertDialog.Builder(this)
                        .setTitle("最近 100 条余额检查")
                        .setMessage(text.ifBlank { "尚无检查记录" })
                        .setPositiveButton("关闭", null)
                        .show()
                }
            }
        }
        val name = field("账户名称")
        val type = spinner("账户类型", listOf("现金 / 银行卡 / 钱包余额", "信用卡负债"))
        val provider = field("机构（可选）")
        val tail = field("尾号（可选，仅填末四位）")
        val command = id()
        button("添加账户") {
            task {
                api(
                    "accounts",
                    JSONObject()
                        .put("command_id", command)
                        .put("name", name.text.toString())
                        .put("type", if (type.selectedItemPosition == 0) "ASSET" else "LIABILITY")
                        .put("cash", type.selectedItemPosition == 0)
                        .put("provider", provider.text.toString())
                        .put("masked_ref", tail.text.toString()),
                )
                reload()
            }
        }
    }

    private fun balanceCheck(a: JSONObject) {
        content.removeAllViews()
        label("${a.getString("name")} · 余额检查", 22f)
        label(
            "输入同一时点的实际${if(a.getString("type") == "LIABILITY") "欠款（欠款为正）" else "余额"}。仅保存比较结果，不调整账务，也不代表完整银行对账。",
            14f,
        )
        val actual = field("实际金额（元；可输入负值）")
        val at = field("检查时点（年-月-日 时:分:秒）", displayTime(Instant.now().toString()))
        val command = id()
        button("保存检查") {
            task {
                val result =
                    JSONObject(
                        api(
                            "balance-checks",
                            JSONObject()
                                .put("command_id", command)
                                .put("account_id", a.getString("id"))
                                .put("as_of", instant(at.text.toString()))
                                .put("actual_minor", minor(actual.text.toString())),
                        )
                    )
                runOnUiThread {
                    AlertDialog.Builder(this)
                        .setTitle("余额比较结果")
                        .setMessage(
                            "账面：${money(result.getString("book_minor"))} 元\n实际：${money(result.getString("actual_minor"))} 元\n差额（实际减账面）：${money(result.getString("difference_minor"))} 元\n如有差额，请检查遗漏流水、待查款或期初。账务未调整。"
                        )
                        .setPositiveButton("关闭") { _, _ -> render() }
                        .show()
                }
            }
        }
        button("返回账户") { render() }
    }

    private fun opening(a: JSONObject) {
        content.removeAllViews()
        label("${a.getString("name")} · 期初", 22f)
        val amount =
            field(if (a.getString("type") == "LIABILITY") "欠款金额（元）" else "余额（元）", "0", true)
        val command = id()
        button("确认期初") {
            task {
                val l =
                    (0 until ledgers.length())
                        .map { ledgers.getJSONObject(it) }
                        .first { it.getString("id") == settings.ledger }
                api(
                    "opening",
                    JSONObject()
                        .put("command_id", command)
                        .put("account_id", a.getString("id"))
                        .put("expected_revision", a.getLong("revision"))
                        .put("amount_minor", minor(amount.text.toString()))
                        .put("as_of", l.getString("cutover_time"))
                        .put(
                            "meaning",
                            if (a.getString("type") == "LIABILITY") "DEBT" else "BALANCE",
                        ),
                )
                reload()
            }
        }
    }

    private fun eventForm(old: JSONObject?) {
        content.removeAllViews()
        label(if (old == null) "手动记账" else "确认 / 修订事件", 22f)
        val facts = old?.getJSONObject("facts") ?: JSONObject()
        val requiresCNY = old != null && facts.optString("currency") != "CNY"
        if (requiresCNY) label("证据缺少明确的人民币结算信息，请核对实际人民币金额后确认。", 14f)
        val kinds =
            listOf(
                "EXPENSE",
                "INCOME",
                "CASH_OUT",
                "CASH_IN",
                "TRANSFER_OUT",
                "TRANSFER_IN",
                "ADVANCE",
                "REIMBURSEMENT",
                "ASSET_PURCHASE",
                "REFUND",
                "ASSET_REFUND",
                "CARD_REPAYMENT",
            )
        val names =
            listOf(
                "生活消费",
                "收入",
                "用途待查的扣款",
                "来源待查的入款",
                "内部转账转出",
                "内部转账转入",
                "报销垫付",
                "报销到账",
                "固定资产购置",
                "消费退款",
                "资产退款",
                "信用卡还款",
            )
        val kind = spinner("类型", names)
        kind.setSelection(kinds.indexOf(facts.optString("kind")).coerceAtLeast(0))
        val amount =
            field(
                "金额（元）",
                if (facts.has("amount_minor") && !requiresCNY)
                    money(facts.getString("amount_minor"))
                else "",
                true,
            )
        val available =
            (0 until accounts.length())
                .map { accounts.getJSONObject(it) }
                .filter { it.getString("code").startsWith("user.") && it.getBoolean("initialized") }
        if (available.isEmpty()) {
            label("请先添加账户并设置期初余额")
            return
        }
        val account =
            spinner("实际付款 / 收款账户", listOf("请选择实际账户") + available.map { it.getString("name") })
        account.setSelection(
            available
                .indexOfFirst { it.getString("id") == facts.optString("funding_account_id") }
                .plus(1)
        )
        val at =
            field(
                "实际发生时间（年-月-日 时:分:秒）",
                if (
                    old != null &&
                        (facts.optString("occurred_at").isBlank() ||
                            facts.optString("occurred_at") == "null")
                )
                    ""
                else displayTime(facts.optString("occurred_at")),
            )
        val merchant = field("商户", facts.optString("merchant"))
        val category = field("分类", facts.optString("category"))
        val originals =
            (0 until eventCache.length())
                .map { eventCache.getJSONObject(it) }
                .filter {
                    it.optBoolean("posted") &&
                        it.getJSONObject("facts").optString("kind") in
                            listOf("EXPENSE", "ASSET_PURCHASE") &&
                        it.getString("id") != old?.optString("id")
                }
                .toMutableList()
        if (
            facts.optString("original_event_id").isNotBlank() &&
                originals.none { it.getString("id") == facts.getString("original_event_id") }
        ) {
            originals.add(
                JSONObject()
                    .put("id", facts.getString("original_event_id"))
                    .put("reference_only", true)
            )
        }
        val original =
            spinner(
                "退款关联原消费（退款时选择）",
                listOf("尚未关联") +
                    originals.map {
                        if (it.optBoolean("reference_only")) "已关联原消费（更早记录）"
                        else
                            "${it.getJSONObject("facts").optString("merchant")} · ¥ ${money(it.getJSONObject("facts").getString("amount_minor"))} · ${displayTime(it.getJSONObject("facts").optString("occurred_at"))}"
                    },
            )
        original.setSelection(
            originals.indexOfFirst { it.getString("id") == facts.optString("original_event_id") } +
                1
        )
        val asset = field("资产名称（资产购置时填写）", facts.optString("asset_title"))
        val note = field("备注", facts.optString("note"))
        val eventID = old?.getString("id") ?: id()
        val command = id()
        val cards = available.filter { it.getString("type") == "LIABILITY" }
        val card = spinner("还款目标信用卡（仅还款时选择）", listOf("请选择") + cards.map { it.getString("name") })
        card.setSelection(
            cards.indexOfFirst { it.getString("id") == facts.optString("repayment_account_id") } + 1
        )
        val historical =
            CheckBox(this).apply {
                text = "原消费在记账起点之前（消费退款）"
                isChecked = facts.optBoolean("historical_original")
                content.addView(this)
            }
        val learn =
            CheckBox(this).apply {
                text = "今后同商户沿用此分类"
                content.addView(this)
            }
        button("确认保存") {
            task {
                require(account.selectedItemPosition > 0) { "请选择真实付款或收款账户" }
                val f =
                    JSONObject()
                        .put("kind", kinds[kind.selectedItemPosition])
                        .put("amount_minor", minor(amount.text.toString()))
                        .put("currency", "CNY")
                        .put("occurred_at", instant(at.text.toString()))
                        .put("time_precision", "EXACT")
                        .put(
                            "funding_account_id",
                            available[account.selectedItemPosition - 1].getString("id"),
                        )
                        .put("merchant", merchant.text.toString())
                        .put("category", category.text.toString())
                        .put("asset_title", asset.text.toString())
                        .put("note", note.text.toString())
                        .put("historical_original", historical.isChecked)
                if (original.selectedItemPosition > 0)
                    f.put(
                        "original_event_id",
                        originals[original.selectedItemPosition - 1].getString("id"),
                    )
                if (card.selectedItemPosition > 0)
                    f.put(
                        "repayment_account_id",
                        cards[card.selectedItemPosition - 1].getString("id"),
                    )
                api(
                    "events",
                    JSONObject()
                        .put("command_id", command)
                        .put("event_id", eventID)
                        .put("expected_revision", old?.getLong("revision") ?: 0)
                        .put("learn_category", learn.isChecked)
                        .put("facts", f),
                )
                page = "首页"
                reload()
            }
        }
    }

    private fun review() {
        label("事件与待确认", 22f)
        if (eventCursor.isNotBlank())
            button("返回最新事件") {
                eventCursor = ""
                render()
            }
        task {
            val events =
                JSONArray(
                    api(if (eventCursor.isBlank()) "events" else "events?before=$eventCursor")
                )
            if (eventCursor.isNotBlank()) {
                val known =
                    (0 until eventCache.length())
                        .map { eventCache.getJSONObject(it).getString("id") }
                        .toSet()
                for (i in 0 until events.length()) {
                    val ev = events.getJSONObject(i)
                    if (ev.getString("id") !in known) eventCache.put(ev)
                }
            }
            runOnUiThread {
                if (page != "待确认") return@runOnUiThread
                for (i in 0 until events.length()) {
                    val ev = events.getJSONObject(i)
                    val f = ev.getJSONObject("facts")
                    label(
                        "${f.optString("merchant").ifBlank{"未识别商户"}} · ¥ ${money(f.getString("amount_minor"))}\n${eventStatus(ev)}"
                    )
                    if (!f.isNull("occurred_at"))
                        label(displayTime(f.getString("occurred_at")), 12f)
                    button("查看证据") {
                        task {
                            val evidence = JSONArray(api("evidence?event_id=${ev.getString("id")}"))
                            runOnUiThread {
                                AlertDialog.Builder(this)
                                    .setTitle("原始证据")
                                    .setMessage(evidence.toString(2))
                                    .setPositiveButton("关闭", null)
                                    .show()
                            }
                        }
                    }
                    if (ev.getString("status") !in listOf("MERGED", "SPLIT")) {
                        button("查看并确认") { eventForm(ev) }
                        button("合并重复事件") { mergeDialog(ev, events) }
                        button("拆分为两笔") { splitDialog(ev) }
                        if (f.optString("kind") == "TRANSFER_OUT")
                            button("关联转入端") { transferDialog(ev, events) }
                        if (f.optString("kind") in listOf("TRANSFER_OUT", "TRANSFER_IN"))
                            button("解除转账关联") {
                                task {
                                    val relations =
                                        JSONArray(api("relations?event_id=${ev.getString("id")}"))
                                    val transfers =
                                        (0 until relations.length())
                                            .map { relations.getJSONObject(it) }
                                            .filter { it.getString("type") == "TRANSFER_LEG_OF" }
                                    runOnUiThread {
                                        if (transfers.isEmpty()) {
                                            Toast.makeText(this, "尚未关联", Toast.LENGTH_SHORT).show()
                                        } else {
                                            AlertDialog.Builder(this)
                                                .setMessage("解除此转账关联后，两端保留各自分录，可分别修订。")
                                                .setNegativeButton("取消", null)
                                                .setPositiveButton("解除") { _, _ ->
                                                    task {
                                                        api(
                                                            "unlink-transfer",
                                                            JSONObject()
                                                                .put("command_id", id())
                                                                .put(
                                                                    "relation_id",
                                                                    transfers[0].getString("id"),
                                                                ),
                                                        )
                                                        reload()
                                                    }
                                                }
                                                .show()
                                        }
                                    }
                                }
                            }
                    }
                }
                if (events.length() == 500)
                    button("更早的事件") {
                        eventCursor = events.getJSONObject(events.length() - 1).getString("id")
                        content.removeAllViews()
                        review()
                    }
            }
        }
    }

    private fun mergeDialog(source: JSONObject, events: JSONArray) {
        val targets =
            (0 until events.length())
                .map { events.getJSONObject(it) }
                .filter {
                    it.getString("id") != source.getString("id") &&
                        it.getString("status") !in listOf("MERGED", "SPLIT")
                }
        if (targets.isEmpty()) return
        val names =
            targets
                .map {
                    "${it.getJSONObject("facts").optString("merchant")} ¥ ${money(it.getJSONObject("facts").getString("amount_minor"))} · ${it.getString("id").take(8)}"
                }
                .toTypedArray()
        AlertDialog.Builder(this)
            .setTitle("保留哪一笔的解释？")
            .setItems(names) { _, i ->
                val target = targets[i]
                AlertDialog.Builder(this)
                    .setMessage("将冲销当前这笔的账务，保留所选事件并汇集证据。确认它们是同一笔交易？")
                    .setNegativeButton("取消", null)
                    .setPositiveButton("确认合并") { _, _ ->
                        task {
                            api(
                                "merge",
                                JSONObject()
                                    .put("command_id", id())
                                    .put("source_id", source.getString("id"))
                                    .put("target_id", target.getString("id"))
                                    .put("source_revision", source.getLong("revision"))
                                    .put("target_revision", target.getLong("revision")),
                            )
                            reload()
                        }
                    }
                    .show()
            }
            .show()
    }

    private fun transferDialog(source: JSONObject, events: JSONArray) {
        val facts = source.getJSONObject("facts")
        val targets =
            (0 until events.length())
                .map { events.getJSONObject(it) }
                .filter {
                    val f = it.getJSONObject("facts")
                    it.optBoolean("posted") &&
                        f.optString("kind") == "TRANSFER_IN" &&
                        f.getString("amount_minor") == facts.getString("amount_minor") &&
                        f.optString("funding_account_id") != facts.optString("funding_account_id")
                }
        if (targets.isEmpty()) {
            AlertDialog.Builder(this)
                .setMessage("请先录入并确认另一账户的等额转入")
                .setPositiveButton("知道了", null)
                .show()
            return
        }
        AlertDialog.Builder(this)
            .setTitle("选择同一次转账的转入端")
            .setItems(
                targets
                    .map {
                        displayTime(it.getJSONObject("facts").optString("occurred_at")) +
                            " · ¥ " +
                            money(it.getJSONObject("facts").getString("amount_minor"))
                    }
                    .toTypedArray()
            ) { _, i ->
                val target = targets[i]
                task {
                    api(
                        "transfer",
                        JSONObject()
                            .put("command_id", id())
                            .put("out_id", source.getString("id"))
                            .put("in_id", target.getString("id"))
                            .put("out_revision", source.getLong("revision"))
                            .put("in_revision", target.getLong("revision")),
                    )
                    reload()
                }
            }
            .show()
    }

    private fun splitDialog(source: JSONObject) {
        val first =
            EditText(this).apply {
                hint = "第一笔金额（元）"
                inputType = InputType.TYPE_CLASS_NUMBER or InputType.TYPE_NUMBER_FLAG_DECIMAL
            }
        AlertDialog.Builder(this)
            .setTitle("原金额拆成两笔")
            .setMessage("两笔沿用原类型、账户和发生时间；保存后可以分别修订。")
            .setView(first)
            .setNegativeButton("取消", null)
            .setPositiveButton("确认拆分") { _, _ ->
                task {
                    val original = source.getJSONObject("facts")
                    val total = original.getString("amount_minor").toLong()
                    val n = minor(first.text.toString()).toLong()
                    require(n > 0 && n < total) { "拆分金额须小于原金额" }
                    val a = JSONObject(original.toString()).put("amount_minor", n.toString())
                    val b =
                        JSONObject(original.toString()).put("amount_minor", (total - n).toString())
                    api(
                        "split",
                        JSONObject()
                            .put("command_id", id())
                            .put("source_id", source.getString("id"))
                            .put("expected_revision", source.getLong("revision"))
                            .put("children", JSONArray().put(a).put(b)),
                    )
                    reload()
                }
            }
            .show()
    }
}
