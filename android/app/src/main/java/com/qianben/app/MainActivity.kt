package com.qianben.app

import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.content.res.ColorStateList
import android.content.res.Configuration
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
import android.graphics.drawable.RippleDrawable
import android.os.Bundle
import android.provider.Settings as AndroidSettings
import android.text.InputType
import android.view.Gravity
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
    private var reviewOnly = false
    private val night
        get() =
            resources.configuration.uiMode and Configuration.UI_MODE_NIGHT_MASK ==
                Configuration.UI_MODE_NIGHT_YES

    private val ink
        get() = Color.parseColor(if (night) "#E8EEEA" else "#213C33")

    private val muted
        get() = Color.parseColor(if (night) "#B1C1B7" else "#64756D")

    private val surface
        get() = Color.parseColor(if (night) "#1B2923" else "#FFFFFF")

    private val canvas
        get() = Color.parseColor(if (night) "#101B16" else "#F4F6F2")

    private val accent
        get() = Color.parseColor(if (night) "#A1D7BC" else "#28664C")

    private val line
        get() = Color.parseColor(if (night) "#35453C" else "#DDE5DD")

    private var screenGeneration = 0

    override fun onCreate(state: Bundle?) {
        super.onCreate(state)
        window.decorView.systemUiVisibility =
            if (night) 0
            else
                android.view.View.SYSTEM_UI_FLAG_LIGHT_STATUS_BAR or
                    android.view.View.SYSTEM_UI_FLAG_LIGHT_NAVIGATION_BAR
        window.statusBarColor = canvas
        window.navigationBarColor = surface
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

    private fun shape(color: Int = surface, radius: Int = 16, border: Boolean = false) =
        GradientDrawable().apply {
            setColor(color)
            cornerRadius = dp(radius).toFloat()
            if (border) setStroke(dp(1), line)
        }

    private fun textView(s: String, size: Float = 16f, color: Int = ink) =
        TextView(this).apply {
            text = s
            textSize = size
            setTextColor(color)
            setLineSpacing(dp(3).toFloat(), 1f)
            if (size >= 20f) typeface = Typeface.create("sans-serif-medium", Typeface.NORMAL)
        }

    private fun label(s: String, size: Float = 16f) {
        content.addView(
            textView(s, size, if (size < 16f) muted else ink).apply {
                setPadding(0, dp(7), 0, dp(7))
            }
        )
    }

    private fun card(block: () -> Unit) {
        val parent = content
        val group =
            LinearLayout(this).apply {
                orientation = LinearLayout.VERTICAL
                background = shape()
                setPadding(dp(20), dp(16), dp(20), dp(16))
            }
        parent.addView(group, LinearLayout.LayoutParams(-1, -2).apply { bottomMargin = dp(14) })
        content = group
        try {
            block()
        } finally {
            content = parent
        }
    }

    private fun button(s: String, action: () -> Unit) {
        content.addView(
            Button(this).apply {
                text = s
                stateListAnimator = null
                elevation = 0f
                isAllCaps = false
                textSize = 15f
                minHeight = dp(48)
                minimumHeight = dp(48)
                typeface = Typeface.create("sans-serif-medium", Typeface.NORMAL)
                val primary = s.startsWith("保存") || s.startsWith("确认保存") || s == "创建账本"
                val danger = s.contains("删除") || s.startsWith("撤销")
                setTextColor(
                    if (danger) Color.parseColor(if (night) "#FFB4A8" else "#A5352C")
                    else if (primary) canvas else accent
                )
                background =
                    RippleDrawable(
                        ColorStateList.valueOf(line),
                        shape(if (primary) accent else surface, 12, !primary),
                        null,
                    )
                layoutParams =
                    LinearLayout.LayoutParams(-1, -2).apply {
                        topMargin = dp(6)
                        bottomMargin = dp(6)
                    }
                setPadding(dp(16), dp(8), dp(16), dp(8))
                setOnClickListener { action() }
            }
        )
    }

    private fun field(title: String, value: String = "", numeric: Boolean = false): EditText {
        label(title, 13f)
        return EditText(this).apply {
            contentDescription = title
            setText(value)
            textSize = 16f
            setTextColor(ink)
            setHintTextColor(muted)
            background = shape(surface, 12, true)
            setPadding(dp(14), dp(12), dp(14), dp(12))
            minHeight = dp(52)
            layoutParams = LinearLayout.LayoutParams(-1, -2).apply { bottomMargin = dp(12) }
            setSingleLine()
            if (numeric)
                inputType = InputType.TYPE_CLASS_NUMBER or InputType.TYPE_NUMBER_FLAG_DECIMAL
            content.addView(this)
        }
    }

    private fun spinner(title: String, values: List<String>): Spinner {
        label(title, 13f)
        return Spinner(this).apply {
            background = shape(surface, 12, true)
            setPadding(dp(10), dp(8), dp(10), dp(8))
            minimumHeight = dp(52)
            layoutParams = LinearLayout.LayoutParams(-1, -2).apply { bottomMargin = dp(12) }
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
            "待确认"
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
        screenGeneration++
        val root =
            LinearLayout(this).apply {
                orientation = LinearLayout.VERTICAL
                setPadding(0, dp(24), 0, dp(8))
                setOnApplyWindowInsetsListener { view, insets ->
                    view.setPadding(
                        0,
                        insets.systemWindowInsetTop + dp(8),
                        0,
                        insets.systemWindowInsetBottom,
                    )
                    insets
                }
                setBackgroundColor(canvas)
            }
        root.addView(
            TextView(this).apply {
                val ledgerName =
                    (0 until ledgers.length())
                        .map { ledgers.getJSONObject(it) }
                        .firstOrNull { it.getString("id") == settings.ledger }
                        ?.optString("name")
                text = "钱本${ledgerName?.let { "  /  $it" } ?: ""}"
                textSize = 22f
                typeface = Typeface.create("sans-serif-medium", Typeface.NORMAL)
                setPadding(dp(24), dp(12), dp(24), dp(8))
                setTextColor(ink)
            }
        )
        val tabs = LinearLayout(this)
        tabs.setPadding(dp(12), dp(8), dp(12), dp(8))
        tabs.setBackgroundColor(surface)
        listOf("首页", "账户", "记账", "待确认", "设置").forEach { name ->
            tabs.addView(
                Button(this).apply {
                    text = name
                    stateListAnimator = null
                    elevation = 0f
                    textSize = 13f
                    isAllCaps = false
                    minWidth = 0
                    minimumWidth = 0
                    minHeight = 0
                    minimumHeight = 0
                    setPadding(0, dp(8), 0, dp(8))
                    setTextColor(if (page == name) accent else muted)
                    typeface = Typeface.create("sans-serif-medium", Typeface.NORMAL)
                    background =
                        RippleDrawable(
                            ColorStateList.valueOf(line),
                            shape(if (page == name) canvas else surface, 14),
                            null,
                        )
                    contentDescription = "$name${if(page==name) "，当前页面" else ""}"
                    setOnClickListener {
                        page = name
                        render()
                    }
                },
                LinearLayout.LayoutParams(0, dp(48), 1f),
            )
        }
        content =
            LinearLayout(this).apply {
                orientation = LinearLayout.VERTICAL
                setPadding(dp(20), dp(12), dp(20), dp(20))
            }
        root.addView(
            ScrollView(this).apply {
                addView(content)
                isFillViewport = true
                isVerticalScrollBarEnabled = false
            },
            LinearLayout.LayoutParams(-1, 0, 1f),
        )
        root.addView(tabs)
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
        button("选择通知来源（${settings.notificationSources.size} 个）") {
            val packages = NotificationSources.names.keys.toList()
            val checked = packages.map { it in settings.notificationSources }.toBooleanArray()
            AlertDialog.Builder(this)
                .setTitle("允许读取的财务通知来源")
                .setMultiChoiceItems(
                    packages.map { NotificationSources.names.getValue(it) }.toTypedArray(),
                    checked,
                ) { _, which, on ->
                    checked[which] = on
                }
                .setNegativeButton("取消", null)
                .setPositiveButton("保存") { _, _ ->
                    settings.notificationSources =
                        packages.filterIndexed { index, _ -> checked[index] }.toSet()
                    render()
                }
                .show()
        }
        label("开启后，所选来源的必要财务通知片段会加密保存在本机并上传当前服务解析。关闭来源仅停止新采集，已排队证据仍会上传。", 13f)
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
        val generation = screenGeneration
        val controls = LinearLayout(this).apply { gravity = Gravity.CENTER_VERTICAL }
        fun monthButton(title: String, step: Long) =
            Button(this).apply {
                text = title
                stateListAnimator = null
                elevation = 0f
                isAllCaps = false
                textSize = 14f
                setTextColor(accent)
                background = RippleDrawable(ColorStateList.valueOf(line), shape(surface, 12), null)
                minWidth = 0
                minimumWidth = 0
                setPadding(0, 0, 0, 0)
                contentDescription = if (step < 0) "上个月" else "下个月"
                setOnClickListener {
                    reportMonth = reportMonth.plusMonths(step)
                    render()
                }
            }
        controls.addView(monthButton("‹", -1), LinearLayout.LayoutParams(dp(48), dp(48)))
        controls.addView(
            textView("${reportMonth.year} 年 ${reportMonth.monthValue} 月", 18f).apply {
                gravity = Gravity.CENTER
            },
            LinearLayout.LayoutParams(0, -2, 1f),
        )
        controls.addView(monthButton("›", 1), LinearLayout.LayoutParams(dp(48), dp(48)))
        content.addView(controls, LinearLayout.LayoutParams(-1, -2).apply { bottomMargin = dp(18) })
        val loading = textView("正在读取账本…", 14f, muted)
        content.addView(loading)
        val zone = ZoneId.systemDefault()
        val selectedMonth = reportMonth
        val from = selectedMonth.atDay(1).atStartOfDay(zone).toInstant()
        val to = selectedMonth.plusMonths(1).atDay(1).atStartOfDay(zone).toInstant()
        task {
            val r =
                try {
                    JSONObject(
                        api(
                            "reports?from=${java.net.URLEncoder.encode(from.toString(),"UTF-8")}&to=${java.net.URLEncoder.encode(to.toString(),"UTF-8")}"
                        )
                    )
                } catch (e: Exception) {
                    runOnUiThread {
                        if (generation == screenGeneration) {
                            loading.text = "未能读取账本，请检查连接后重试。"
                            button("重试读取") { render() }
                        }
                    }
                    throw e
                }
            runOnUiThread {
                if (page != "首页" || reportMonth != selectedMonth || generation != screenGeneration)
                    return@runOnUiThread
                content.removeView(loading)
                card {
                    label("期末账面净资产", 14f)
                    label("¥ ${money(r.getString("net_worth_minor"))}", 34f)
                    val row = LinearLayout(this)
                    for ((title, key) in
                        listOf("资产" to "assets_minor", "负债" to "liabilities_minor")) {
                        row.addView(
                            LinearLayout(this).apply {
                                orientation = LinearLayout.VERTICAL
                                addView(textView(title, 13f, muted))
                                addView(
                                    textView("¥ ${money(r.getString(key))}", 18f).apply {
                                        setPadding(0, dp(8), 0, dp(12))
                                    }
                                )
                            },
                            LinearLayout.LayoutParams(0, -2, 1f),
                        )
                    }
                    content.addView(row)
                    label("包含应收、待查与在途，尚未与银行核对。", 12f)
                }
                val metrics = LinearLayout(this)
                for ((title, key) in
                    listOf("生活消费" to "consumption_minor", "现金变动" to "cash_change_minor")) {
                    metrics.addView(
                        LinearLayout(this).apply {
                            orientation = LinearLayout.VERTICAL
                            background = shape()
                            setPadding(dp(16), dp(18), dp(12), dp(18))
                            addView(textView(title, 13f, muted))
                            addView(
                                textView("¥ ${money(r.getString(key))}", 22f).apply {
                                    setPadding(0, dp(10), 0, 0)
                                }
                            )
                        },
                        LinearLayout.LayoutParams(0, -2, 1f).apply {
                            if (title == "生活消费") rightMargin = dp(12)
                        },
                    )
                }
                content.addView(
                    metrics,
                    LinearLayout.LayoutParams(-1, -2).apply { bottomMargin = dp(14) },
                )
                card {
                    label("会计收支", 18f)
                    label(
                        "收入  ¥ ${money(r.getString("income_minor"))}\n费用  ¥ ${money(r.getString("expense_minor"))}\n结余  ¥ ${money(r.getString("surplus_minor"))}",
                        16f,
                    )
                    button("查看现金流明细") {
                        AlertDialog.Builder(this)
                            .setTitle("本月现金流")
                            .setMessage(
                                "外部现金流：${money(r.getString("external_cash_minor"))}\n内部划转净额：${money(r.getString("internal_cash_minor"))}\n未分类现金净额：${money(r.getString("unresolved_cash_minor"))}"
                            )
                            .setPositiveButton("关闭", null)
                            .show()
                    }
                }
                card {
                    label("待处理", 18f)
                    label(
                        "${r.getLong("review_count")} 笔待确认  ·  待查 ¥ ${money(r.getString("unresolved_balance_minor"))}"
                    )
                    label(
                        "转账在途 ¥ ${money(r.getString("transfer_clearing_minor"))}  ·  ${r.getLong("unlinked_transfer_count")} 个未关联端",
                        13f,
                    )
                    button("查看待确认") {
                        page = "待确认"
                        render()
                    }
                }
                card {
                    label("消费分类", 18f)
                    val totals = r.getJSONArray("categories")
                    if (totals.length() == 0) label("这个月还没有消费记录。", 14f)
                    for (i in 0 until totals.length()) {
                        val total = totals.getJSONObject(i)
                        val row =
                            LinearLayout(this).apply {
                                gravity = Gravity.CENTER_VERTICAL
                                setPadding(0, dp(10), 0, dp(10))
                            }
                        row.addView(
                            textView(total.getString("category"), 15f),
                            LinearLayout.LayoutParams(0, -2, 1f),
                        )
                        row.addView(
                            textView("¥ ${money(total.getString("consumption_minor"))}", 15f)
                        )
                        content.addView(row)
                    }
                    label("退款按发生月份冲减，分类金额可以为负。", 12f)
                }
                label("账本版本 ${r.getLong("source_version")} · 未录入账户和期初会影响完整性。", 12f)
                button("刷新账本") { reload() }
            }
        }
    }

    private fun rulePage() {
        screenGeneration++
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
            card {
                label("${a.getString("name")}   ¥ ${money(a.getString("balance_minor"))}", 18f)
                label(if (a.getString("type") == "LIABILITY") "信用卡 · 欠款为正" else "实际账户 · 账面余额", 13f)
                if (!a.getBoolean("initialized"))
                    button("设置 ${a.getString("name")} 的期初") { opening(a) }
                else button("检查 ${a.getString("name")} 的实际余额") { balanceCheck(a) }
            }
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
        screenGeneration++
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
        screenGeneration++
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
        screenGeneration++
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
        fun showField(view: android.view.View, show: Boolean) {
            val visibility = if (show) android.view.View.VISIBLE else android.view.View.GONE
            view.visibility = visibility
            val parent = view.parent as LinearLayout
            val index = parent.indexOfChild(view)
            if (index > 0) parent.getChildAt(index - 1).visibility = visibility
        }
        fun updateFields() {
            val selected = kinds[kind.selectedItemPosition]
            showField(original, selected in listOf("REFUND", "ASSET_REFUND"))
            showField(asset, selected == "ASSET_PURCHASE")
            showField(card, selected == "CARD_REPAYMENT")
            historical.visibility =
                if (selected == "REFUND") android.view.View.VISIBLE else android.view.View.GONE
        }
        kind.onItemSelectedListener =
            object : AdapterView.OnItemSelectedListener {
                override fun onItemSelected(
                    parent: AdapterView<*>?,
                    view: android.view.View?,
                    position: Int,
                    itemId: Long,
                ) {
                    updateFields()
                }

                override fun onNothingSelected(parent: AdapterView<*>?) {}
            }
        updateFields()
        button("确认保存") {
            task {
                require(account.selectedItemPosition > 0) { "请选择真实付款或收款账户" }
                val selectedKind = kinds[kind.selectedItemPosition]
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
                        .put(
                            "asset_title",
                            if (selectedKind == "ASSET_PURCHASE") asset.text.toString() else "",
                        )
                        .put("note", note.text.toString())
                        .put(
                            "historical_original",
                            selectedKind == "REFUND" && historical.isChecked,
                        )
                if (
                    original.selectedItemPosition > 0 &&
                        selectedKind in listOf("REFUND", "ASSET_REFUND")
                )
                    f.put(
                        "original_event_id",
                        originals[original.selectedItemPosition - 1].getString("id"),
                    )
                if (card.selectedItemPosition > 0 && selectedKind == "CARD_REPAYMENT")
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
        val generation = screenGeneration
        label("事件与待确认", 22f)
        val filter =
            Switch(this).apply {
                text = "只看待确认"
                setTextColor(ink)
                isChecked = reviewOnly
                setPadding(0, dp(8), 0, dp(16))
                setOnCheckedChangeListener { _, on ->
                    reviewOnly = on
                    render()
                }
            }
        content.addView(filter)
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
                for (i in 0 until events.length()) if (
                    events.getJSONObject(i).getString("id") !in known
                )
                    eventCache.put(events.getJSONObject(i))
            }
            runOnUiThread {
                if (page != "待确认" || generation != screenGeneration) return@runOnUiThread
                val visible =
                    (0 until events.length())
                        .map { events.getJSONObject(it) }
                        .filter { !reviewOnly || it.getString("status") == "REVIEW_REQUIRED" }
                if (visible.isEmpty())
                    card {
                        label(if (reviewOnly) "当前页没有待确认事件" else "还没有事件记录", 18f)
                        label("可手动记账、导入账单，或查看更早记录。", 14f)
                    }
                for (ev in visible) card {
                    val f = ev.getJSONObject("facts")
                    label(f.optString("merchant").ifBlank { "未识别商户" }, 18f)
                    label("¥ ${money(f.getString("amount_minor"))}", 24f)
                    label(eventStatus(ev), 14f)
                    if (!f.isNull("occurred_at"))
                        label(displayTime(f.getString("occurred_at")), 12f)
                    if (ev.getString("status") !in listOf("MERGED", "SPLIT"))
                        button("查看并确认") { eventForm(ev) }
                    button("更多操作") {
                        val choices = mutableListOf("查看证据", "查看修订历史")
                        if (ev.getString("status") !in listOf("MERGED", "SPLIT")) {
                            choices.addAll(listOf("合并重复事件", "拆分为两笔"))
                            if (f.optString("kind") == "TRANSFER_OUT") choices.add("关联转入端")
                            if (f.optString("kind") in listOf("TRANSFER_OUT", "TRANSFER_IN"))
                                choices.add("解除转账关联")
                        }
                        AlertDialog.Builder(this)
                            .setTitle("事件操作")
                            .setItems(choices.toTypedArray()) { _, n ->
                                when (choices[n]) {
                                    "查看修订历史" -> historyPage(ev)
                                    "查看证据" ->
                                        task {
                                            val evidence =
                                                JSONArray(
                                                    api("evidence?event_id=${ev.getString("id")}")
                                                )
                                            runOnUiThread {
                                                AlertDialog.Builder(this)
                                                    .setTitle("原始证据")
                                                    .setMessage(evidence.toString(2))
                                                    .setPositiveButton("关闭", null)
                                                    .show()
                                            }
                                        }
                                    "合并重复事件" -> mergeDialog(ev, events)
                                    "拆分为两笔" -> splitDialog(ev)
                                    "关联转入端" -> transferDialog(ev, events)
                                    "解除转账关联" -> unlinkTransferDialog(ev)
                                }
                            }
                            .show()
                    }
                }
                if (events.length() == 500)
                    button("更早的事件") {
                        eventCursor = events.getJSONObject(events.length() - 1).getString("id")
                        render()
                    }
            }
        }
    }

    private fun unlinkTransferDialog(ev: JSONObject) {
        task {
            val relations = JSONArray(api("relations?event_id=${ev.getString("id")}"))
            val transfers =
                (0 until relations.length())
                    .map { relations.getJSONObject(it) }
                    .filter { it.getString("type") == "TRANSFER_LEG_OF" }
            runOnUiThread {
                if (transfers.isEmpty()) {
                    Toast.makeText(this, "尚未关联", Toast.LENGTH_SHORT).show()
                    return@runOnUiThread
                }
                AlertDialog.Builder(this)
                    .setTitle("解除转账关联？")
                    .setMessage("关联解除后可修订两端事件，既有分录保持不变。")
                    .setNegativeButton("取消", null)
                    .setPositiveButton("解除") { _, _ ->
                        task {
                            api(
                                "unlink-transfer",
                                JSONObject()
                                    .put("command_id", id())
                                    .put("relation_id", transfers[0].getString("id")),
                            )
                            reload()
                        }
                    }
                    .show()
            }
        }
    }

    private fun historyPage(ev: JSONObject) {
        screenGeneration++
        val generation = screenGeneration
        content.removeAllViews()
        label("修订历史", 22f)
        label("保留每次接受的事实与账务；最近 100 次修订。", 14f)
        button("返回事件列表") { render() }
        task {
            val versions = JSONArray(api("history?event_id=${ev.getString("id")}"))
            runOnUiThread {
                if (generation != screenGeneration || page != "待确认") return@runOnUiThread
                for (i in 0 until versions.length()) {
                    val v = versions.getJSONObject(i)
                    val f = v.getJSONObject("facts")
                    card {
                        label("第 ${v.getLong("revision")} 次修订", 18f)
                        label(displayTime(v.getString("created_at")), 12f)
                        label(
                            "${f.optString("merchant").ifBlank { "未识别商户" }} · ¥ ${money(f.getString("amount_minor"))}"
                        )
                        label(
                            "${f.optString("category").ifBlank { "未分类" }} · ${f.optString("note").ifBlank { "无备注" }}",
                            14f,
                        )
                        val journals = v.getJSONArray("journals")
                        if (journals.length() == 0) label("未生成正式分录", 14f)
                        for (n in 0 until journals.length()) {
                            val j = journals.getJSONObject(n)
                            label(
                                if (j.getString("kind") == "REVERSAL") "冲销旧账务"
                                else if (!j.isNull("reversed_by")) "原账务 · 后续已冲销" else "正式账务",
                                14f,
                            )
                            val entries = j.getJSONArray("entries")
                            for (k in 0 until entries.length()) {
                                val entry = entries.getJSONObject(k)
                                val debit = entry.getString("debit_minor")
                                label(
                                    "${entry.getString("account")}  ${if(debit!="0") "借 ${money(debit)}" else "贷 ${money(entry.getString("credit_minor"))}"}",
                                    13f,
                                )
                            }
                        }
                        label(
                            "账本版本 ${v.getLong("source_version")} · ${v.getJSONArray("evidence_ids").length()} 份证据",
                            12f,
                        )
                    }
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
