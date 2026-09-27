package app.fuelwatch

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Color
import android.os.Bundle
import android.view.Gravity
import android.view.WindowManager
import android.widget.FrameLayout
import android.widget.TextView
import androidx.activity.ComponentActivity
import androidx.activity.addCallback
import androidx.core.content.ContextCompat
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.lifecycleScope
import androidx.lifecycle.repeatOnLifecycle
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlin.random.Random

/**
 * Домашний экран (D-03): чёрный, минимальная яркость, мелкий статус, который смещается против выгорания.
 * Пока приложение в foreground, Android не ограничивает камеру и сервис.
 * Долгое нажатие на статус открывает настройку (адрес сервера, разрешения).
 */
class HomeActivity : ComponentActivity() {
    private lateinit var root: FrameLayout
    private lateinit var status: TextView

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        root = FrameLayout(this).apply { setBackgroundColor(Color.BLACK) }
        status = TextView(this).apply {
            setTextColor(STATUS_COLOR)
            textSize = 12f
            setOnLongClickListener {
                openSetup()
                true
            }
        }
        root.addView(status, FrameLayout.LayoutParams(FrameLayout.LayoutParams.WRAP_CONTENT, FrameLayout.LayoutParams.WRAP_CONTENT, Gravity.TOP or Gravity.START))
        setContentView(root)

        window.attributes = window.attributes.apply { screenBrightness = MIN_BRIGHTNESS }
        WindowCompat.getInsetsController(window, root).apply {
            hide(WindowInsetsCompat.Type.systemBars())
            systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        }
        // Это домашний экран: «Назад» ничего не делает.
        onBackPressedDispatcher.addCallback(this) {}

        lifecycleScope.launch {
            repeatOnLifecycle(Lifecycle.State.STARTED) {
                val app = FwApp.of(this@HomeActivity)
                if (app.prefs.pairing() == null || !hasCamera()) {
                    openSetup()
                    return@repeatOnLifecycle
                }
                app.state.value = app.state.value.copy(needsPairing = false)
                MonitorService.start(this@HomeActivity)
                launch {
                    app.state.collect { s ->
                        status.text = s.status
                        if (s.keepScreenOn) {
                            window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
                        } else {
                            window.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
                        }
                        if (s.needsPairing) openSetup()
                    }
                }
                launch {
                    while (true) {
                        moveStatus()
                        delay(STATUS_MOVE_MS)
                    }
                }
            }
        }
    }

    private fun hasCamera() =
        ContextCompat.checkSelfPermission(this, Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED

    private fun openSetup() {
        startActivity(Intent(this, SetupActivity::class.java))
    }

    private fun moveStatus() {
        val w = root.width - status.width
        val h = root.height - status.height
        if (w > 0 && h > 0) {
            status.translationX = Random.nextInt(w).toFloat()
            status.translationY = Random.nextInt(h).toFloat()
        }
    }

    private companion object {
        const val MIN_BRIGHTNESS = 0.01f
        const val STATUS_MOVE_MS = 60_000L
        val STATUS_COLOR = Color.rgb(90, 90, 90)
    }
}
