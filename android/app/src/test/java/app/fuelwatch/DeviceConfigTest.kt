package app.fuelwatch

import kotlinx.serialization.json.JsonObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class DeviceConfigTest {
    private fun parse(s: String) = DeviceConfig.parse(Api.json.decodeFromString(JsonObject.serializer(), s))

    @Test
    fun readsServerValues() {
        val c = parse(
            """{"config_version":8,"mode":"armed","live_until":null,"zones":[{"type":"MONITOR","points":[[0,0],[1,0],[1,1]]}],
               "settings":{"capture.keyframe_interval_s":60,"capture.zoom":2.5,"capture.keep_screen_on":false,"capture.resolution":"1280x720"}}""",
        )
        assertEquals(8, c.configVersion)
        assertEquals("armed", c.mode)
        assertEquals(60, c.int("capture.keyframe_interval_s"))
        assertEquals(2.5, c.double("capture.zoom"), 1e-9)
        assertEquals(false, c.bool("capture.keep_screen_on"))
        assertEquals("1280x720", c.string("capture.resolution"))
        assertEquals(1, c.zones.size)
    }

    @Test
    fun fallsBackToDefaultsOnMissingOrBadValues() {
        val c = parse("""{"config_version":"x","zones":"bad","settings":{"capture.keyframe_interval_s":"sixty","capture.zoom":{"a":1}}}""")
        assertEquals(0, c.configVersion)
        assertEquals("setup", c.mode)
        assertTrue(c.zones.isEmpty())
        assertEquals(300, c.int("capture.keyframe_interval_s"))
        assertEquals(1.0, c.double("capture.zoom"), 1e-9)
        assertEquals(20, c.int("net.heartbeat_interval_s"))
    }

    @Test
    fun cameraParamsFromConfig() {
        val p = CameraSampler.Params.from(parse("""{"config_version":3,"settings":{"capture.resolution":"1280x720","capture.focus_mode":"infinity"}}"""))
        assertEquals(1280, p.width)
        assertEquals(720, p.height)
        assertTrue(p.focusInfinity)
        assertEquals(300_000L, p.keyframeIntervalMs)
    }

    @Test
    fun emptyConfigHasDefaultsForEverything() {
        // До первого heartbeat конфига нет: все ключи, которые читает приложение, должны иметь значения по умолчанию.
        val p = CameraSampler.Params.from(DeviceConfig.EMPTY)
        assertEquals(1920, p.width)
        assertEquals(15_000L, p.sampleIntervalMs)
        assertEquals(0.08, p.changeFrac, 1e-9)
        assertEquals(5_000L, p.liveIntervalMs)
    }
}
