package app.fuelwatch

import android.content.Context
import android.graphics.ImageFormat
import android.graphics.Rect
import android.hardware.camera2.CameraCharacteristics
import android.hardware.camera2.CameraMetadata
import android.hardware.camera2.CaptureRequest
import android.hardware.display.DisplayManager
import android.os.SystemClock
import android.util.Log
import android.util.Range
import android.util.Size
import android.view.Display
import androidx.annotation.OptIn
import androidx.camera.camera2.interop.Camera2CameraInfo
import androidx.camera.camera2.interop.Camera2Interop
import androidx.camera.camera2.interop.ExperimentalCamera2Interop
import androidx.camera.core.AspectRatio
import androidx.camera.core.Camera
import androidx.camera.core.CameraSelector
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.ImageProxy
import androidx.camera.core.resolutionselector.AspectRatioStrategy
import androidx.camera.core.resolutionselector.ResolutionSelector
import androidx.camera.core.resolutionselector.ResolutionStrategy
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.concurrent.futures.await
import androidx.lifecycle.LifecycleOwner
import java.time.Instant
import java.util.ArrayDeque
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.Executors
import kotlin.math.abs

/**
 * Единственный поток камеры — CameraX ImageAnalysis (D-04). Кадры приходят непрерывно и сразу закрываются,
 * кроме выборки раз в capture.sample_interval_s (D-05, docs/04-android-app.md §5):
 *  - запрос (снимок, эталон) → полный кадр;
 *  - живой режим → полный кадр kind=snapshot раз в live.interval_s;
 *  - пора плановый → полный кадр kind=keyframe;
 *  - сервер попросил ещё кадры → вырез зоны kind=change;
 *  - режим armed и изменение в зоне ≥ detector.change_frac → вырез зоны kind=change (с лимитом в час).
 * База для сравнения — последний загруженный keyframe/change.
 */
class CameraSampler(
    private val ctx: Context,
    private val owner: LifecycleOwner,
    private val onCaptured: (Captured) -> Unit,
) {
    data class Params(
        val width: Int,
        val height: Int,
        val zoom: Float,
        val focusInfinity: Boolean,
        val jpegQuality: Int,
        val sampleIntervalMs: Long,
        val keyframeIntervalMs: Long,
        val maxUploadsPerHour: Int,
        val cropMargin: Double,
        val changeFrac: Double,
        val detector: ChangeDetector.Params,
        val liveIntervalMs: Long,
        val armed: Boolean,
        val zones: List<Zone>,
        val configVersion: Int,
    ) {
        companion object {
            fun from(c: DeviceConfig): Params {
                val (w, h) = c.string("capture.resolution").split('x').mapNotNull { it.toIntOrNull() }
                    .takeIf { it.size == 2 } ?: listOf(1920, 1080)
                return Params(
                    width = w,
                    height = h,
                    zoom = c.double("capture.zoom").toFloat(),
                    focusInfinity = c.string("capture.focus_mode") == "infinity",
                    jpegQuality = c.int("capture.jpeg_quality"),
                    sampleIntervalMs = c.int("capture.sample_interval_s") * 1000L,
                    keyframeIntervalMs = c.int("capture.keyframe_interval_s") * 1000L,
                    maxUploadsPerHour = c.int("capture.max_uploads_per_hour"),
                    cropMargin = c.double("capture.crop_margin"),
                    changeFrac = c.double("detector.change_frac"),
                    detector = ChangeDetector.Params.from(c),
                    liveIntervalMs = c.int("live.interval_s") * 1000L,
                    armed = c.mode == "armed",
                    zones = Geometry.parseZones(c.zones),
                    configVersion = c.configVersion,
                )
            }
        }
    }

    data class Request(val kind: String, val commandId: String?)

    class Captured(
        val kind: String,
        val commandId: String?,
        val jpeg: ByteArray,
        val rotation: Int,
        val width: Int,
        val height: Int,
        val takenAt: Instant,
        val zoom: Float,
        val configVersion: Int,
        val cropRect: NormRect? = null,
        val diff: Double? = null,
    )

    private val executor = Executors.newSingleThreadExecutor()
    private val requests = ConcurrentLinkedQueue<Request>()
    private var provider: ProcessCameraProvider? = null
    private var camera: Camera? = null

    @Volatile private var params: Params? = null
    @Volatile private var boundAt = 0L

    // Состояние выборки — только в потоке анализатора.
    private val detector = ChangeDetector()
    private var lastSampleAt = 0L
    private var lastKeyframeAt = 0L
    private var lastLiveAt = 0L
    private val changeUploads = ArrayDeque<Long>()
    private var lightJumps = 0
    private var maskKey: Any? = null
    private var mask: BooleanArray? = null
    private var yCopy = ByteArray(0)

    /** Доп. кадры, которые попросил сервер (docs/05-backend.md §3). */
    @Volatile var extraFrames = 0

    /** Живой режим до этого момента (epoch ms). */
    @Volatile var liveUntilMs = 0L

    /** elapsedRealtime последнего кадра от камеры — для last_frame_age_s. */
    @Volatile var lastFrameAt = 0L
        private set

    var caps: CameraCaps? = null
        private set

    val liveActive get() = System.currentTimeMillis() < liveUntilMs

    /** Вызывать с главного потока. */
    suspend fun start(p: Params) {
        params = p
        provider = ProcessCameraProvider.getInstance(ctx).await()
        bind()
    }

    /** Применить новые настройки: разрешение и фокус требуют перезапуска камеры, zoom — нет. Главный поток. */
    fun apply(p: Params) {
        val old = params
        params = p
        if (old == null || provider == null) return
        if (old.width != p.width || old.height != p.height || old.focusInfinity != p.focusInfinity) {
            bind()
        } else if (old.zoom != p.zoom) {
            setZoom(p.zoom)
        }
    }

    fun restart() {
        if (provider != null) bind()
    }

    /** Заново получить провайдер камеры (если обычный перезапуск не помог). Главный поток. */
    suspend fun recreate() {
        provider?.unbindAll()
        provider = ProcessCameraProvider.getInstance(ctx).await()
        bind()
    }

    fun request(kind: String, commandId: String?) {
        requests.add(Request(kind, commandId))
    }

    fun stop() {
        provider?.unbindAll()
        executor.shutdown()
    }

    @OptIn(ExperimentalCamera2Interop::class)
    private fun bind() {
        val prov = provider ?: return
        val p = params ?: return
        prov.unbindAll()

        val selector = CameraSelector.DEFAULT_BACK_CAMERA
        val info = prov.availableCameraInfos.firstOrNull { selector.filter(listOf(it)).isNotEmpty() }
        val chars = info?.let { Camera2CameraInfo.from(it) }

        val builder = ImageAnalysis.Builder()
            .setResolutionSelector(
                ResolutionSelector.Builder()
                    // Без явного соотношения CameraX для анализа предпочитает 4:3 и не выбирает 1920x1080.
                    .setAspectRatioStrategy(aspectFor(p.width, p.height))
                    .setResolutionStrategy(
                        ResolutionStrategy(Size(p.width, p.height), ResolutionStrategy.FALLBACK_RULE_CLOSEST_LOWER_THEN_HIGHER),
                    )
                    .build(),
            )
            .setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST)
            .setOutputImageFormat(ImageAnalysis.OUTPUT_IMAGE_FORMAT_YUV_420_888)
            .setTargetRotation(displayRotation())
        val ext = Camera2Interop.Extender(builder)
        // Экономия: наименьшая нижняя граница FPS из поддерживаемых.
        chars?.getCameraCharacteristic(CameraCharacteristics.CONTROL_AE_AVAILABLE_TARGET_FPS_RANGES)
            ?.minWithOrNull(compareBy<Range<Int>> { it.lower }.thenBy { it.upper })
            ?.let { ext.setCaptureRequestOption(CaptureRequest.CONTROL_AE_TARGET_FPS_RANGE, it) }
        if (p.focusInfinity) {
            ext.setCaptureRequestOption(CaptureRequest.CONTROL_AF_MODE, CameraMetadata.CONTROL_AF_MODE_OFF)
            ext.setCaptureRequestOption(CaptureRequest.LENS_FOCUS_DISTANCE, 0f)
        }

        val analysis = builder.build()
        analysis.setAnalyzer(executor, ::analyze)
        val cam = prov.bindToLifecycle(owner, selector, analysis)
        camera = cam
        boundAt = SystemClock.elapsedRealtime()
        caps = collectCaps(cam)
        setZoom(p.zoom)
        Log.i(TAG, "camera bound: ${analysis.resolutionInfo?.resolution} rotation=${displayRotation()}")
    }

    private fun setZoom(zoom: Float) {
        val cam = camera ?: return
        val z = cam.cameraInfo.zoomState.value
        val clamped = if (z != null) zoom.coerceIn(z.minZoomRatio, z.maxZoomRatio) else zoom
        cam.cameraControl.setZoomRatio(clamped)
    }

    private fun aspectFor(w: Int, h: Int): AspectRatioStrategy {
        val r = maxOf(w, h).toFloat() / minOf(w, h)
        val ratio = if (abs(r - 16f / 9f) < abs(r - 4f / 3f)) AspectRatio.RATIO_16_9 else AspectRatio.RATIO_4_3
        return AspectRatioStrategy(ratio, AspectRatioStrategy.FALLBACK_RULE_AUTO)
    }

    private fun displayRotation(): Int =
        ctx.getSystemService(DisplayManager::class.java).getDisplay(Display.DEFAULT_DISPLAY)?.rotation ?: 0

    private fun analyze(image: ImageProxy) {
        try {
            sample(image)
        } catch (e: Exception) {
            Log.e(TAG, "analyze failed", e)
        } finally {
            image.close()
        }
    }

    private fun sample(image: ImageProxy) {
        val now = SystemClock.elapsedRealtime()
        lastFrameAt = now
        val p = params ?: return
        // Первые кадры после запуска камеры тёмные: автоэкспозиция ещё не подстроилась.
        if (now - boundAt < WARMUP_MS) return

        requests.poll()?.let { req ->
            emit(image, p, req.kind, req.commandId)
            return
        }
        if (liveActive && now - lastLiveAt >= p.liveIntervalMs) {
            lastLiveAt = now
            emit(image, p, "snapshot", null)
            return
        }
        if (now - lastSampleAt < p.sampleIntervalMs) return
        lastSampleAt = now

        val rotation = image.imageInfo.rotationDegrees
        val thumb = thumbnail(image)
        val m = maskFor(p, thumb.size, image.width, image.height, rotation)

        when {
            now - lastKeyframeAt >= p.keyframeIntervalMs -> {
                lastKeyframeAt = now
                emit(image, p, "keyframe", null)
                detector.setBase(thumb)
            }
            extraFrames > 0 -> {
                extraFrames--
                emitChange(image, p, rotation, null)
                detector.setBase(thumb)
            }
            !detector.hasBase() -> detector.setBase(thumb)
            p.armed -> {
                val r = detector.compare(thumb, m, p.detector)
                Log.d(TAG, "sample: $r")
                lightJumps = if (r is ChangeDetector.Result.LightJump) lightJumps + 1 else 0
                // Скачок освещения держится несколько выборок — это не вспышка, а новое освещение
                // (включили свет, фары остановившейся машины): отправить кадр и принять новую базу.
                val lightChanged = lightJumps >= LIGHT_JUMP_CONFIRM
                if (lightChanged && underHourlyLimit(now, p)) {
                    lightJumps = 0
                    changeUploads.addLast(now)
                    emitChange(image, p, rotation, null)
                    detector.setBase(thumb)
                } else if (r is ChangeDetector.Result.Diff && r.changed >= p.changeFrac && underHourlyLimit(now, p)) {
                    changeUploads.addLast(now)
                    emitChange(image, p, rotation, r.changed)
                    detector.setBase(thumb)
                } else if (r is ChangeDetector.Result.Diff && r.changed >= p.changeFrac) {
                    Log.w(TAG, "change ${"%.2f".format(r.changed)} skipped: hourly limit")
                }
            }
        }
    }

    private fun underHourlyLimit(now: Long, p: Params): Boolean {
        while (changeUploads.isNotEmpty() && now - changeUploads.first() > HOUR_MS) changeUploads.removeFirst()
        return changeUploads.size < p.maxUploadsPerHour
    }

    private fun thumbnail(image: ImageProxy): IntArray {
        val plane = image.planes[0]
        val buf = plane.buffer
        buf.rewind()
        if (yCopy.size != buf.remaining()) yCopy = ByteArray(buf.remaining())
        buf.get(yCopy)
        val th = ChangeDetector.thumbHeight(image.width, image.height)
        return ChangeDetector.downscale(yCopy, image.width, image.height, plane.rowStride, plane.pixelStride, ChangeDetector.THUMB_W, th)
    }

    private fun maskFor(p: Params, size: Int, w: Int, h: Int, rotation: Int): BooleanArray? {
        val key = listOf(p.zones, size, w, h, rotation)
        if (key != maskKey) {
            val th = ChangeDetector.thumbHeight(w, h)
            mask = Geometry.mask(p.zones, ChangeDetector.THUMB_W, th, rotation)
            maskKey = key
        }
        return mask
    }

    /** Кадр по изменению: вырез вокруг MONITOR (если зоны есть), иначе полный. */
    private fun emitChange(image: ImageProxy, p: Params, rotation: Int, diff: Double?) {
        val bounds = Geometry.monitorBounds(p.zones, p.cropMargin)
        if (bounds == null) {
            emit(image, p, "change", null, diff = diff)
            return
        }
        val (px, norm) = Geometry.crop(bounds, rotation, image.width, image.height)
        emit(image, p, "change", null, Rect(px.left, px.top, px.right, px.bottom), norm, diff)
    }

    private fun emit(
        image: ImageProxy, p: Params, kind: String, commandId: String?,
        crop: Rect? = null, cropNorm: NormRect? = null, diff: Double? = null,
    ) {
        val jpeg = YuvJpeg.encode(image, p.jpegQuality, crop)
        onCaptured(
            Captured(
                kind = kind,
                commandId = commandId,
                jpeg = jpeg,
                rotation = image.imageInfo.rotationDegrees,
                width = image.width,
                height = image.height,
                takenAt = Instant.now(),
                zoom = camera?.cameraInfo?.zoomState?.value?.zoomRatio ?: p.zoom,
                configVersion = p.configVersion,
                cropRect = cropNorm,
                diff = diff,
            ),
        )
    }

    @OptIn(ExperimentalCamera2Interop::class)
    private fun collectCaps(cam: Camera): CameraCaps {
        val info = Camera2CameraInfo.from(cam.cameraInfo)
        val zoom = cam.cameraInfo.zoomState.value
        val map = info.getCameraCharacteristic(CameraCharacteristics.SCALER_STREAM_CONFIGURATION_MAP)
        val sizes = map?.getOutputSizes(ImageFormat.YUV_420_888).orEmpty()
            .filter { it.width in MIN_CAPS_WIDTH..MAX_CAPS_WIDTH }
            .sortedByDescending { it.width * it.height }
            .map { "${it.width}x${it.height}" }
            .distinct()
        val minFocus = info.getCameraCharacteristic(CameraCharacteristics.LENS_INFO_MINIMUM_FOCUS_DISTANCE) ?: 0f
        val capabilities = info.getCameraCharacteristic(CameraCharacteristics.REQUEST_AVAILABLE_CAPABILITIES) ?: IntArray(0)
        val manual = minFocus > 0f && capabilities.contains(CameraMetadata.REQUEST_AVAILABLE_CAPABILITIES_MANUAL_SENSOR)
        val fps = info.getCameraCharacteristic(CameraCharacteristics.CONTROL_AE_AVAILABLE_TARGET_FPS_RANGES).orEmpty()
            .map { listOf(it.lower, it.upper) }
        return CameraCaps(
            cameraId = info.cameraId,
            zoomMin = zoom?.minZoomRatio ?: 1f,
            zoomMax = zoom?.maxZoomRatio ?: 1f,
            resolutions = sizes,
            manualFocus = manual,
            fpsRanges = fps,
            sensorOrientation = info.getCameraCharacteristic(CameraCharacteristics.SENSOR_ORIENTATION) ?: 0,
        )
    }

    private companion object {
        const val TAG = "CameraSampler"
        const val MIN_CAPS_WIDTH = 640
        const val MAX_CAPS_WIDTH = 2560 // больше для анализа раз в несколько секунд не нужно
        const val WARMUP_MS = 2000L
        const val HOUR_MS = 3_600_000L
        const val LIGHT_JUMP_CONFIRM = 2
    }
}
