package app.fuelwatch

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.Process
import android.os.SystemClock
import android.util.Log
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import androidx.lifecycle.LifecycleService
import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonPrimitive
import java.time.Instant
import java.util.UUID
import kotlin.math.min

/**
 * Foreground-сервис типа camera (docs/04-android-app.md §2–3, §6–8): камера, датчики, heartbeat, команды,
 * очередь на диске (события и кадры доходят после отсутствия связи), самовосстановление камеры.
 */
class MonitorService : LifecycleService() {
    private val app get() = FwApp.of(this)
    private lateinit var sampler: CameraSampler
    private lateinit var outbox: Outbox
    private lateinit var motion: MotionLogic
    private var motionMonitor: MotionMonitor? = null
    private lateinit var traffic: TrafficCounter
    private var pairing: Prefs.Pairing? = null
    private var config = DeviceConfig.EMPTY
    private var capsSent = false
    private var disabled = false
    private var lastError: String? = null
    private var started = false

    private val wakeSender = Channel<Unit>(Channel.CONFLATED)
    private val resultsLock = Mutex()
    private val results = mutableListOf<CommandDone>()

    private val powerReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            val type = if (intent.action == Intent.ACTION_POWER_CONNECTED) "POWER_ON" else "POWER_OFF"
            val bat = DeviceStatus.battery(context).percent
            addEvent(type, JsonObject(if (bat != null) mapOf("battery" to JsonPrimitive(bat)) else emptyMap()))
        }
    }

    override fun onCreate() {
        super.onCreate()
        startInForeground()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        super.onStartCommand(intent, flags, startId)
        if (!started) {
            started = true
            lifecycleScope.launch { run() }
        }
        return START_STICKY
    }

    override fun onDestroy() {
        motionMonitor?.stop()
        runCatching { unregisterReceiver(powerReceiver) }
        if (::sampler.isInitialized) sampler.stop()
        super.onDestroy()
    }

    private fun startInForeground() {
        val nm = getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(
            NotificationChannel(CHANNEL, getString(R.string.channel_monitor), NotificationManager.IMPORTANCE_LOW),
        )
        val open = PendingIntent.getActivity(
            this, 0, Intent(this, HomeActivity::class.java), PendingIntent.FLAG_IMMUTABLE,
        )
        val n = Notification.Builder(this, CHANNEL)
            .setSmallIcon(android.R.drawable.ic_menu_camera)
            .setContentTitle(getString(R.string.app_name))
            .setContentText(getString(R.string.notif_monitor))
            .setOngoing(true)
            .setContentIntent(open)
            .build()
        val type = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) ServiceInfo.FOREGROUND_SERVICE_TYPE_CAMERA else 0
        ServiceCompat.startForeground(this, NOTIFICATION_ID, n, type)
    }

    private suspend fun run() {
        val p = app.prefs.pairing()
        if (p == null) {
            app.state.value = app.state.value.copy(needsPairing = true, status = "нужна привязка")
            stopSelf()
            return
        }
        pairing = p
        config = app.prefs.config() ?: DeviceConfig.EMPTY
        resultsLock.withLock { results.addAll(app.prefs.pendingResults()) }
        outbox = Outbox(Events.outboxDir(this), config.int("queue.max_mb") * MB)
        traffic = TrafficCounter(this)
        publishState()

        motion = MotionLogic(config.double("health.tilt_alert_deg"), config.double("health.shake_ms2"))
        // Нет сохранённого положения (первый запуск) — запомнить текущее.
        app.prefs.motionBaseline()?.let { motion.restoreBaseline(it) } ?: rearmMotion()
        motionMonitor = MotionMonitor(this, motion) { ev -> onMoved(ev) }.also { it.start() }

        ContextCompat.registerReceiver(
            this, powerReceiver,
            IntentFilter().apply {
                addAction(Intent.ACTION_POWER_CONNECTED)
                addAction(Intent.ACTION_POWER_DISCONNECTED)
            },
            ContextCompat.RECEIVER_NOT_EXPORTED,
        )

        sampler = CameraSampler(this, this) { cap -> enqueueFrame(cap) }
        sampler.liveUntilMs = parseEpochMs(config.liveUntil)
        try {
            sampler.start(CameraSampler.Params.from(config))
        } catch (e: Exception) {
            lastError = "camera: ${e.message}"
            Log.e(TAG, "camera start failed", e)
        }
        lifecycleScope.launch { senderLoop() }
        lifecycleScope.launch { heartbeatLoop() }
        lifecycleScope.launch { cameraWatchdog() }
    }

    private fun publishState(status: String? = null) {
        val s = status ?: when {
            disabled -> "устройство отключено в панели"
            else -> "${pairing?.deviceId ?: ""} · ${modeLabel(config.mode)}"
        }
        app.state.value = app.state.value.copy(status = s, keepScreenOn = config.bool("capture.keep_screen_on"))
    }

    private fun modeLabel(mode: String) = when (mode) {
        "armed" -> "мониторинг"
        "paused" -> "пауза"
        else -> "настройка"
    }

    // --- датчики и события --------------------------------------------------------------

    private fun addEvent(type: String, data: JsonObject = JsonObject(emptyMap())) {
        if (!::outbox.isInitialized) return
        runCatching { outbox.addEvent(Events.json(type, data)) }.onFailure { Log.e(TAG, "event $type", it) }
        wakeSender.trySend(Unit)
    }

    /** Телефон сдвинули: событие и сразу свежий снимок — оператор увидит, что в кадре. */
    private fun onMoved(ev: MotionLogic.Event) {
        val data = when (ev) {
            is MotionLogic.Event.Tilt -> mapOf("tilt_deg" to JsonPrimitive(ev.deg))
            is MotionLogic.Event.Shake -> mapOf("shake" to JsonPrimitive(ev.ms2))
        }
        Log.w(TAG, "MOVED $data")
        addEvent("MOVED", JsonObject(data))
        if (::sampler.isInitialized) sampler.request("snapshot", null)
    }

    private fun rearmMotion() {
        motion.rearm(SystemClock.elapsedRealtime())
        // Базовый вектор готов через MotionLogic.BASELINE_MS — сохраним его тогда.
        lifecycleScope.launch {
            delay(MotionLogic.BASELINE_MS + 1_000)
            motion.baselineVector?.let { app.prefs.saveMotionBaseline(it) }
        }
    }

    // --- камера ------------------------------------------------------------------------

    /** Нет кадров дольше health.camera_stale_s/2 — перезапуск камеры; после 3 неудач — заново через провайдер. */
    private suspend fun cameraWatchdog() {
        var restarts = 0
        while (lifecycleScope.isActive) {
            delay(WATCHDOG_MS)
            if (!::sampler.isInitialized) continue
            val staleMs = config.int("health.camera_stale_s") * 1000L / 2
            val age = SystemClock.elapsedRealtime() - sampler.lastFrameAt
            if (sampler.lastFrameAt == 0L || age < staleMs) {
                if (restarts > 0 && age < staleMs) {
                    restarts = 0
                    lastError = null
                }
                continue
            }
            restarts++
            Log.w(TAG, "camera stale ${age / 1000}s, restart #$restarts")
            try {
                if (restarts <= CAMERA_SOFT_RESTARTS) sampler.restart() else sampler.recreate()
            } catch (e: Exception) {
                Log.e(TAG, "camera restart failed", e)
            }
            if (restarts > CAMERA_SOFT_RESTARTS) {
                lastError = "camera: нет кадров ${age / 1000} с, перезапуски не помогли"
                addEvent("CAMERA_ERROR", JsonObject(mapOf("age_s" to JsonPrimitive(age / 1000))))
            }
        }
    }

    // --- heartbeat и команды -------------------------------------------------------------

    private suspend fun heartbeatLoop() {
        while (lifecycleScope.isActive) {
            try {
                heartbeatOnce()
            } catch (e: CancellationException) {
                throw e
            } catch (e: ApiException) {
                Log.w(TAG, "heartbeat: ${e.code} ${e.message}")
                when {
                    e.code == 401 -> {
                        app.prefs.clearToken()
                        app.state.value = app.state.value.copy(needsPairing = true, status = "нужна привязка")
                        stopSelf()
                        return
                    }
                    e.code == 403 && e.error == "device_disabled" -> {
                        disabled = true
                        publishState()
                    }
                }
            } catch (e: Exception) {
                Log.w(TAG, "heartbeat failed: ${e.message}")
                publishState("нет связи с сервером")
            }
            val interval = if (::sampler.isInitialized && sampler.liveActive) "live.heartbeat_interval_s" else "net.heartbeat_interval_s"
            delay(config.int(interval) * 1000L)
        }
    }

    private suspend fun heartbeatOnce() {
        val p = pairing ?: return
        val done = resultsLock.withLock { results.toList() }
        val caps = if (!capsSent) sampler.caps else null
        val bat = DeviceStatus.battery(this)
        val lastFrame = sampler.lastFrameAt
        val req = HeartbeatRequest(
            time = Instant.now().toString(),
            uptimeS = (SystemClock.elapsedRealtime() - Process.getStartElapsedRealtime()) / 1000,
            appVersion = BuildConfig.VERSION_NAME,
            mode = config.mode,
            configVersion = config.configVersion,
            battery = bat.percent,
            charging = bat.charging,
            batteryTempC = bat.tempC,
            thermalStatus = DeviceStatus.thermalStatus(this),
            lastFrameAgeS = if (lastFrame > 0) (SystemClock.elapsedRealtime() - lastFrame) / 1000.0 else null,
            tiltDeg = motion.tiltDeg?.let { Math.round(it * 10) / 10.0 },
            network = DeviceStatus.network(this),
            txBytesToday = traffic.today(),
            queueItems = outbox.count(),
            queueMb = Math.round(outbox.sizeBytes() / 1e5) / 10.0,
            live = sampler.liveActive,
            lastError = lastError,
            cameraCaps = caps,
            commandsDone = done,
        )
        val resp = Api.heartbeat(p, req)
        if (disabled) wakeSender.trySend(Unit) // снова включили — отправить накопленное
        disabled = false
        if (caps != null) capsSent = true
        if (done.isNotEmpty()) {
            resultsLock.withLock { results.removeAll(done.toSet()) }
            savePendingResults()
        }
        resp.config?.let { applyConfig(it) }
        requestExtraFrames(resp.extraFrames)
        for (c in resp.commands) runCommand(c)
        publishState()
    }

    private suspend fun applyConfig(raw: JsonObject) {
        val next = DeviceConfig.parse(raw)
        config = next
        app.prefs.saveConfig(raw)
        outbox.maxBytes = next.int("queue.max_mb") * MB
        motion.tiltAlertDeg = next.double("health.tilt_alert_deg")
        motion.shakeMs2 = next.double("health.shake_ms2")
        if (::sampler.isInitialized) {
            sampler.apply(CameraSampler.Params.from(next))
            sampler.liveUntilMs = parseEpochMs(next.liveUntil)
        }
        Log.i(TAG, "config v${next.configVersion} applied, mode=${next.mode}")
    }

    private suspend fun report(id: String, ok: Boolean, error: String? = null) {
        app.prefs.addDoneId(id)
        resultsLock.withLock { results.add(CommandDone(id, ok, error)) }
        savePendingResults()
    }

    private suspend fun savePendingResults() {
        val snapshot = resultsLock.withLock { results.toList() }
        app.prefs.savePendingResults(snapshot)
    }

    private suspend fun runCommand(c: Command) {
        val alreadyDone = c.id in app.prefs.doneIds()
        val pending = resultsLock.withLock { results.any { it.id == c.id } }
        if (alreadyDone) {
            // Выполнена, но результат не дошёл до сервера — сообщить ещё раз, не выполняя.
            if (!pending) resultsLock.withLock { results.add(CommandDone(c.id, true)) }
            return
        }
        if (runCatching { Instant.parse(c.expiresAt).isBefore(Instant.now()) }.getOrDefault(false)) {
            report(c.id, false, "expired")
            return
        }
        Log.i(TAG, "command ${c.type} ${c.id}")
        when (c.type) {
            // Результат снимка — после отправки кадра (senderLoop).
            "snapshot" -> sampler.request("snapshot", c.id)
            "capture_reference" -> {
                rearmMotion()
                sampler.request("reference", c.id)
            }
            // Режим хранит сервер: он уже пришёл в конфиге этого же ответа (конфиг применяется до команд).
            "arm" -> {
                rearmMotion()
                report(c.id, true)
            }
            "pause" -> report(c.id, true)
            "live" -> {
                val until = c.params?.get("until")?.jsonPrimitive?.contentOrNull
                sampler.liveUntilMs = parseEpochMs(until)
                report(c.id, true)
            }
            "restart_camera" -> {
                sampler.restart()
                report(c.id, true)
            }
            "restart_app" -> {
                report(c.id, true)
                delay(RESTART_DELAY_MS)
                Process.killProcess(Process.myPid())
            }
            else -> report(c.id, false, "unsupported command ${c.type}")
        }
    }

    private fun parseEpochMs(iso: String?): Long =
        iso?.let { runCatching { Instant.parse(it).toEpochMilli() }.getOrNull() } ?: 0L

    /** Сервер попросил ещё кадры (неясно, есть ли бензовоз) — docs/05-backend.md §5. */
    private fun requestExtraFrames(n: Int) {
        if (n > 0 && ::sampler.isInitialized) sampler.extraFrames = maxOf(sampler.extraFrames, n)
    }

    // --- очередь и отправка ----------------------------------------------------------------

    /** Кадр с камеры → очередь на диске (поток анализатора). */
    private fun enqueueFrame(cap: CameraSampler.Captured) {
        val meta = FrameMeta(
            frameId = UUID.randomUUID().toString(),
            kind = cap.kind,
            takenAt = cap.takenAt.toString(),
            rotation = cap.rotation,
            width = cap.width,
            height = cap.height,
            zoom = cap.zoom,
            configVersion = cap.configVersion,
            commandId = cap.commandId,
            cropRect = cap.cropRect?.let { listOf(it.x0, it.y0, it.x1, it.y1) },
            diff = cap.diff,
        )
        runCatching { outbox.addFrame(cap.kind, Api.json.encodeToString(FrameMeta.serializer(), meta), cap.jpeg) }
            .onFailure { Log.e(TAG, "outbox add", it) }
        wakeSender.trySend(Unit)
    }

    /** Отправка очереди: события раньше кадров, от старых к новым; при ошибке — пауза с ростом до 60 с. */
    private suspend fun senderLoop() {
        var backoff = SEND_BACKOFF_MIN_MS
        while (lifecycleScope.isActive) {
            val p = pairing
            val ok = if (p == null || disabled) true else sendAll(p)
            if (ok) {
                backoff = SEND_BACKOFF_MIN_MS
                withTimeoutOrNull(SENDER_IDLE_MS) { wakeSender.receive() }
            } else {
                delay(backoff)
                backoff = min(backoff * 2, SEND_BACKOFF_MAX_MS)
            }
        }
    }

    /** true — всё отправлено (или очередь пуста). */
    private suspend fun sendAll(p: Prefs.Pairing): Boolean {
        val items = outbox.items()
        if (items.isEmpty()) return true
        val events = items.filter { it.isEvent }
        if (events.isNotEmpty()) {
            try {
                Api.sendEvents(p, events.map { it.json.readText() })
                events.forEach { outbox.remove(it) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                Log.w(TAG, "send events: ${e.message}")
                return false
            }
        }
        for (it in items.filterNot { it.isEvent }) {
            val metaJson = runCatching { it.json.readText() }.getOrNull()
            val jpeg = runCatching { it.jpeg?.readBytes() }.getOrNull()
            if (metaJson == null || jpeg == null) {
                outbox.remove(it)
                continue
            }
            val meta = Api.json.decodeFromString(FrameMeta.serializer(), metaJson)
            try {
                val r = Api.uploadFrame(p, meta, jpeg)
                outbox.remove(it)
                requestExtraFrames(r.extraFrames)
                lastError = lastError?.takeUnless { e -> e.startsWith("upload") }
                meta.commandId?.let { id -> report(id, true) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: ApiException) {
                if (e.code == 400 || e.code == 413) {
                    // Сервер не примет этот кадр никогда — не держать очередь.
                    Log.e(TAG, "frame rejected: ${e.message}")
                    outbox.remove(it)
                    meta.commandId?.let { id -> report(id, false, e.message) }
                    continue
                }
                lastError = "upload: ${e.message}"
                return false
            } catch (e: Exception) {
                lastError = "upload: ${e.message}"
                return false
            }
        }
        return true
    }

    companion object {
        private const val TAG = "MonitorService"
        private const val CHANNEL = "monitor"
        private const val NOTIFICATION_ID = 1
        private const val MB = 1024L * 1024L
        private const val RESTART_DELAY_MS = 1500L
        private const val WATCHDOG_MS = 10_000L
        private const val CAMERA_SOFT_RESTARTS = 3
        private const val SENDER_IDLE_MS = 30_000L
        private const val SEND_BACKOFF_MIN_MS = 2_000L
        private const val SEND_BACKOFF_MAX_MS = 60_000L

        fun start(ctx: Context) {
            ContextCompat.startForegroundService(ctx, Intent(ctx, MonitorService::class.java))
        }
    }
}
