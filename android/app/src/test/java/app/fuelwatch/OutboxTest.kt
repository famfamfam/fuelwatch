package app.fuelwatch

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.nio.file.Files

class OutboxTest {
    private fun box(max: Long) = Outbox(Files.createTempDirectory("outbox").toFile(), max)

    @Test
    fun eventsFirstThenFramesOldestFirst() {
        val o = box(10_000_000)
        o.addFrame("keyframe", "{\"n\":1}", ByteArray(10))
        Thread.sleep(2)
        o.addEvent("{\"type\":\"POWER_OFF\"}")
        Thread.sleep(2)
        o.addFrame("change", "{\"n\":2}", ByteArray(10))
        val kinds = o.items().map { it.kind }
        assertEquals(listOf("event", "keyframe", "change"), kinds)
        assertEquals(3, o.count())
    }

    @Test
    fun overflowDropsKeyframesFirstAndKeepsEvents() {
        val o = box(2_500)
        o.addEvent("{\"type\":\"MOVED\"}")
        o.addFrame("keyframe", "{}", ByteArray(1000)); Thread.sleep(2)
        o.addFrame("change", "{}", ByteArray(1000)); Thread.sleep(2)
        o.addFrame("keyframe", "{}", ByteArray(1000)); Thread.sleep(2)
        o.addFrame("change", "{}", ByteArray(1000))
        val kinds = o.items().map { it.kind }
        assertTrue("event kept: $kinds", kinds.first() == "event")
        assertTrue("size ${o.sizeBytes()}", o.sizeBytes() <= 2_500)
        assertEquals("keyframes dropped first: $kinds", listOf("event", "change", "change"), kinds)
    }

    @Test
    fun removeAndSurviveRestart() {
        val dir = Files.createTempDirectory("outbox").toFile()
        val o = Outbox(dir, 1_000_000)
        o.addFrame("snapshot", "{}", ByteArray(5))
        java.io.File(dir, "junk.json.tmp").writeText("partial")
        val again = Outbox(dir, 1_000_000) // «после перезапуска процесса»
        val items = again.items()
        assertEquals(1, items.size)
        again.remove(items[0])
        assertEquals(0, again.count())
        assertTrue(dir.listFiles()!!.isEmpty())
    }
}
