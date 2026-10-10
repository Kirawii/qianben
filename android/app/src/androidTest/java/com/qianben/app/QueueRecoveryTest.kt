package com.qianben.app

import androidx.room.Room
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import java.util.UUID
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class QueueRecoveryTest {
    @Test
    fun packagedGoEngineCreatesAndPostsWithoutServerOrToken() {
        val ctx = InstrumentationRegistry.getInstrumentation().targetContext
        val settings = Settings(ctx)
        val oldMode = settings.localMode
        val ledger = UUID.randomUUID().toString()
        val cut = java.time.Instant.now().minusSeconds(3600).toString()
        try {
            settings.localMode = true
            // No URL, socket, authentication token or PostgreSQL is used here.
            val root = "/v1/ledgers/$ledger"
            val created =
                org.json.JSONObject(
                    Api.request(
                        settings,
                        "/v1/ledgers",
                        org.json
                            .JSONObject()
                            .put("id", ledger)
                            .put("name", "独立使用合成验收")
                            .put("cutover_time", cut),
                    )
                )
            assertEquals(ledger, created.getString("id"))
            val account =
                org.json.JSONObject(
                    Api.request(
                        settings,
                        "$root/accounts",
                        org.json
                            .JSONObject()
                            .put("command_id", UUID.randomUUID().toString())
                            .put("name", "合成银行卡")
                            .put("type", "ASSET")
                            .put("cash", true),
                    )
                )
            val bank = account.getString("id")
            Api.request(
                settings,
                "$root/opening",
                org.json
                    .JSONObject()
                    .put("command_id", UUID.randomUUID().toString())
                    .put("account_id", bank)
                    .put("amount_minor", "10000")
                    .put("as_of", cut)
                    .put("meaning", "BALANCE")
                    .put("expected_revision", 0),
            )
            val command = UUID.randomUUID().toString()
            val body =
                org.json
                    .JSONObject()
                    .put("command_id", command)
                    .put("event_id", UUID.randomUUID().toString())
                    .put("expected_revision", 0)
                    .put(
                        "facts",
                        org.json
                            .JSONObject()
                            .put("kind", "EXPENSE")
                            .put("amount_minor", "1234")
                            .put("currency", "CNY")
                            .put("funding_account_id", bank)
                            .put("occurred_at", java.time.Instant.now().minusSeconds(60).toString())
                            .put("time_precision", "EXACT")
                            .put("merchant", "合成商户")
                            .put("category", "餐饮"),
                    )
            val event = org.json.JSONObject(Api.request(settings, "$root/events", body))
            assertTrue(event.getBoolean("posted"))
            assertEquals(event.toString(), Api.request(settings, "$root/events", body))
            val accounts = org.json.JSONArray(Api.request(settings, "$root/accounts"))
            val actual =
                (0 until accounts.length())
                    .map { accounts.getJSONObject(it) }
                    .single { it.getString("id") == bank }
            assertEquals("8766", actual.getString("balance_minor"))
            val report =
                org.json.JSONObject(
                    Api.request(
                        settings,
                        "$root/reports?from=$cut&to=${java.time.Instant.now().plusSeconds(60)}",
                    )
                )
            assertEquals("1234", report.getString("consumption_minor"))
            assertEquals("8766", report.getString("net_worth_minor"))
        } finally {
            Api.request(
                settings,
                "/v1/ledgers/$ledger/delete",
                org.json.JSONObject().put("confirm_name", "独立使用合成验收"),
            )
            settings.localMode = oldMode
        }
    }

    @Test
    fun rawRetentionMigrationExpiryAndDeletionPreserveQueueAndOtherLedgers() {
        val ctx = InstrumentationRegistry.getInstrumentation().targetContext
        val name = "qa-raw-${UUID.randomUUID()}.db"
        val old = ctx.openOrCreateDatabase(name, 0, null)
        old.execSQL(
            "CREATE TABLE Pending (id TEXT NOT NULL PRIMARY KEY,ledger TEXT NOT NULL,encrypted TEXT NOT NULL,endpoint TEXT NOT NULL,status TEXT NOT NULL)"
        )
        old.execSQL(
            "CREATE TABLE Episode (`key` TEXT NOT NULL PRIMARY KEY,episode TEXT NOT NULL,hash TEXT NOT NULL,sequence INTEGER NOT NULL)"
        )
        old.execSQL(
            "CREATE TABLE PendingCommand (id TEXT NOT NULL PRIMARY KEY,path TEXT NOT NULL,encrypted TEXT NOT NULL,endpoint TEXT NOT NULL,status TEXT NOT NULL)"
        )
        old.execSQL(
            "CREATE TABLE CachedView (`key` TEXT NOT NULL PRIMARY KEY,ledger TEXT NOT NULL,encrypted TEXT NOT NULL,capturedAt TEXT NOT NULL)"
        )
        val sealed = Vault.seal("支付成功 支付30.50元 合成商户")
        old.execSQL(
            "INSERT INTO Pending VALUES ('queued','ledger-a',?,'https://example.invalid','QUEUED')",
            arrayOf(sealed),
        )
        old.version = 3
        old.close()
        var db =
            Room.databaseBuilder(ctx, LocalDB::class.java, name)
                .addMigrations(LocalDB.RAW_MIGRATION)
                .build()
        try {
            var q = db.queue()
            assertEquals("queued", q.batch().single().id)
            assertFalse(sealed.contains("商户"))
            q.raw(LocalRawNotification("expired", "ledger-a", "identity-a", sealed, 0, 100))
            q.raw(LocalRawNotification("alive", "ledger-a", "identity-a", sealed, 100, 300))
            q.raw(LocalRawNotification("other", "ledger-b", "identity-b", sealed, 100, 500))
            assertTrue(q.raw("ledger-a", "identity-b", 101).isEmpty())
            assertEquals("alive", q.raw("ledger-a", "identity-a", 100).single().id)
            q.expireRaw(100)
            db.close()
            db = Room.databaseBuilder(ctx, LocalDB::class.java, name).build()
            q = db.queue()
            assertEquals(
                "支付成功 支付30.50元 合成商户",
                Vault.open(q.raw("ledger-a", "identity-a", 101).single().encrypted),
            )
            q.shortenRaw(150)
            assertEquals(250L, q.raw("ledger-a", "identity-a", 101).single().expiresAt)
            q.shortenRaw(1000)
            assertEquals(250L, q.raw("ledger-a", "identity-a", 101).single().expiresAt)
            q.ack("queued")
            assertEquals(1, q.raw("ledger-a", "identity-a", 101).size)
            q.add(Pending("still-queued", "ledger-b", sealed, "https://example.invalid"))
            q.clear("ledger-a")
            assertTrue(q.raw("ledger-a", "identity-a", 101).isEmpty())
            assertEquals(1, q.raw("ledger-b", "identity-b", 101).size)
            q.clearRaw()
            assertTrue(q.raw("ledger-b", "identity-b", 101).isEmpty())
            assertEquals("still-queued", q.batch().single().id)
        } finally {
            db.close()
            ctx.deleteDatabase(name)
        }
    }

    @Test
    fun localNotificationSummaryDoesNotLeakOrInventEvidence() {
        val hash = "a".repeat(64)
        val result = LocalNotificationParser.summarize("微信支付", "支付成功 支付30.50元 商户秘密名称", "", hash)
        assertEquals("3050", result.getString("amount_minor"))
        assertEquals("CASH_OUT", result.getString("kind"))
        assertFalse(result.toString().contains("秘密"))
        assertFalse(result.has("occurred_at"))
        assertFalse(result.has("funding_account_id"))
        val ambiguous = LocalNotificationParser.summarize("银行", "账户支出人民币30元 余额人民币100元", "", hash)
        assertEquals("0", ambiguous.getString("amount_minor"))
        assertEquals("UNKNOWN", ambiguous.getString("kind"))
        val foreign = LocalNotificationParser.summarize("银行", "支付成功 支付10美元", "", hash)
        assertEquals("UNKNOWN", foreign.getString("currency"))
        val absent = LocalNotificationParser.summarize("银行", "支付成功 支付10", "", hash)
        assertEquals("UNKNOWN", absent.getString("currency"))
        val invalid = LocalNotificationParser.summarize("银行", "支付成功 支付1.234元", "", hash)
        assertEquals("0", invalid.getString("amount_minor"))
        val big = LocalNotificationParser.summarize("银行", "账户支出人民币10元", "账户支出人民币20元", hash)
        assertEquals("2000", big.getString("amount_minor"))
    }

    @Test
    fun sourceSelectionPersistsAndRejectsUnknownPackages() {
        val ctx = InstrumentationRegistry.getInstrumentation().targetContext
        val settings = Settings(ctx)
        val previous = settings.notificationSources
        try {
            settings.notificationSources = setOf("com.tencent.mm", "not.a.financial.source")
            assertEquals(setOf("com.tencent.mm"), Settings(ctx).notificationSources)
            settings.notificationSources = emptySet()
            assertTrue(Settings(ctx).notificationSources.isEmpty())
        } finally {
            settings.notificationSources = previous
        }
    }

    @Test
    fun encryptedQueueSurvivesDatabaseReopen() {
        val ctx = InstrumentationRegistry.getInstrumentation().targetContext
        val name = "qa-${UUID.randomUUID()}.db"
        val payload = "{\"amount_minor\":\"9007199254740993\"}"
        val ciphertext = Vault.seal(payload)
        assertFalse(ciphertext.contains("amount_minor"))
        var db = Room.databaseBuilder(ctx, LocalDB::class.java, name).build()
        db.queue().add(Pending("delivery-1", "ledger-1", ciphertext, "https://example.invalid"))
        db.queue()
            .command(
                PendingCommand(
                    "command-1",
                    "/v1/ledgers/ledger-1/events",
                    Vault.seal("{}"),
                    "https://example.invalid",
                )
            )
        db.queue().episode(Episode("notification-key", "episode-1", "hash-A", 1))
        db.queue().cache(CachedView("view-1", "ledger-1", ciphertext, "2026-10-08T00:00:00Z"))
        db.close()
        db = Room.databaseBuilder(ctx, LocalDB::class.java, name).build()
        assertEquals(payload, Vault.open(db.queue().batch().single().encrypted))
        assertEquals("command-1", db.queue().commands().single().id)
        assertEquals(1L, db.queue().episode("notification-key")!!.sequence)
        assertEquals(payload, Vault.open(db.queue().cache("view-1")!!.encrypted))
        db.queue().ack("delivery-1")
        assertEquals(0, db.queue().count())
        db.close()
        ctx.deleteDatabase(name)
    }

    @Test
    fun ledgerDeletionKeepsOtherLedgerQueue() {
        val ctx = InstrumentationRegistry.getInstrumentation().targetContext
        val db = Room.inMemoryDatabaseBuilder(ctx, LocalDB::class.java).build()
        val q = db.queue()
        q.add(Pending("a", "ledger-a", Vault.seal("{}"), "https://example.invalid"))
        q.add(Pending("b", "ledger-b", Vault.seal("{}"), "https://example.invalid"))
        q.cache(CachedView("a", "ledger-a", Vault.seal("{}"), "2026-10-08T00:00:00Z"))
        q.cache(CachedView("b", "ledger-b", Vault.seal("{}"), "2026-10-08T00:00:00Z"))
        q.command(
            PendingCommand(
                "a",
                "/v1/ledgers/ledger-a/events",
                Vault.seal("{}"),
                "https://example.invalid",
            )
        )
        q.command(
            PendingCommand(
                "b",
                "/v1/ledgers/ledger-b/events",
                Vault.seal("{}"),
                "https://example.invalid",
            )
        )
        q.clear("ledger-a")
        q.clearCommands("ledger-a")
        assertEquals("b", q.batch().single().id)
        assertEquals("b", q.commands().single().id)
        assertNull(q.cache("a"))
        assertNotNull(q.cache("b"))
        db.close()
    }

    @Test
    fun offlineViewsNeverBypassAuthorizationOrCrossTokens() {
        val ctx = InstrumentationRegistry.getInstrumentation().targetContext
        val settings = Settings(ctx)
        val oldUrl = settings.url
        val oldMode = settings.localMode
        val oldToken = settings.token
        val oldOffline = settings.offline
        val oldTime = settings.cachedAt
        val ledger = UUID.randomUUID().toString()
        val server = java.net.ServerSocket(0)
        val fixture = "{\"net_worth_minor\":\"9007199254740993\"}"
        val thread =
            kotlin.concurrent.thread {
                for (status in listOf(200, 401)) {
                    server.accept().use { socket ->
                        val reader = socket.getInputStream().bufferedReader()
                        while (!reader.readLine().isNullOrEmpty()) {}
                        val response =
                            if (status == 200) fixture else "{\"message\":\"unauthorized\"}"
                        socket
                            .getOutputStream()
                            .write(
                                ("HTTP/1.1 $status QA\r\nContent-Type: application/json\r\nContent-Length: ${response.toByteArray().size}\r\nConnection: close\r\n\r\n$response")
                                    .toByteArray()
                            )
                    }
                }
                server.close()
            }
        try {
            settings.localMode = false
            settings.url = "http://127.0.0.1:${server.localPort}"
            settings.token = "qa-cache-token"
            settings.offline = false
            val path = "/v1/ledgers/$ledger/reports?from=2026-10-01&to=2026-11-01"
            assertEquals(fixture, Api.request(settings, path))
            try {
                Api.request(settings, path)
                fail("401 returned cached financial data")
            } catch (e: ApiFailure) {
                assertEquals(401, e.status)
            }
            thread.join(5000)
            assertFalse(thread.isAlive)
            try {
                Api.request(settings, path)
                fail("known invalid token retained cached financial data")
            } catch (_: java.io.IOException) {}
            settings.token = "qa-cache-valid"
            LocalDB.get(ctx)
                .queue()
                .cache(
                    CachedView(
                        Api.viewKey(settings, path),
                        ledger,
                        Vault.seal(fixture),
                        "2026-10-08T00:00:00Z",
                    )
                )
            assertEquals(fixture, Api.request(settings, path))
            assertTrue(settings.offline)
            assertTrue(settings.cachedAt.isNotBlank())
            settings.token = "another-cache-token"
            try {
                Api.request(settings, path)
                fail("cache crossed token identity")
            } catch (_: java.io.IOException) {}
        } finally {
            server.close()
            LocalDB.get(ctx).queue().clearViews(ledger)
            settings.url = oldUrl
            settings.localMode = oldMode
            settings.token = oldToken
            settings.offline = oldOffline
            settings.cachedAt = oldTime
        }
    }
}
