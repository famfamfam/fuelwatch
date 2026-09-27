package app.fuelwatch

import android.content.Context
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.first
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.builtins.serializer
import kotlinx.serialization.json.JsonObject

private val Context.dataStore by preferencesDataStore("fuelwatch")

/** Хранилище на телефоне: адрес сервера, токен, последний конфиг, выполненные команды. */
class Prefs(private val ctx: Context) {
    private object K {
        val server = stringPreferencesKey("server_url")
        val token = stringPreferencesKey("token")
        val deviceId = stringPreferencesKey("device_id")
        val config = stringPreferencesKey("config_json")
        val doneIds = stringPreferencesKey("done_command_ids")
        val pendingDone = stringPreferencesKey("pending_command_results")
        val baseline = stringPreferencesKey("motion_baseline")
    }

    data class Pairing(val server: String, val token: String, val deviceId: String)

    private suspend fun data() = ctx.dataStore.data.first()

    suspend fun pairing(): Pairing? {
        val d = data()
        val s = d[K.server] ?: return null
        val t = d[K.token] ?: return null
        return Pairing(s, t, d[K.deviceId] ?: "")
    }

    suspend fun lastServer(): String? = data()[K.server]

    /** Новая привязка: сбрасывает конфиг и историю команд прежнего устройства. */
    suspend fun savePairing(p: Pairing) {
        ctx.dataStore.edit {
            it[K.server] = p.server
            it[K.token] = p.token
            it[K.deviceId] = p.deviceId
            it.remove(K.config)
            it.remove(K.doneIds)
            it.remove(K.pendingDone)
            it.remove(K.baseline)
        }
    }

    suspend fun clearToken() {
        ctx.dataStore.edit { it.remove(K.token) }
    }

    suspend fun config(): DeviceConfig? {
        val raw = data()[K.config] ?: return null
        return runCatching { DeviceConfig.parse(Api.json.decodeFromString(JsonObject.serializer(), raw)) }.getOrNull()
    }

    suspend fun saveConfig(raw: JsonObject) {
        ctx.dataStore.edit { it[K.config] = raw.toString() }
    }

    /** id выполненных команд — повторно не выполняются (docs/04-android-app.md §8). */
    suspend fun doneIds(): List<String> = decodeList(data()[K.doneIds])

    suspend fun addDoneId(id: String) {
        ctx.dataStore.edit {
            val ids = (decodeList(it[K.doneIds]) + id).takeLast(MAX_DONE_IDS)
            it[K.doneIds] = Api.json.encodeToString(stringList, ids)
        }
    }

    /** Результаты команд, ещё не отправленные в heartbeat (переживают перезапуск процесса). */
    suspend fun pendingResults(): List<CommandDone> =
        data()[K.pendingDone]?.let { runCatching { Api.json.decodeFromString(resultList, it) }.getOrNull() } ?: emptyList()

    suspend fun savePendingResults(list: List<CommandDone>) {
        ctx.dataStore.edit { it[K.pendingDone] = Api.json.encodeToString(resultList, list) }
    }

    /** Базовый вектор гравитации (запомнен при arm) — переживает перезапуск процесса. */
    suspend fun motionBaseline(): DoubleArray? =
        data()[K.baseline]?.split(',')?.mapNotNull { it.toDoubleOrNull() }?.takeIf { it.size == 3 }?.toDoubleArray()

    suspend fun saveMotionBaseline(v: DoubleArray) {
        ctx.dataStore.edit { it[K.baseline] = v.joinToString(",") }
    }

    private fun decodeList(raw: String?): List<String> =
        raw?.let { runCatching { Api.json.decodeFromString(stringList, it) }.getOrNull() } ?: emptyList()

    private companion object {
        const val MAX_DONE_IDS = 100
        val stringList = ListSerializer(String.serializer())
        val resultList = ListSerializer(CommandDone.serializer())
    }
}
