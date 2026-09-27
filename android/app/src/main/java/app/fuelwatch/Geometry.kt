package app.fuelwatch

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.doubleOrNull
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonPrimitive

/** Прямоугольник в нормализованных координатах [0..1]. */
data class NormRect(val x0: Double, val y0: Double, val x1: Double, val y1: Double)

/** Прямоугольник в пикселях буфера камеры. */
data class PixRect(val left: Int, val top: Int, val right: Int, val bottom: Int)

data class Zone(val monitor: Boolean, val xs: DoubleArray, val ys: DoubleArray)

/**
 * Координаты зон (docs/04-android-app.md §4). Зоны заданы нормализованно для ПОВЁРНУТОГО кадра
 * (как его видит панель), буфер камеры — в ориентации сенсора. rotation — на сколько градусов
 * по часовой повернуть буфер, чтобы получить нормальный вид (imageInfo.rotationDegrees).
 * Чистый Kotlin без Android API — покрыт unit-тестами.
 */
object Geometry {
    /** Повёрнутый (u, v) → буфер (x, y). */
    fun toBuffer(u: Double, v: Double, rotation: Int): Pair<Double, Double> = when (rotation) {
        90 -> v to 1 - u
        180 -> 1 - u to 1 - v
        270 -> 1 - v to u
        else -> u to v
    }

    /** Буфер (x, y) → повёрнутый (u, v). */
    fun toRotated(x: Double, y: Double, rotation: Int): Pair<Double, Double> = when (rotation) {
        90 -> 1 - y to x
        180 -> 1 - x to 1 - y
        270 -> y to 1 - x
        else -> x to y
    }

    fun parseZones(arr: JsonArray): List<Zone> = arr.mapNotNull { el ->
        val obj = el as? JsonObject ?: return@mapNotNull null
        val type = obj["type"]?.jsonPrimitive?.contentOrNull
        val pts = runCatching { obj["points"]!!.jsonArray }.getOrNull() ?: return@mapNotNull null
        val xs = DoubleArray(pts.size)
        val ys = DoubleArray(pts.size)
        for ((i, p) in pts.withIndex()) {
            val pair = runCatching { p.jsonArray }.getOrNull() ?: return@mapNotNull null
            xs[i] = pair.getOrNull(0)?.jsonPrimitive?.doubleOrNull ?: return@mapNotNull null
            ys[i] = pair.getOrNull(1)?.jsonPrimitive?.doubleOrNull ?: return@mapNotNull null
        }
        if (pts.size < 3 || (type != "MONITOR" && type != "IGNORE")) null else Zone(type == "MONITOR", xs, ys)
    }

    fun inside(x: Double, y: Double, z: Zone): Boolean {
        var inside = false
        var j = z.xs.size - 1
        for (i in z.xs.indices) {
            val yi = z.ys[i]
            val yj = z.ys[j]
            if ((yi > y) != (yj > y) && x < (z.xs[j] - z.xs[i]) * (y - yi) / (yj - yi) + z.xs[i]) inside = !inside
            j = i
        }
        return inside
    }

    /**
     * Маска w×h в ориентации буфера: MONITOR минус IGNORE. Без MONITOR — весь кадр (null).
     */
    fun mask(zones: List<Zone>, w: Int, h: Int, rotation: Int): BooleanArray? {
        if (zones.none { it.monitor }) return null
        val m = BooleanArray(w * h)
        for (cy in 0 until h) {
            for (cx in 0 until w) {
                val (u, v) = toRotated((cx + 0.5) / w, (cy + 0.5) / h, rotation)
                m[cy * w + cx] = zones.any { it.monitor && inside(u, v, it) } && zones.none { !it.monitor && inside(u, v, it) }
            }
        }
        return if (m.any { it }) m else null
    }

    /** Прямоугольник вокруг MONITOR с запасом, в повёрнутых координатах. null — MONITOR нет. */
    fun monitorBounds(zones: List<Zone>, margin: Double): NormRect? {
        val mon = zones.filter { it.monitor }
        if (mon.isEmpty()) return null
        return NormRect(
            (mon.minOf { it.xs.min() } - margin).coerceAtLeast(0.0),
            (mon.minOf { it.ys.min() } - margin).coerceAtLeast(0.0),
            (mon.maxOf { it.xs.max() } + margin).coerceAtMost(1.0),
            (mon.maxOf { it.ys.max() } + margin).coerceAtMost(1.0),
        )
    }

    /**
     * Вырез для JPEG: прямоугольник в пикселях буфера (чётные координаты — требование NV21)
     * и тот же прямоугольник в повёрнутых нормализованных координатах (crop_rect для сервера).
     */
    fun crop(bounds: NormRect, rotation: Int, bufW: Int, bufH: Int): Pair<PixRect, NormRect> {
        val (ax, ay) = toBuffer(bounds.x0, bounds.y0, rotation)
        val (bx, by) = toBuffer(bounds.x1, bounds.y1, rotation)
        fun even(v: Double, size: Int) = ((v * size).toInt() and 1.inv()).coerceIn(0, size and 1.inv())
        val left = even(minOf(ax, bx), bufW)
        val top = even(minOf(ay, by), bufH)
        var right = even(maxOf(ax, bx) + 1.0 / bufW, bufW)
        var bottom = even(maxOf(ay, by) + 1.0 / bufH, bufH)
        if (right - left < 2) right = minOf(left + 2, bufW and 1.inv())
        if (bottom - top < 2) bottom = minOf(top + 2, bufH and 1.inv())
        val px = PixRect(left, top, right, bottom)
        val (u0, v0) = toRotated(left.toDouble() / bufW, top.toDouble() / bufH, rotation)
        val (u1, v1) = toRotated(right.toDouble() / bufW, bottom.toDouble() / bufH, rotation)
        return px to NormRect(minOf(u0, u1), minOf(v0, v1), maxOf(u0, u1), maxOf(v0, v1))
    }
}
