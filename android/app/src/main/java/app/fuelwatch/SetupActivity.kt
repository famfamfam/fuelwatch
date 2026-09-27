package app.fuelwatch

import android.Manifest
import android.annotation.SuppressLint
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.PowerManager
import android.provider.Settings
import android.text.InputType
import android.view.View
import android.view.ViewGroup
import android.widget.Button
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import androidx.activity.ComponentActivity
import androidx.activity.result.contract.ActivityResultContracts
import androidx.core.content.ContextCompat
import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.launch

/**
 * Первый запуск (docs/04-android-app.md §9): адрес сервера + код привязки → токен,
 * разрешения, исключение из оптимизации батареи, выбор FuelWatch домашним экраном.
 */
class SetupActivity : ComponentActivity() {
    private lateinit var server: EditText
    private lateinit var code: EditText
    private lateinit var pairInfo: TextView
    private lateinit var permBtn: Button
    private lateinit var batteryBtn: Button
    private lateinit var homeBtn: Button
    private lateinit var doneBtn: Button

    private val permissions = registerForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) { refresh() }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val col = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(24), dp(24), dp(24), dp(24))
        }
        fun add(v: View) = col.addView(v, LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT).apply { topMargin = dp(8) })

        add(TextView(this).apply { text = "FuelWatch — настройка"; textSize = 22f })
        add(TextView(this).apply { text = "1. Привязка к серверу"; textSize = 16f; setPadding(0, dp(16), 0, 0) })
        server = EditText(this).apply {
            hint = "Адрес сервера, например https://fuel.example.com"
            inputType = InputType.TYPE_TEXT_VARIATION_URI
            setSingleLine()
        }
        add(server)
        code = EditText(this).apply {
            hint = "Код привязки из панели (XXXX-XXXX)"
            inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_CAP_CHARACTERS
            setSingleLine()
        }
        add(code)
        add(Button(this).apply { text = "Привязать"; setOnClickListener { pair() } })
        pairInfo = TextView(this)
        add(pairInfo)

        add(TextView(this).apply { text = "2. Разрешения и автозапуск"; textSize = 16f; setPadding(0, dp(16), 0, 0) })
        permBtn = Button(this).apply { setOnClickListener { requestPermissions() } }
        add(permBtn)
        batteryBtn = Button(this).apply { setOnClickListener { requestBatteryExemption() } }
        add(batteryBtn)
        homeBtn = Button(this).apply { setOnClickListener { startActivity(Intent(Settings.ACTION_HOME_SETTINGS)) } }
        add(homeBtn)
        add(TextView(this).apply {
            text = "На Xiaomi, Huawei и подобных дополнительно разрешите «Автозапуск» и работу в фоне в настройках приложения."
            textSize = 13f
        })

        doneBtn = Button(this).apply { text = "Готово — запустить мониторинг"; setOnClickListener { finishSetup() } }
        add(doneBtn)
        add(Button(this).apply { text = "Настройки Android"; setOnClickListener { startActivity(Intent(Settings.ACTION_SETTINGS)) } })

        setContentView(ScrollView(this).apply { addView(col) })

        lifecycleScope.launch {
            val prefs = FwApp.of(this@SetupActivity).prefs
            prefs.lastServer()?.let { server.setText(it) }
            prefs.pairing()?.let { pairInfo.text = "Привязано: ${it.deviceId}. Для перепривязки введите новый код." }
        }
    }

    override fun onResume() {
        super.onResume()
        refresh()
    }

    private fun dp(v: Int) = (v * resources.displayMetrics.density).toInt()

    private fun granted(p: String) = ContextCompat.checkSelfPermission(this, p) == PackageManager.PERMISSION_GRANTED

    private fun neededPermissions(): Array<String> =
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            arrayOf(Manifest.permission.CAMERA, Manifest.permission.POST_NOTIFICATIONS)
        } else {
            arrayOf(Manifest.permission.CAMERA)
        }

    private fun batteryExempt() = getSystemService(PowerManager::class.java).isIgnoringBatteryOptimizations(packageName)

    private fun isDefaultHome(): Boolean {
        val home = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_HOME)
        return packageManager.resolveActivity(home, PackageManager.MATCH_DEFAULT_ONLY)?.activityInfo?.packageName == packageName
    }

    private fun mark(ok: Boolean, label: String) = (if (ok) "✓ " else "") + label

    private fun refresh() {
        val perms = neededPermissions().all(::granted)
        permBtn.text = mark(perms, "Разрешить камеру и уведомления")
        batteryBtn.text = mark(batteryExempt(), "Работа без ограничений батареи")
        homeBtn.text = mark(isDefaultHome(), "Сделать FuelWatch домашним экраном")
    }

    private fun requestPermissions() = permissions.launch(neededPermissions())

    @SuppressLint("BatteryLife") // свои устройства, без Google Play (D-02)
    private fun requestBatteryExemption() {
        if (!batteryExempt()) {
            startActivity(Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, Uri.parse("package:$packageName")))
        }
    }

    private fun normalizeServer(raw: String): String? {
        val s = raw.trim().trimEnd('/')
        return if (s.startsWith("https://") || s.startsWith("http://")) s else null
    }

    private fun pair() {
        val url = normalizeServer(server.text.toString())
        val c = code.text.toString().trim()
        if (url == null) {
            pairInfo.text = "Адрес должен начинаться с https:// (или http:// в локальной сети)"
            return
        }
        if (c.isEmpty()) {
            pairInfo.text = "Введите код привязки"
            return
        }
        pairInfo.text = "Привязываем…"
        lifecycleScope.launch {
            try {
                val r = Api.pair(url, PairRequest(c, "${Build.MANUFACTURER} ${Build.MODEL}", Build.VERSION.RELEASE, BuildConfig.VERSION_NAME))
                val app = FwApp.of(this@SetupActivity)
                app.prefs.savePairing(Prefs.Pairing(url, r.token, r.deviceId))
                app.state.value = app.state.value.copy(needsPairing = false, status = "привязано: ${r.deviceId}")
                // Перезапустить сервис с новым токеном.
                stopService(Intent(this@SetupActivity, MonitorService::class.java))
                code.setText("")
                pairInfo.text = "Привязано: ${r.deviceId}"
            } catch (e: ApiException) {
                pairInfo.text = if (e.error == "invalid_code") "Код не найден или истёк — получите новый в панели" else "Ошибка сервера: ${e.message}"
            } catch (e: Exception) {
                pairInfo.text = "Нет связи с сервером: ${e.message}"
            }
        }
    }

    private fun finishSetup() {
        lifecycleScope.launch {
            val paired = FwApp.of(this@SetupActivity).prefs.pairing() != null
            when {
                !paired -> pairInfo.text = "Сначала привяжите устройство"
                !granted(Manifest.permission.CAMERA) -> requestPermissions()
                else -> {
                    startActivity(Intent(this@SetupActivity, HomeActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP))
                    finish()
                }
            }
        }
    }
}
