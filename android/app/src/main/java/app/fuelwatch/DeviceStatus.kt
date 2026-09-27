package app.fuelwatch

import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.os.BatteryManager
import android.os.PowerManager

/** Заряд, питание, температура, нагрев и сеть — для heartbeat. */
object DeviceStatus {
    data class Battery(val percent: Int?, val charging: Boolean?, val tempC: Double?)

    fun battery(ctx: Context): Battery {
        // Липкий broadcast: registerReceiver(null, …) просто возвращает последнее значение.
        val i: Intent = ctx.registerReceiver(null, IntentFilter(Intent.ACTION_BATTERY_CHANGED))
            ?: return Battery(null, null, null)
        val level = i.getIntExtra(BatteryManager.EXTRA_LEVEL, -1)
        val scale = i.getIntExtra(BatteryManager.EXTRA_SCALE, -1)
        val plugged = i.getIntExtra(BatteryManager.EXTRA_PLUGGED, -1)
        val temp = i.getIntExtra(BatteryManager.EXTRA_TEMPERATURE, Int.MIN_VALUE)
        return Battery(
            percent = if (level >= 0 && scale > 0) level * 100 / scale else null,
            charging = if (plugged >= 0) plugged != 0 else null,
            tempC = if (temp != Int.MIN_VALUE) temp / 10.0 else null,
        )
    }

    fun thermalStatus(ctx: Context): Int? = ctx.getSystemService(PowerManager::class.java)?.currentThermalStatus

    fun network(ctx: Context): String? {
        val cm = ctx.getSystemService(ConnectivityManager::class.java) ?: return null
        val caps = cm.getNetworkCapabilities(cm.activeNetwork) ?: return null
        return when {
            caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> "wifi"
            caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> "cellular"
            caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) -> "ethernet"
            else -> "other"
        }
    }
}
