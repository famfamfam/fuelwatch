package app.fuelwatch

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.MultipartBody
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import java.io.IOException
import java.util.concurrent.TimeUnit

@Serializable
data class PairRequest(
    val code: String,
    val model: String,
    val android: String,
    @SerialName("app_version") val appVersion: String,
)

@Serializable
data class PairResponse(@SerialName("device_id") val deviceId: String, val token: String)

@Serializable
data class CommandDone(val id: String, val ok: Boolean, val error: String? = null)

@Serializable
data class CameraCaps(
    @SerialName("camera_id") val cameraId: String,
    @SerialName("zoom_min") val zoomMin: Float,
    @SerialName("zoom_max") val zoomMax: Float,
    val resolutions: List<String>,
    @SerialName("manual_focus") val manualFocus: Boolean,
    @SerialName("fps_ranges") val fpsRanges: List<List<Int>>,
    @SerialName("sensor_orientation") val sensorOrientation: Int,
)

@Serializable
data class HeartbeatRequest(
    val time: String,
    @SerialName("uptime_s") val uptimeS: Long,
    @SerialName("app_version") val appVersion: String,
    val mode: String,
    @SerialName("config_version") val configVersion: Int,
    val battery: Int?,
    val charging: Boolean?,
    @SerialName("battery_temp_c") val batteryTempC: Double?,
    @SerialName("thermal_status") val thermalStatus: Int?,
    @SerialName("last_frame_age_s") val lastFrameAgeS: Double?,
    @SerialName("tilt_deg") val tiltDeg: Double? = null,
    val network: String?,
    @SerialName("tx_bytes_today") val txBytesToday: Long? = null,
    @SerialName("queue_items") val queueItems: Int,
    @SerialName("queue_mb") val queueMb: Double = 0.0,
    val live: Boolean = false,
    @SerialName("last_error") val lastError: String?,
    @SerialName("camera_caps") val cameraCaps: CameraCaps?,
    @SerialName("commands_done") val commandsDone: List<CommandDone>,
)

@Serializable
data class Command(
    val id: String,
    val type: String,
    val params: JsonObject? = null,
    @SerialName("expires_at") val expiresAt: String,
)

@Serializable
data class HeartbeatResponse(
    val commands: List<Command> = emptyList(),
    val config: JsonObject? = null,
    @SerialName("extra_frames") val extraFrames: Int = 0,
)

@Serializable
data class FrameMeta(
    @SerialName("frame_id") val frameId: String,
    val kind: String,
    @SerialName("taken_at") val takenAt: String,
    val rotation: Int,
    val width: Int,
    val height: Int,
    val zoom: Float,
    @SerialName("config_version") val configVersion: Int,
    @SerialName("command_id") val commandId: String? = null,
    /** [x0, y0, x1, y1] выреза в нормализованных координатах повёрнутого полного кадра. */
    @SerialName("crop_rect") val cropRect: List<Double>? = null,
    val diff: Double? = null,
)

@Serializable
data class FrameResponse(val ok: Boolean = false, @SerialName("extra_frames") val extraFrames: Int = 0)

/** Ошибка HTTP от сервера: code — статус, error — поле "error" ответа (docs/07-api.md). */
class ApiException(val code: Int, val error: String?, message: String) : IOException(message)

/** HTTP-клиент телефона (docs/07-api.md §1). */
object Api {
    val json = Json {
        ignoreUnknownKeys = true
        explicitNulls = false
        encodeDefaults = true
    }

    private val client = OkHttpClient.Builder()
        .connectTimeout(15, TimeUnit.SECONDS)
        .readTimeout(30, TimeUnit.SECONDS)
        .writeTimeout(60, TimeUnit.SECONDS)
        .build()

    private val jsonType = "application/json; charset=utf-8".toMediaType()
    private val jpegType = "image/jpeg".toMediaType()

    suspend fun pair(server: String, req: PairRequest): PairResponse =
        post(server, "/api/device/pair", null, json.encodeToString(PairRequest.serializer(), req).toRequestBody(jsonType))
            .let { json.decodeFromString(PairResponse.serializer(), it) }

    suspend fun heartbeat(p: Prefs.Pairing, req: HeartbeatRequest): HeartbeatResponse =
        post(p.server, "/api/device/heartbeat", p.token, json.encodeToString(HeartbeatRequest.serializer(), req).toRequestBody(jsonType))
            .let { json.decodeFromString(HeartbeatResponse.serializer(), it) }

    suspend fun uploadFrame(p: Prefs.Pairing, meta: FrameMeta, jpeg: ByteArray): FrameResponse {
        val body = MultipartBody.Builder().setType(MultipartBody.FORM)
            .addFormDataPart("meta", json.encodeToString(FrameMeta.serializer(), meta))
            .addFormDataPart("image", "${meta.frameId}.jpg", jpeg.toRequestBody(jpegType))
            .build()
        return post(p.server, "/api/device/frames", p.token, body)
            .let { json.decodeFromString(FrameResponse.serializer(), it) }
    }

    /** События из очереди — одним запросом (docs/07-api.md, /events). */
    suspend fun sendEvents(p: Prefs.Pairing, eventsJson: List<String>) {
        val body = "{\"events\":[" + eventsJson.joinToString(",") + "]}"
        post(p.server, "/api/device/events", p.token, body.toRequestBody(jsonType))
    }

    private suspend fun post(server: String, path: String, token: String?, body: okhttp3.RequestBody): String =
        withContext(Dispatchers.IO) {
            val rb = Request.Builder().url(server.trimEnd('/') + path).post(body)
            if (token != null) rb.header("Authorization", "Bearer $token")
            client.newCall(rb.build()).execute().use { resp ->
                val text = resp.body?.string().orEmpty()
                if (!resp.isSuccessful) {
                    val obj = runCatching { json.parseToJsonElement(text) as JsonObject }.getOrNull()
                    val code = obj?.get("error")?.jsonPrimitive?.contentOrNull
                    val msg = obj?.get("message")?.jsonPrimitive?.contentOrNull ?: "HTTP ${resp.code}"
                    throw ApiException(resp.code, code, msg)
                }
                text
            }
        }
}
