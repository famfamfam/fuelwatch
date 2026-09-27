package app.fuelwatch

import kotlin.math.abs
import kotlin.math.acos
import kotlin.math.sqrt

/**
 * Сдвиг телефона по акселерометру (D-10, docs/04-android-app.md §6). Чистый Kotlin — unit-тесты.
 * - Гравитация — low-pass по ускорению.
 * - Базовый вектор запоминается после rearm (arm / capture_reference): среднее за BASELINE_MS.
 * - Наклон от базового > tiltAlertDeg дольше TILT_HOLD_MS → MOVED(tilt). Повторно — только после rearm.
 * - Тряска: |a| отличается от g больше shakeMs2 → MOVED(shake), не чаще раза в SHAKE_COOLDOWN_MS.
 */
class MotionLogic(var tiltAlertDeg: Double, var shakeMs2: Double) {
    sealed interface Event {
        data class Tilt(val deg: Double) : Event
        data class Shake(val ms2: Double) : Event
    }

    private val g = DoubleArray(3)
    private var gInit = false
    private var baseline: DoubleArray? = null
    private var baselineSum = DoubleArray(3)
    private var baselineN = 0
    private var baselineUntil = 0L
    private var tiltSince = 0L
    private var tiltFired = false
    private var lastShake = Long.MIN_VALUE / 2

    /** Текущий наклон от базового, градусы; null — базы ещё нет. */
    var tiltDeg: Double? = null
        private set

    val baselineVector: DoubleArray? get() = baseline?.copyOf()

    fun restoreBaseline(v: DoubleArray) {
        baseline = v.copyOf()
    }

    /** Запомнить положение заново (включили мониторинг / сделали эталон). */
    fun rearm(nowMs: Long) {
        baselineSum = DoubleArray(3)
        baselineN = 0
        baselineUntil = nowMs + BASELINE_MS
        tiltFired = false
        tiltSince = 0
    }

    val capturingBaseline get() = baselineUntil > 0

    fun onSample(x: Double, y: Double, z: Double, nowMs: Long): Event? {
        if (!gInit) {
            g[0] = x; g[1] = y; g[2] = z
            gInit = true
        } else {
            g[0] += ALPHA * (x - g[0]); g[1] += ALPHA * (y - g[1]); g[2] += ALPHA * (z - g[2])
        }

        if (baselineUntil > 0) {
            baselineSum[0] += g[0]; baselineSum[1] += g[1]; baselineSum[2] += g[2]
            baselineN++
            if (nowMs >= baselineUntil && baselineN > 0) {
                baseline = doubleArrayOf(baselineSum[0] / baselineN, baselineSum[1] / baselineN, baselineSum[2] / baselineN)
                baselineUntil = 0
            }
        }

        val mag = sqrt(x * x + y * y + z * z)
        val dyn = abs(mag - GRAVITY)
        if (dyn > shakeMs2 && nowMs - lastShake >= SHAKE_COOLDOWN_MS && !capturingBaseline) {
            lastShake = nowMs
            return Event.Shake(dyn)
        }

        val b = baseline ?: return null
        val tilt = angle(g, b)
        tiltDeg = tilt
        if (tilt > tiltAlertDeg && !capturingBaseline) {
            if (tiltSince == 0L) tiltSince = nowMs
            if (!tiltFired && nowMs - tiltSince >= TILT_HOLD_MS) {
                tiltFired = true
                return Event.Tilt(tilt)
            }
        } else {
            tiltSince = 0
        }
        return null
    }

    companion object {
        const val GRAVITY = 9.81
        const val ALPHA = 0.1
        const val BASELINE_MS = 5_000L
        const val TILT_HOLD_MS = 10_000L
        const val SHAKE_COOLDOWN_MS = 60_000L

        fun angle(a: DoubleArray, b: DoubleArray): Double {
            val dot = a[0] * b[0] + a[1] * b[1] + a[2] * b[2]
            val na = sqrt(a[0] * a[0] + a[1] * a[1] + a[2] * a[2])
            val nb = sqrt(b[0] * b[0] + b[1] * b[1] + b[2] * b[2])
            if (na == 0.0 || nb == 0.0) return 0.0
            return Math.toDegrees(acos((dot / (na * nb)).coerceIn(-1.0, 1.0)))
        }
    }
}
