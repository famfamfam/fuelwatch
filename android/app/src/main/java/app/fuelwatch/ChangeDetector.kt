package app.fuelwatch

import kotlin.math.abs
import kotlin.math.roundToInt

/**
 * Сравнение кадров (D-05, docs/04-android-app.md §5). Чистый Kotlin без Android API.
 * Сравнивает уменьшенный Y-канал с последним ЗАГРУЖЕННЫМ кадром внутри маски зоны,
 * выравнивая общую яркость (облака, автоэкспозиция).
 */
class ChangeDetector {
    data class Params(
        val pixelDelta: Int,
        val pixelDeltaNight: Int,
        val nightLuma: Int,
        val maxLightJump: Double,
    ) {
        companion object {
            fun from(c: DeviceConfig) = Params(
                pixelDelta = c.int("detector.pixel_delta"),
                pixelDeltaNight = c.int("detector.pixel_delta_night"),
                nightLuma = c.int("detector.night_luma"),
                maxLightJump = c.double("detector.max_light_jump"),
            )
        }
    }

    sealed interface Result {
        /** Базы ещё нет или размер изменился — сравнивать не с чем. */
        data object NoBase : Result

        /** Резкий скачок освещения (фары, вспышка) — выборку пропустить. */
        data class LightJump(val gain: Double) : Result

        /** changed — доля изменившихся клеток маски. */
        data class Diff(val changed: Double, val night: Boolean) : Result
    }

    private var base: IntArray? = null

    fun setBase(thumb: IntArray) {
        base = thumb.copyOf()
    }

    fun hasBase() = base != null

    fun compare(thumb: IntArray, mask: BooleanArray?, p: Params): Result {
        val b = base
        if (b == null || b.size != thumb.size || (mask != null && mask.size != thumb.size)) return Result.NoBase
        var sumB = 0L
        var sumT = 0L
        var n = 0
        for (i in thumb.indices) {
            if (mask == null || mask[i]) {
                sumB += b[i]
                sumT += thumb[i]
                n++
            }
        }
        if (n == 0) return Result.NoBase
        val meanB = sumB.toDouble() / n
        val meanT = sumT.toDouble() / n
        // В почти полной темноте отношение яркостей — шум (0 против 1): не выравнивать и не считать скачком.
        val gain = if (meanB < LOW_LIGHT_LUMA || meanT < LOW_LIGHT_LUMA) 1.0 else meanB / meanT
        if (abs(1 - gain) > p.maxLightJump) return Result.LightJump(gain)
        val night = meanT < p.nightLuma
        val delta = if (night) p.pixelDeltaNight else p.pixelDelta
        var changed = 0
        for (i in thumb.indices) {
            if ((mask == null || mask[i]) && abs(thumb[i] * gain - b[i]) > delta) changed++
        }
        return Result.Diff(changed.toDouble() / n, night)
    }

    companion object {
        const val THUMB_W = 64

        /** Ниже этой средней яркости (0–255) выравнивание яркости не применяется. */
        const val LOW_LIGHT_LUMA = 8.0

        /** Высота уменьшенного кадра с теми же пропорциями (64×36 для 16:9). */
        fun thumbHeight(w: Int, h: Int) = maxOf(1, (THUMB_W.toDouble() * h / w).roundToInt())

        /**
         * Уменьшить Y-плоскость до outW×outH средним по блокам. Внутри блока берётся каждый step-й пиксель:
         * для сравнения этого достаточно, а работы в десятки раз меньше.
         */
        fun downscale(
            y: ByteArray, w: Int, h: Int, rowStride: Int, pixelStride: Int,
            outW: Int, outH: Int, step: Int = 4,
        ): IntArray {
            val out = IntArray(outW * outH)
            for (oy in 0 until outH) {
                val y0 = oy * h / outH
                val y1 = maxOf(y0 + 1, (oy + 1) * h / outH)
                for (ox in 0 until outW) {
                    val x0 = ox * w / outW
                    val x1 = maxOf(x0 + 1, (ox + 1) * w / outW)
                    var sum = 0L
                    var cnt = 0
                    var yy = y0
                    while (yy < y1) {
                        var xx = x0
                        val row = yy * rowStride
                        while (xx < x1) {
                            sum += y[row + xx * pixelStride].toInt() and 0xFF
                            cnt++
                            xx += step
                        }
                        yy += step
                    }
                    out[oy * outW + ox] = (sum / cnt).toInt()
                }
            }
            return out
        }
    }
}
