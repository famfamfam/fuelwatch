package app.fuelwatch

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.hardware.Sensor
import android.hardware.SensorEvent
import android.hardware.SensorEventListener
import android.hardware.SensorManager
import android.net.TrafficStats
import android.os.Process
import android.os.SystemClock
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import java.io.File
import java.time.Instant
import java.time.LocalDate
import java.util.UUID

/** События телефона для /api/device/events (docs/07-api.md §1). */
object Events {
    fun json(type: String, data: JsonObject = JsonObject(emptyMap())): String =
        buildJsonObject {
            put("id", kotlinx.serialization.json.JsonPrimitive(UUID.randomUUID().toString()))
            put("type", kotlinx.serialization.json.JsonPrimitive(type))
            put("time", kotlinx.serialization.json.JsonPrimitive(Instant.now().toString()))
            put("data", data)
        }.toString()

    fun outboxDir(ctx: Context) = File(ctx.filesDir, "outbox")
}

/** После перезагрузки: только событие BOOT в очередь. Камеру запускает домашний экран (D-03). */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action == Intent.ACTION_BOOT_COMPLETED || intent.action == Intent.ACTION_LOCKED_BOOT_COMPLETED) {
            runCatching { Outbox(Events.outboxDir(context), Long.MAX_VALUE).addEvent(Events.json("BOOT")) }
        }
    }
}

/** Акселерометр → MotionLogic (наклон и тряска). */
class MotionMonitor(ctx: Context, private val logic: MotionLogic, private val onEvent: (MotionLogic.Event) -> Unit) :
    SensorEventListener {
    private val sm = ctx.getSystemService(SensorManager::class.java)
    private val sensor = sm?.getDefaultSensor(Sensor.TYPE_ACCELEROMETER)

    val available get() = sensor != null

    fun start() {
        sensor?.let { sm.registerListener(this, it, SensorManager.SENSOR_DELAY_GAME) }
    }

    fun stop() {
        sm?.unregisterListener(this)
    }

    override fun onSensorChanged(e: SensorEvent) {
        val ev = logic.onSample(e.values[0].toDouble(), e.values[1].toDouble(), e.values[2].toDouble(), SystemClock.elapsedRealtime())
        if (ev != null) onEvent(ev)
    }

    override fun onAccuracyChanged(sensor: Sensor?, accuracy: Int) {}
}

/**
 * Трафик приложения за сегодня (tx+rx). TrafficStats считает с загрузки телефона,
 * поэтому храним накопленное за день и учитываем перезагрузки.
 */
class TrafficCounter(ctx: Context) {
    private val sp = ctx.getSharedPreferences("traffic", Context.MODE_PRIVATE)
    private val uid = Process.myUid()

    fun today(): Long? {
        val tx = TrafficStats.getUidTxBytes(uid)
        val rx = TrafficStats.getUidRxBytes(uid)
        if (tx == TrafficStats.UNSUPPORTED.toLong() || rx == TrafficStats.UNSUPPORTED.toLong()) return null
        val raw = tx + rx
        val day = LocalDate.now().toString()
        var accum = sp.getLong("accum", 0)
        var segStart = sp.getLong("seg_start", raw)
        val last = sp.getLong("last", raw)
        if (sp.getString("day", day) != day) {
            accum = 0
            segStart = raw
        } else if (raw < last) {
            // Перезагрузка: счётчик системы начался заново.
            accum += last - segStart
            segStart = 0
        }
        sp.edit().putString("day", day).putLong("accum", accum).putLong("seg_start", segStart).putLong("last", raw).apply()
        return accum + (raw - segStart)
    }
}
