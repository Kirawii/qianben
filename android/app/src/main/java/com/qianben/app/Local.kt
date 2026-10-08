package com.qianben.app

import android.app.Application
import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import androidx.room.*
import androidx.work.*
import java.net.HttpURLConnection
import java.net.URL
import java.security.KeyStore
import java.util.UUID
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec
import org.json.JSONObject

object Vault {
    @Synchronized
    private fun key(): SecretKey {
        val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (ks.getKey("qianben-private-v1", null) as? SecretKey)?.let {
            return it
        }
        return KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
            .apply {
                init(
                    KeyGenParameterSpec.Builder(
                            "qianben-private-v1",
                            KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
                        )
                        .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                        .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                        .build()
                )
            }
            .generateKey()
    }

    fun seal(value: String): String {
        val c = Cipher.getInstance("AES/GCM/NoPadding")
        c.init(Cipher.ENCRYPT_MODE, key())
        return Base64.encodeToString(
            c.iv + c.doFinal(value.toByteArray(Charsets.UTF_8)),
            Base64.NO_WRAP,
        )
    }

    fun open(value: String): String {
        val b = Base64.decode(value, Base64.NO_WRAP)
        val c = Cipher.getInstance("AES/GCM/NoPadding")
        c.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, b.copyOfRange(0, 12)))
        return String(c.doFinal(b.copyOfRange(12, b.size)), Charsets.UTF_8)
    }
}

object NotificationSources {
    val names =
        linkedMapOf(
            "com.tencent.mm" to "微信支付",
            "com.eg.android.AlipayGphone" to "支付宝",
            "com.unionpay" to "云闪付",
            "com.icbc" to "工商银行",
            "com.chinamworld.main" to "建设银行",
            "com.chinamworld.bocmbci" to "中国银行",
            "com.android.bankabc" to "农业银行",
            "cmb.pb" to "招商银行",
            "com.bankcomm.Bankcomm" to "交通银行",
            "com.psbc.mbank" to "邮储银行",
        )
}

class Settings(ctx: Context) {
    internal val context = ctx.applicationContext
    private val p = ctx.getSharedPreferences("settings", Context.MODE_PRIVATE)
    var url: String
        get() = p.getString("url", "http://127.0.0.1:8080")!!
        set(v) {
            p.edit().putString("url", v.trimEnd('/')).commit()
        }

    var token: String
        get() = p.getString("token", null)?.let { Vault.open(it) } ?: ""
        set(v) {
            p.edit().putString("token", Vault.seal(v)).commit()
        }

    var ledger: String
        get() = p.getString("ledger", "")!!
        set(v) {
            p.edit().putString("ledger", v).commit()
        }

    val device: String
        get() {
            val old = p.getString("device", null)
            if (old != null) return old
            val id = UUID.randomUUID().toString()
            p.edit().putString("device", id).commit()
            return id
        }

    var collecting: Boolean
        get() = p.getBoolean("collect", false)
        set(v) {
            p.edit().putBoolean("collect", v).commit()
        }

    var offline: Boolean
        get() = p.getBoolean("offline_cache", false)
        set(v) {
            p.edit().putBoolean("offline_cache", v).commit()
        }

    var cachedAt: String
        get() = p.getString("cached_at", "")!!
        set(v) {
            p.edit().putString("cached_at", v).commit()
        }

    var notificationSources: Set<String>
        get() = p.getStringSet("notification_sources", NotificationSources.names.keys)!!.toSet()
        set(v) {
            p.edit()
                .putStringSet("notification_sources", v.intersect(NotificationSources.names.keys))
                .commit()
        }

    var captureError: String
        get() = p.getString("capture_error", "")!!
        set(v) {
            p.edit().putString("capture_error", v).commit()
        }

    var lastCapture: String
        get() = p.getString("last_capture", "")!!
        set(v) {
            p.edit().putString("last_capture", v).commit()
        }
}

@Entity
data class Pending(
    @PrimaryKey val id: String,
    val ledger: String,
    val encrypted: String,
    val endpoint: String,
    val status: String = "QUEUED",
)

@Entity
data class Episode(
    @PrimaryKey val key: String,
    val episode: String,
    val hash: String,
    val sequence: Long,
)

@Entity
data class PendingCommand(
    @PrimaryKey val id: String,
    val path: String,
    val encrypted: String,
    val endpoint: String,
    val status: String = "QUEUED",
)

class ApiFailure(val status: Int, message: String) : IllegalStateException(message)

@Entity
data class CachedView(
    @PrimaryKey val key: String,
    val ledger: String,
    val encrypted: String,
    val capturedAt: String,
)

@Dao
interface QueueDao {
    @Insert(onConflict = OnConflictStrategy.REPLACE) fun cache(value: CachedView)

    @Query("SELECT * FROM CachedView WHERE `key`=:key") fun cache(key: String): CachedView?

    @Query("DELETE FROM CachedView WHERE `key` LIKE :scope || ':%'")
    fun invalidateViews(scope: String)

    @Query("DELETE FROM CachedView WHERE ledger=:ledger OR ledger=''")
    fun clearViews(ledger: String)

    @Insert(onConflict = OnConflictStrategy.IGNORE) fun command(item: PendingCommand)

    @Query("SELECT * FROM PendingCommand WHERE id=:id") fun command(id: String): PendingCommand?

    @Query("SELECT * FROM PendingCommand WHERE status='QUEUED' ORDER BY rowid LIMIT 50")
    fun commands(): List<PendingCommand>

    @Query("DELETE FROM PendingCommand WHERE id=:id") fun commandAck(id: String)

    @Query("UPDATE PendingCommand SET status='REJECTED' WHERE id=:id") fun commandReject(id: String)

    @Query("DELETE FROM PendingCommand WHERE path LIKE '/v1/ledgers/' || :ledger || '/%'")
    fun clearCommands(ledger: String)

    @Insert fun add(item: Pending)

    @Query("SELECT * FROM Pending WHERE status='QUEUED' ORDER BY rowid LIMIT 50")
    fun batch(): List<Pending>

    @Query("DELETE FROM Pending WHERE id=:id") fun ack(id: String)

    @Query("UPDATE Pending SET status='REJECTED' WHERE id=:id") fun reject(id: String)

    @Query("SELECT count(*) FROM Pending") fun count(): Int

    @Query("SELECT count(*) FROM PendingCommand") fun commandCount(): Int

    @Query("SELECT * FROM Episode WHERE `key`=:key") fun episode(key: String): Episode?

    @Insert(onConflict = OnConflictStrategy.REPLACE) fun episode(item: Episode)

    @Query("DELETE FROM Episode WHERE `key`=:key") fun removed(key: String)

    @Query("DELETE FROM Pending WHERE ledger=:ledger") fun clearPending(ledger: String)

    @Transaction
    fun clear(ledger: String) {
        clearPending(ledger)
        clearViews(ledger)
    }
}

@Database(
    entities = [Pending::class, Episode::class, PendingCommand::class, CachedView::class],
    version = 3,
    exportSchema = false,
)
abstract class LocalDB : RoomDatabase() {
    abstract fun queue(): QueueDao

    companion object {
        @Volatile private var instance: LocalDB? = null

        fun get(ctx: Context): LocalDB =
            instance
                ?: synchronized(this) {
                    instance
                        ?: Room.databaseBuilder(
                                ctx.applicationContext,
                                LocalDB::class.java,
                                "qianben-private.db",
                            )
                            .addMigrations(
                                object : androidx.room.migration.Migration(1, 2) {
                                    override fun migrate(
                                        db: androidx.sqlite.db.SupportSQLiteDatabase
                                    ) {
                                        db.execSQL(
                                            "CREATE TABLE IF NOT EXISTS PendingCommand (id TEXT NOT NULL PRIMARY KEY,path TEXT NOT NULL,encrypted TEXT NOT NULL,endpoint TEXT NOT NULL,status TEXT NOT NULL)"
                                        )
                                    }
                                },
                                object : androidx.room.migration.Migration(2, 3) {
                                    override fun migrate(
                                        db: androidx.sqlite.db.SupportSQLiteDatabase
                                    ) {
                                        db.execSQL(
                                            "CREATE TABLE IF NOT EXISTS CachedView (`key` TEXT NOT NULL PRIMARY KEY,ledger TEXT NOT NULL,encrypted TEXT NOT NULL,capturedAt TEXT NOT NULL)"
                                        )
                                    }
                                },
                            )
                            .build()
                            .also { instance = it }
                }
    }
}

object Api {
    internal fun cached(s: Settings, path: String): String? {
        val view = LocalDB.get(s.context).queue().cache(viewKey(s, path)) ?: return null
        s.offline = true
        s.cachedAt = view.capturedAt
        return Vault.open(view.encrypted)
    }

    private fun digest(value: String) =
        java.security.MessageDigest.getInstance("SHA-256")
            .digest(value.toByteArray(Charsets.UTF_8))
            .joinToString("") { "%02x".format(it) }

    internal fun viewKey(s: Settings, path: String) =
        digest(s.url + "\n" + s.token) + ":" + digest(path)

    fun command(ctx: Context, s: Settings, path: String, body: JSONObject): String {
        val q = LocalDB.get(ctx).queue()
        val id = body.getString("command_id")
        require(q.commands().none { it.id != id }) { "有一条操作尚未确认，请先重试上传并刷新" }
        q.command(PendingCommand(id, path, Vault.seal(body.toString()), s.url))
        val pending = q.command(id)!!
        require(
            pending.endpoint == s.url &&
                JSONObject(Vault.open(pending.encrypted)).toString() == body.toString()
        ) {
            "之前提交的内容尚未确认，请先重试上传"
        }
        UploadWorker.enqueue(ctx)
        return try {
            val result = request(s, path, body)
            q.commandAck(id)
            result
        } catch (e: ApiFailure) {
            if (e.status in 400..499) {
                q.commandAck(id)
            }
            throw e
        }
    }

    fun request(s: Settings, path: String, body: JSONObject? = null): String {
        val url = URL(s.url + path)
        require(url.protocol == "https" || (BuildConfig.DEBUG && url.protocol == "http")) {
            "服务地址需要 HTTPS"
        }
        val c = url.openConnection() as HttpURLConnection
        val cacheKey = viewKey(s, path)
        c.connectTimeout = 10000
        c.readTimeout = 20000
        c.instanceFollowRedirects = false
        c.requestMethod = if (body == null) "GET" else "POST"
        c.setRequestProperty("Authorization", "Bearer ${s.token}")
        c.setRequestProperty("Content-Type", "application/json")
        try {
            if (body != null) {
                c.doOutput = true
                c.outputStream.use { it.write(body.toString().toByteArray()) }
            }
            val code = c.responseCode
            val text =
                (if (code in 200..299) c.inputStream else c.errorStream)?.bufferedReader()?.use {
                    it.readText()
                } ?: ""
            if (code == 401 || code == 403)
                LocalDB.get(s.context).queue().invalidateViews(cacheKey.substringBefore(':'))
            if (code !in 200..299)
                throw ApiFailure(code, JSONObject(text).optString("message", "请求失败 $code"))
            if (body == null) {
                val ledger =
                    if (path.startsWith("/v1/ledgers/"))
                        path.removePrefix("/v1/ledgers/").substringBefore('/')
                    else ""
                LocalDB.get(s.context)
                    .queue()
                    .cache(
                        CachedView(
                            cacheKey,
                            ledger,
                            Vault.seal(text),
                            java.time.Instant.now().toString(),
                        )
                    )
            } else {
                val ledger =
                    if (path.startsWith("/v1/ledgers/"))
                        path.removePrefix("/v1/ledgers/").substringBefore('/')
                    else ""
                if (ledger.isNotBlank()) LocalDB.get(s.context).queue().clearViews(ledger)
            }
            return text
        } catch (e: java.io.IOException) {
            if (body == null) {
                val cached = LocalDB.get(s.context).queue().cache(cacheKey)
                if (cached != null) {
                    s.offline = true
                    s.cachedAt = cached.capturedAt
                    return Vault.open(cached.encrypted)
                }
            }
            throw e
        } finally {
            c.disconnect()
        }
    }
}

class UploadWorker(ctx: Context, p: WorkerParameters) : Worker(ctx, p) {
    override fun doWork(): Result {
        val s = Settings(applicationContext)
        if (s.token.isBlank()) return Result.retry()
        val q = LocalDB.get(applicationContext).queue()
        return try {
            for (cmd in q.commands()) {
                if (cmd.endpoint != s.url) {
                    q.commandReject(cmd.id)
                    continue
                }
                try {
                    Api.request(s, cmd.path, JSONObject(Vault.open(cmd.encrypted)))
                    q.commandAck(cmd.id)
                } catch (e: ApiFailure) {
                    if (e.status in 400..499) q.commandReject(cmd.id) else throw e
                }
            }
            val batch = q.batch()
            val sameServer =
                batch.filter {
                    if (it.endpoint != s.url) {
                        q.reject(it.id)
                        false
                    } else true
                }
            for (group in sameServer.groupBy { it.ledger }) {
                val items = org.json.JSONArray()
                group.value.forEach { items.put(JSONObject(Vault.open(it.encrypted))) }
                val raw =
                    Api.request(
                        s,
                        "/v1/ledgers/${group.key}/observations",
                        JSONObject().put("items", items),
                    )
                val acks = org.json.JSONArray(raw)
                for (i in 0 until acks.length()) {
                    val ack = acks.getJSONObject(i)
                    when (ack.getString("status")) {
                        "ACK",
                        "IGNORED" -> q.ack(ack.getString("delivery_id"))
                        "REJECTED" -> q.reject(ack.getString("delivery_id"))
                    }
                }
            }
            if (q.batch().isNotEmpty() || q.commands().isNotEmpty()) Result.retry()
            else Result.success()
        } catch (_: Exception) {
            Result.retry()
        }
    }

    companion object {
        fun enqueue(ctx: Context) {
            WorkManager.getInstance(ctx)
                .enqueueUniqueWork(
                    "evidence-upload",
                    ExistingWorkPolicy.APPEND_OR_REPLACE,
                    OneTimeWorkRequestBuilder<UploadWorker>()
                        .setConstraints(
                            Constraints.Builder()
                                .setRequiredNetworkType(NetworkType.CONNECTED)
                                .build()
                        )
                        .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 30, TimeUnit.SECONDS)
                        .build(),
                )
        }
    }
}

class QianBenApp : Application() {
    override fun onCreate() {
        super.onCreate()
        WorkManager.getInstance(this)
            .enqueueUniquePeriodicWork(
                "evidence-recovery",
                ExistingPeriodicWorkPolicy.KEEP,
                PeriodicWorkRequestBuilder<UploadWorker>(15, TimeUnit.MINUTES)
                    .setConstraints(
                        Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build()
                    )
                    .build(),
            )
        UploadWorker.enqueue(this)
    }

    companion object {
        val io = Executors.newSingleThreadExecutor()
    }
}
