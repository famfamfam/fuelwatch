package app.fuelwatch

import android.graphics.ImageFormat
import android.graphics.Rect
import android.graphics.YuvImage
import androidx.camera.core.ImageProxy
import java.io.ByteArrayOutputStream

/** YUV_420_888 → NV21 → JPEG (docs/04-android-app.md §4). Учитывает rowStride/pixelStride плоскостей. */
object YuvJpeg {
    fun encode(image: ImageProxy, quality: Int, crop: Rect? = null): ByteArray {
        val w = image.width and 1.inv() // NV21 требует чётных размеров
        val h = image.height and 1.inv()
        val nv21 = toNv21(image, w, h)
        val out = ByteArrayOutputStream(w * h / 4)
        YuvImage(nv21, ImageFormat.NV21, w, h, null).compressToJpeg(crop ?: Rect(0, 0, w, h), quality, out)
        return out.toByteArray()
    }

    private fun toNv21(image: ImageProxy, w: Int, h: Int): ByteArray {
        val out = ByteArray(w * h * 3 / 2)
        val y = image.planes[0]
        val yBuf = y.buffer
        var pos = 0
        if (y.pixelStride == 1) {
            for (row in 0 until h) {
                yBuf.position(row * y.rowStride)
                yBuf.get(out, pos, w)
                pos += w
            }
        } else {
            for (row in 0 until h) {
                val base = row * y.rowStride
                for (col in 0 until w) out[pos++] = yBuf.get(base + col * y.pixelStride)
            }
        }
        // NV21: после Y идут чередующиеся V и U с половинным разрешением.
        val u = image.planes[1]
        val v = image.planes[2]
        val uBuf = u.buffer
        val vBuf = v.buffer
        for (row in 0 until h / 2) {
            val uBase = row * u.rowStride
            val vBase = row * v.rowStride
            for (col in 0 until w / 2) {
                out[pos++] = vBuf.get(vBase + col * v.pixelStride)
                out[pos++] = uBuf.get(uBase + col * u.pixelStride)
            }
        }
        return out
    }
}
