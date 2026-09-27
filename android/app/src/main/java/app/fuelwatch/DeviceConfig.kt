package app.fuelwatch

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.doubleOrNull
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonPrimitive
import kotlin.math.roundToInt

/**
 * Конфиг от сервера (docs/07-api.md §1, «Конфиг»). Настройки читаются по ключам реестра.
 * Значения по умолчанию действуют только до первого heartbeat и повторяют docs/08-config.md.
 */
class DeviceConfig(
    val configVersion: Int,
    val mode: String,
    val liveUntil: String?,
    val zones: JsonArray,
    private val settings: Map<String, JsonPrimitive>,
) {
    fun int(key: String): Int =
        settings[key]?.doubleOrNull?.roundToInt() ?: (DEFAULTS[key] as Number).toInt()

    fun double(key: String): Double =
        settings[key]?.doubleOrNull ?: (DEFAULTS[key] as Number).toDouble()

    fun bool(key: String): Boolean = settings[key]?.booleanOrNull ?: DEFAULTS[key] as Boolean

    fun string(key: String): String = settings[key]?.contentOrNull ?: DEFAULTS[key] as String

    companion object {
        val DEFAULTS: Map<String, Any> = mapOf(
            "capture.sample_interval_s" to 15,
            "capture.keyframe_interval_s" to 300,
            "capture.max_uploads_per_hour" to 40,
            "capture.resolution" to "1920x1080",
            "capture.zoom" to 1.0,
            "capture.focus_mode" to "auto",
            "capture.jpeg_quality" to 75,
            "capture.crop_margin" to 0.10,
            "capture.keep_screen_on" to true,
            "detector.change_frac" to 0.08,
            "detector.pixel_delta" to 18,
            "detector.pixel_delta_night" to 28,
            "detector.night_luma" to 40,
            "detector.max_light_jump" to 0.35,
            "live.interval_s" to 5,
            "live.heartbeat_interval_s" to 5,
            "net.heartbeat_interval_s" to 20,
            "queue.max_mb" to 300,
            "health.camera_stale_s" to 120,
            "health.tilt_alert_deg" to 2.0,
            "health.shake_ms2" to 3.0,
        )

        val EMPTY = DeviceConfig(0, "setup", null, JsonArray(emptyList()), emptyMap())

        /** Разбор с проверкой: непонятные ключи и не-примитивы пропускаются. */
        fun parse(obj: JsonObject): DeviceConfig {
            val settings = (obj["settings"] as? JsonObject)
                ?.filterValues { it is JsonPrimitive }
                ?.mapValues { it.value.jsonPrimitive }
                ?: emptyMap()
            return DeviceConfig(
                configVersion = obj["config_version"]?.jsonPrimitive?.intOrNull ?: 0,
                mode = obj["mode"]?.jsonPrimitive?.contentOrNull ?: "setup",
                liveUntil = obj["live_until"]?.jsonPrimitive?.contentOrNull,
                zones = runCatching { obj["zones"]?.jsonArray }.getOrNull() ?: JsonArray(emptyList()),
                settings = settings,
            )
        }
    }
}
