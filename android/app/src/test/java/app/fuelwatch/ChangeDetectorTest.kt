package app.fuelwatch

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import kotlin.random.Random

class ChangeDetectorTest {
    private val p = ChangeDetector.Params(pixelDelta = 18, pixelDeltaNight = 28, nightLuma = 40, maxLightJump = 0.35)
    private val w = 64
    private val h = 36

    private fun scene(level: Int = 120, noise: Int = 0, seed: Int = 1): IntArray {
        val r = Random(seed)
        return IntArray(w * h) { (level + if (noise > 0) r.nextInt(-noise, noise + 1) else 0).coerceIn(0, 255) }
    }

    private fun withObject(src: IntArray, x0: Int, y0: Int, x1: Int, y1: Int, value: Int = 30): IntArray {
        val out = src.copyOf()
        for (y in y0 until y1) for (x in x0 until x1) out[y * w + x] = value
        return out
    }

    private fun detector(base: IntArray) = ChangeDetector().apply { setBase(base) }

    @Test
    fun noBaseWithoutReference() {
        assertEquals(ChangeDetector.Result.NoBase, ChangeDetector().compare(scene(), null, p))
    }

    @Test
    fun objectAppearsInZone() {
        val base = scene(noise = 3)
        val cur = withObject(base, 10, 10, 40, 30) // «грузовик» ~26% кадра
        val r = detector(base).compare(cur, null, p) as ChangeDetector.Result.Diff
        assertTrue("changed=${r.changed}", r.changed > 0.2)
    }

    @Test
    fun globalBrightnessShiftIsCompensated() {
        val base = scene(level = 120, noise = 3)
        val brighter = IntArray(base.size) { (base[it] * 1.2).toInt() } // облако ушло / автоэкспозиция
        val r = detector(base).compare(brighter, null, p) as ChangeDetector.Result.Diff
        assertTrue("changed=${r.changed}", r.changed < 0.02)
    }

    @Test
    fun sensorNoiseIsIgnored() {
        val r = detector(scene(noise = 8, seed = 1)).compare(scene(noise = 8, seed = 2), null, p) as ChangeDetector.Result.Diff
        assertTrue("changed=${r.changed}", r.changed < 0.02)
    }

    @Test
    fun suddenLightJumpIsSkipped() {
        val base = scene(level = 60)
        val flash = scene(level = 200)
        assertTrue(detector(base).compare(flash, null, p) is ChangeDetector.Result.LightJump)
    }

    @Test
    fun pitchBlackNoiseIsNotALightJump() {
        val black = IntArray(w * h) { 0 }
        val noisy = IntArray(w * h) { if (it % 3 == 0) 2 else 0 }
        val r = detector(black).compare(noisy, null, p)
        assertTrue("$r", r is ChangeDetector.Result.Diff && r.changed == 0.0)
    }

    @Test
    fun lightsOnInDarkRoomIsAChangeNotAJump() {
        val black = IntArray(w * h) { 1 }
        val lit = scene(level = 90)
        val r = detector(black).compare(lit, null, p) as ChangeDetector.Result.Diff
        assertTrue("changed=${r.changed}", r.changed > 0.9)
    }

    @Test
    fun changeOutsideMaskIsIgnored() {
        val base = scene(noise = 3)
        val cur = withObject(base, 0, 0, 30, 36) // слева
        val mask = BooleanArray(w * h) { (it % w) >= 32 } // зона — правая половина
        val r = detector(base).compare(cur, mask, p) as ChangeDetector.Result.Diff
        assertTrue("changed=${r.changed}", r.changed < 0.02)
    }

    @Test
    fun nightUsesHigherThreshold() {
        // Отклонение ±22 при неизменной средней яркости: днём (порог 18) — изменение, ночью (порог 28) — нет.
        fun jitter(level: Int) = IntArray(w * h) { if (it % 2 == 0) level + 22 else level - 22 }
        val night = detector(scene(level = 30)).compare(jitter(30), null, p) as ChangeDetector.Result.Diff
        assertTrue(night.night)
        assertEquals(0.0, night.changed, 1e-9)
        val day = detector(scene(level = 120)).compare(jitter(120), null, p) as ChangeDetector.Result.Diff
        assertFalse(day.night)
        assertEquals(1.0, day.changed, 1e-9)
    }

    @Test
    fun downscaleAveragesBlocks() {
        val srcW = 128
        val srcH = 72
        val stride = 136 // rowStride больше ширины, как у реальных буферов
        val y = ByteArray(stride * srcH) { i -> if ((i % stride) < srcW / 2) 50 else 200.toByte() }
        val t = ChangeDetector.downscale(y, srcW, srcH, stride, 1, 64, 36, step = 1)
        assertEquals(64 * 36, t.size)
        assertEquals(50, t[0])
        assertEquals(200, t[63])
        assertEquals(36, ChangeDetector.thumbHeight(1920, 1080))
        assertEquals(48, ChangeDetector.thumbHeight(1440, 1080))
    }
}
