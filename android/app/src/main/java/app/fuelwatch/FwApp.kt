package app.fuelwatch

import android.app.Application
import android.content.Context
import kotlinx.coroutines.flow.MutableStateFlow

/** Состояние для домашнего экрана: что показывать и нужна ли повторная привязка. */
data class AppState(
    val status: String = "запуск…",
    val needsPairing: Boolean = false,
    val keepScreenOn: Boolean = true,
)

class FwApp : Application() {
    lateinit var prefs: Prefs
        private set

    val state = MutableStateFlow(AppState())

    override fun onCreate() {
        super.onCreate()
        prefs = Prefs(this)
    }

    companion object {
        fun of(ctx: Context) = ctx.applicationContext as FwApp
    }
}
