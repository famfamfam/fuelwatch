package app.fuelwatch

import java.io.File
import java.util.UUID

/**
 * Очередь на диске (docs/04-android-app.md §7): события и кадры ждут отправки, если нет сети.
 * - Элемент = `{order}_{id}.json` (+ `.jpg` для кадра). order — время добавления: от старых к новым.
 * - Отправка: сначала все события, потом кадры.
 * - Лимит maxBytes: при превышении удаляются самые старые кадры, сначала плановые (keyframe). События не удаляются.
 * Чистый Kotlin/Java IO — unit-тесты на временном каталоге.
 */
class Outbox(private val dir: File, var maxBytes: Long) {
    data class Item(val id: String, val kind: String, val json: File, val jpeg: File?) {
        val isEvent get() = kind == EVENT
        val size get() = json.length() + (jpeg?.length() ?: 0)
    }

    init {
        dir.mkdirs()
        // Недописанные файлы (процесс убит во время записи).
        dir.listFiles { f -> f.name.endsWith(".tmp") }?.forEach { it.delete() }
    }

    @Synchronized
    fun addEvent(json: String): String = add(EVENT, json, null)

    /** kind — тип кадра (keyframe, change, snapshot, reference). */
    @Synchronized
    fun addFrame(kind: String, metaJson: String, jpeg: ByteArray): String {
        val id = add(kind, metaJson, jpeg)
        trim()
        return id
    }

    private fun add(kind: String, json: String, jpeg: ByteArray?): String {
        val id = UUID.randomUUID().toString()
        val base = "${System.currentTimeMillis().toString().padStart(15, '0')}_${kind}_$id"
        if (jpeg != null) write(File(dir, "$base.jpg"), jpeg)
        // json пишется последним: элемент виден в очереди только целиком.
        write(File(dir, "$base.json"), json.toByteArray())
        return id
    }

    private fun write(f: File, bytes: ByteArray) {
        val tmp = File(f.path + ".tmp")
        tmp.writeBytes(bytes)
        if (!tmp.renameTo(f)) {
            tmp.delete()
            error("outbox: cannot write ${f.name}")
        }
    }

    /** Все элементы: события, затем кадры; внутри — от старых к новым. */
    @Synchronized
    fun items(): List<Item> {
        val all = dir.listFiles { f -> f.name.endsWith(".json") }.orEmpty().sortedBy { it.name }.mapNotNull { j ->
            val parts = j.nameWithoutExtension.split('_', limit = 3)
            if (parts.size != 3) return@mapNotNull null
            val jpg = File(dir, j.nameWithoutExtension + ".jpg").takeIf { it.exists() }
            Item(parts[2], parts[1], j, jpg)
        }
        return all.filter { it.isEvent } + all.filterNot { it.isEvent }
    }

    @Synchronized
    fun remove(item: Item) {
        item.jpeg?.delete()
        item.json.delete()
    }

    @Synchronized
    fun sizeBytes(): Long = dir.listFiles().orEmpty().sumOf { it.length() }

    @Synchronized
    fun count(): Int = dir.listFiles { f -> f.name.endsWith(".json") }?.size ?: 0

    private fun trim() {
        var size = sizeBytes()
        if (size <= maxBytes) return
        val frames = items().filterNot { it.isEvent }
        val order = frames.filter { it.kind == "keyframe" } + frames.filterNot { it.kind == "keyframe" }
        for (it in order) {
            if (size <= maxBytes) break
            size -= it.size
            remove(it)
        }
    }

    companion object {
        const val EVENT = "event"
    }
}
