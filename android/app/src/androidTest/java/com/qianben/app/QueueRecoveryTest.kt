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
        db.close()
        db = Room.databaseBuilder(ctx, LocalDB::class.java, name).build()
        assertEquals(payload, Vault.open(db.queue().batch().single().encrypted))
        assertEquals("command-1", db.queue().commands().single().id)
        assertEquals(1L, db.queue().episode("notification-key")!!.sequence)
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
        db.close()
    }
}
