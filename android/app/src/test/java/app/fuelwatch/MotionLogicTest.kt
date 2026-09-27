package app.fuelwatch

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import kotlin.math.cos
import kotlin.math.sin

class MotionLogicTest {
    private val stepMs = 20L // 50 Гц

    /** Телефон наклонён на deg градусов вокруг оси X. */
    private fun g(deg: Double): Triple<Double, Double, Double> {
        val r = Math.toRadians(deg)
        return Triple(0.0, MotionLogic.GRAVITY * sin(r), MotionLogic.GRAVITY * cos(r))
    }

    private fun feed(m: MotionLogic, from: Long, toMs: Long, deg: Double): List<MotionLogic.Event> {
        val out = mutableListOf<MotionLogic.Event>()
        var t = from
        val (x, y, z) = g(deg)
        while (t < toMs) {
            m.onSample(x, y, z, t)?.let { out += it }
            t += stepMs
        }
        return out
    }

    private fun armed(): MotionLogic {
        val m = MotionLogic(tiltAlertDeg = 2.0, shakeMs2 = 3.0)
        m.rearm(0)
        assertTrue(feed(m, 0, 6_000, 0.0).isEmpty())
        return m
    }

    @Test
    fun steadyPhoneRaisesNothing() {
        val m = armed()
        assertTrue(feed(m, 6_000, 60_000, 0.5).isEmpty())
        assertTrue(m.tiltDeg!! < 1.0)
    }

    @Test
    fun sustainedTiltRaisesOnceAfterHold() {
        val m = armed()
        val ev = feed(m, 6_000, 30_000, 5.0)
        assertEquals(1, ev.size)
        val tilt = ev[0] as MotionLogic.Event.Tilt
        assertEquals(5.0, tilt.deg, 0.3)
        assertTrue(feed(m, 30_000, 60_000, 5.0).isEmpty()) // повторно — только после rearm
    }

    @Test
    fun shortTiltIsIgnored() {
        val m = armed()
        assertTrue(feed(m, 6_000, 12_000, 5.0).isEmpty()) // 6 с < 10 с
        assertTrue(feed(m, 12_000, 30_000, 0.0).isEmpty())
    }

    @Test
    fun rearmAcceptsNewPosition() {
        val m = armed()
        assertEquals(1, feed(m, 6_000, 30_000, 5.0).size)
        m.rearm(30_000)
        assertTrue(feed(m, 30_000, 60_000, 5.0).isEmpty())
        assertTrue(m.tiltDeg!! < 1.0)
    }

    @Test
    fun shakeRaisesWithCooldown() {
        val m = armed()
        val hit = m.onSample(0.0, 0.0, 16.0, 10_000) // рывок 6 м/с²
        assertTrue(hit is MotionLogic.Event.Shake)
        assertNull(m.onSample(0.0, 0.0, 16.0, 20_000)) // в пределах минуты — нет
        assertTrue(m.onSample(0.0, 0.0, 16.0, 71_000) is MotionLogic.Event.Shake)
    }

    @Test
    fun noBaselineNoTilt() {
        val m = MotionLogic(2.0, 3.0)
        assertTrue(feed(m, 0, 30_000, 20.0).isEmpty())
        assertNull(m.tiltDeg)
    }
}
