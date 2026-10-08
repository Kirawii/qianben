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
            settings.token = oldToken
            settings.offline = oldOffline
            settings.cachedAt = oldTime
        }
    }
}
