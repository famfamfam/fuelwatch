package app.fuelwatch

import kotlinx.serialization.json.JsonArray
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class GeometryTest {
    private fun zones(json: String) = Geometry.parseZones(Api.json.decodeFromString(JsonArray.serializer(), json))

    @Test
    fun rotationRoundTrip() {
        for (r in listOf(0, 90, 180, 270)) {
            val (x, y) = Geometry.toBuffer(0.2, 0.7, r)
            val (u, v) = Geometry.toRotated(x, y, r)
            assertEquals(0.2, u, 1e-9)
            assertEquals(0.7, v, 1e-9)
        }
        // 90°: верх-левый угол повёрнутого кадра — это низ-левый угол буфера (как на сервере в store.rotate)
        val (x, y) = Geometry.toBuffer(0.0, 0.0, 90)
        assertEquals(0.0, x, 1e-9)
        assertEquals(1.0, y, 1e-9)
    }

    @Test
    fun maskMonitorMinusIgnore() {
        val zs = zones(
            """[{"type":"MONITOR","points":[[0.5,0],[1,0],[1,1],[0.5,1]]},
               {"type":"IGNORE","points":[[0.75,0],[1,0],[1,1],[0.75,1]]}]""",
        )
        val m = Geometry.mask(zs, 64, 36, 0)!!
        assertFalse(m[10 * 64 + 10]) // левая половина — вне зоны
        assertTrue(m[10 * 64 + 40]) // MONITOR
        assertFalse(m[10 * 64 + 60]) // IGNORE
    }

    @Test
    fun maskWithoutMonitorIsWholeFrame() {
        assertNull(Geometry.mask(zones("""[{"type":"IGNORE","points":[[0,0],[1,0],[1,1]]}]"""), 64, 36, 0))
    }

    @Test
    fun cropIsEvenAndMapsBack() {
        val zs = zones("""[{"type":"MONITOR","points":[[0.2,0.5],[0.6,0.5],[0.6,0.9],[0.2,0.9]]}]""")
        val b = Geometry.monitorBounds(zs, 0.1)!!
        for (r in listOf(0, 90, 180, 270)) {
            val (px, norm) = Geometry.crop(b, r, 1920, 1080)
            assertTrue(px.left % 2 == 0 && px.top % 2 == 0 && px.right % 2 == 0 && px.bottom % 2 == 0)
            assertTrue(px.right <= 1920 && px.bottom <= 1080 && px.left < px.right && px.top < px.bottom)
            assertEquals("r=$r x0", b.x0, norm.x0, 0.01)
            assertEquals("r=$r y1", b.y1, norm.y1, 0.01)
        }
    }

    @Test
    fun invalidZonesAreSkipped() {
        val zs = zones("""[{"type":"MONITOR","points":[[0,0],[1,0]]},{"type":"X","points":[[0,0],[1,0],[1,1]]},{"a":1}]""")
        assertTrue(zs.isEmpty())
    }
}
