package com.aliothmoon.maafw.membership

import android.annotation.SuppressLint
import android.content.Context
import android.os.Build
import android.os.IBinder
import android.provider.Settings
import com.aliothmoon.maafw.BuildConfig
import com.aliothmoon.maafw.third.FakeContext
import org.json.JSONArray
import org.json.JSONObject
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

/** 会员最小身份快照：只读取 Android ID 与 SN，不记录原值文件或日志。 */
@SuppressLint("HardwareIds", "MissingPermission", "PrivateApi", "DiscouragedPrivateApi")
object AndroidIdentity {
    const val APP_ENV = "MDA_ANDROID_IDENTITY_APP_JSON"
    private val readers = Executors.newCachedThreadPool { runnable ->
        Thread(runnable, "MDAAndroidIdentity").apply { isDaemon = true }
    }

    private fun errorKind(error: Throwable): String = AndroidIdentityCompat.errorStatus(error)

    private fun read(rows: JSONArray, name: String, source: String, timeoutMs: Long = 1500, reader: () -> Any?) {
        val row = JSONObject().put("name", name).put("source", source)
        if (timeoutMs <= 0) {
            rows.put(row.put("status", "budget_exhausted").put("values", JSONArray()))
            return
        }
        val future = readers.submit<Any?> { reader() }
        try {
            val result = future.get(timeoutMs, TimeUnit.MILLISECONDS)
            val values = when (result) {
                null -> emptyList()
                is Array<*> -> result.filterNotNull().map { it.toString() }
                is Iterable<*> -> result.filterNotNull().map { it.toString() }
                else -> listOf(result.toString())
            }
            row.put("status", if (values.all { it.isBlank() }) "unavailable" else "read")
                .put("values", JSONArray(values))
        } catch (error: Exception) {
            row.put("status", errorKind(error)).put("values", JSONArray())
                .put("diagnostic", JSONObject(AndroidIdentityCompat.diagnostic(error)))
        } finally {
            future.cancel(true)
        }
        rows.put(row)
    }

    fun appEnv(context: Context): Map<String, String> {
        if (!BuildConfig.APPLICATION_ID.startsWith("com.aliothmoon.maafw.mda.")) return emptyMap()
        val rows = JSONArray()
        read(rows, "android_id.app", "Settings.Secure.ANDROID_ID / App ContentResolver") {
            Settings.Secure.getString(context.contentResolver, Settings.Secure.ANDROID_ID)
        }
        return mapOf(APP_ENV to JSONObject().put("rows", rows).toString())
    }

    private fun binder(name: String): IBinder? = Class.forName("android.os.ServiceManager")
        .getMethod("getService", String::class.java).invoke(null, name) as? IBinder

    private fun property(name: String): String = Class.forName("android.os.SystemProperties")
        .getMethod("get", String::class.java).invoke(null, name) as String

    fun privilegedJson(appJson: String?): String {
        val root = runCatching { JSONObject(appJson.orEmpty()) }.getOrElse { JSONObject() }
        val rows = root.optJSONArray("rows") ?: JSONArray().also { root.put("rows", it) }
        if (appJson.isNullOrBlank()) {
            rows.put(JSONObject().put("name", "android_id.app").put("source", "App environment missing")
                .put("status", "unavailable").put("values", JSONArray()))
        }
        root.put("schema_version", 1)
        val context = FakeContext.get()
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(12)
        fun field(name: String, source: String, reader: () -> Any?) {
            val left = TimeUnit.NANOSECONDS.toMillis(deadline - System.nanoTime()).coerceAtMost(1500)
            read(rows, name, source, left, reader)
        }
        field("android_id.privileged", "Settings.Secure.ANDROID_ID / FakeContext / privileged UID") {
            Settings.Secure.getString(context.contentResolver, Settings.Secure.ANDROID_ID)
        }
        field("serial.api", "Build.getSerial / privileged process") { Build.getSerial() }
        field("serial.policy", "IDeviceIdentifiersPolicyService.getSerialForPackage / com.android.shell") {
            val stub = Class.forName("android.os.IDeviceIdentifiersPolicyService\$Stub")
            val service = stub.getMethod("asInterface", IBinder::class.java).invoke(null, binder("device_identifiers"))
            service.javaClass.getMethod("getSerialForPackage", String::class.java, String::class.java)
                .apply { isAccessible = true }
                .invoke(service, "com.android.shell", null)
        }
        for (key in listOf("ro.serialno", "ro.boot.serialno")) {
            field("serial.$key", "SystemProperties.get($key)") { property(key) }
        }
        return root.toString()
    }
}
