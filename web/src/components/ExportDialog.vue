<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Clapperboard, Download, FolderOpen, X } from 'lucide-vue-next'
import { api, exportDownloadUrl, revealExport, subscribeTask } from '../api'
import { useEditor } from '../stores/editor'
import { formatBytes, formatSecondsShort } from '../lib/format'
import type { ExportJob, ExportSettings, TaskEvent } from '../types'

const props = defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{ (e: 'update:modelValue', value: boolean): void }>()

const editor = useEditor()

const preset = ref('source')
const filename = ref('')
const encoder = ref('auto')
const quality = ref(20)
const running = ref(false)
const progress = ref(0)
const message = ref('')
const error = ref('')
const job = ref<ExportJob | null>(null)

let unsubscribe: (() => void) | null = null

const presets = computed(() => {
  const p = editor.project
  if (!p) return []
  const base = [
    { value: 'source', label: `Source (${p.width}×${p.height})`, width: p.width, height: p.height },
    { value: '1080p', label: 'Full HD 1920×1080', width: 1920, height: 1080 },
    { value: '720p', label: 'HD 1280×720', width: 1280, height: 720 },
    { value: 'vertical', label: 'Vertical 1080×1920', width: 1080, height: 1920 },
    { value: 'square', label: 'Square 1080×1080', width: 1080, height: 1080 },
  ]
  return base.filter(
    (item, index, arr) => arr.findIndex((x) => x.width === item.width && x.height === item.height) === index,
  )
})

const resolution = computed(() => {
  const found = presets.value.find((p) => p.value === preset.value)
  return { width: found?.width ?? editor.project?.width ?? 1920, height: found?.height ?? editor.project?.height ?? 1080 }
})

const estimatedBytes = computed(() => {
  const duration = editor.duration
  const bitrate = quality.value <= 16 ? 18_000_000 : quality.value <= 20 ? 12_000_000 : quality.value <= 24 ? 8_000_000 : 5_000_000
  return Math.max(0, (duration * bitrate) / 8)
})

function defaultFilename(): string {
  const name = editor.project?.name ?? 'autocut'
  return name.replace(/[\\/:*?"<>|]+/g, '_').trim().replace(/\s+/g, '_') + '_cut'
}

function stopStream() {
  if (unsubscribe) {
    unsubscribe()
    unsubscribe = null
  }
}

function close() {
  emit('update:modelValue', false)
}

watch(
  () => props.modelValue,
  async (open) => {
    if (!open) {
      stopStream()
      running.value = false
      return
    }
    error.value = ''
    progress.value = 0
    message.value = ''
    job.value = null
    filename.value = defaultFilename()
    preset.value = 'source'
    if (!editor.project) return
    try {
      const latest = await api.latestExport(editor.project.id)
      if (latest.status === 'running') {
        running.value = true
        progress.value = latest.progress / 100
        message.value = latest.message
        resumeStream(latest.id)
      } else if (latest.status === 'completed') {
        job.value = latest
        filename.value = latest.output_name.replace(/\.mp4$/i, '')
      }
    } catch {
      /* belum ada export */
    }
  },
)

onBeforeUnmount(stopStream)

function onTaskEvent(event: TaskEvent<ExportJob>) {
  progress.value = Math.max(0, Math.min(1, event.progress ?? 0))
  if (event.message) message.value = event.message
  if (event.status === 'completed') {
    running.value = false
    stopStream()
    if (event.result) {
      job.value = event.result
      message.value = 'Export selesai.'
    }
    editor.notify('Export selesai. Video siap diunduh.', 'success')
  } else if (event.status === 'failed') {
    running.value = false
    stopStream()
    error.value = event.error || 'Export gagal.'
  }
}

function resumeStream(exportId: string) {
  if (!editor.project) return
  stopStream()
  unsubscribe = subscribeTask<ExportJob>('export', editor.project.id, (event) => {
    if (event.result && event.result.id && event.result.id !== exportId) return
    onTaskEvent(event)
  })
}

async function startExport() {
  if (!editor.project || running.value) return
  error.value = ''
  job.value = null
  progress.value = 0
  running.value = true
  message.value = 'Menyiapkan render...'
  const settings: ExportSettings = {
    width: resolution.value.width,
    height: resolution.value.height,
    fps: editor.project.fps,
    encoder: encoder.value,
    quality: Number(quality.value),
    filename: filename.value || defaultFilename(),
  }
  try {
    const started = await api.startExport(editor.project.id, settings)
    job.value = started
    resumeStream(started.id)
  } catch (err) {
    running.value = false
    error.value = (err as Error).message
  }
}

const downloadHref = computed(() => {
  const p = editor.project
  const j = job.value
  if (!p || !j || j.status !== 'completed') return ''
  return exportDownloadUrl(p.id, j.id)
})

function openInBrowser() {
  if (!downloadHref.value) return
  window.open(downloadHref.value, '_blank')
}

async function revealOutput() {
  const p = editor.project
  const j = job.value
  if (!p || !j) return
  try {
    const shown = await revealExport(p.id, j.output_name)
    if (!shown) openInBrowser()
  } catch (err) {
    editor.notify('Gagal membuka folder export: ' + (err as Error).message, 'error')
  }
}
</script>

<template>
  <Teleport to="body">
    <div v-if="modelValue" class="modal-backdrop" @click.self="close">
      <div class="modal export-modal">
        <header class="modal-header">
          <div class="modal-title">
            <Clapperboard :size="17" />
            Export Video
            <span class="badge teal">mp4 · h264</span>
          </div>
          <button class="icon-btn" @click="close"><X :size="16" /></button>
        </header>

        <div class="modal-body">
          <div class="field">
            <label class="field-label">Nama file</label>
            <div class="filename-row">
              <input v-model="filename" class="input" :disabled="running" spellcheck="false" />
              <span class="filename-ext">.mp4</span>
            </div>
          </div>

          <div class="grid-2">
            <div class="field">
              <label class="field-label">Resolusi</label>
              <select v-model="preset" class="select" :disabled="running">
                <option v-for="item in presets" :key="item.value" :value="item.value">{{ item.label }}</option>
              </select>
            </div>
            <div class="field">
              <label class="field-label">Frame rate</label>
              <input class="input" :value="`${editor.project?.fps ?? 30} fps`" disabled />
            </div>
          </div>

          <div class="grid-2">
            <div class="field">
              <label class="field-label">Encoder</label>
              <select v-model="encoder" class="select" :disabled="running">
                <option value="auto">Auto (AMF → x264)</option>
                <option value="h264_amf">AMD AMF (GPU)</option>
                <option value="h264_nvenc">NVIDIA NVENC</option>
                <option value="libx264">libx264 (CPU)</option>
              </select>
            </div>
            <div class="field">
              <label class="field-label">
                Quality
                <span class="value">CQ {{ quality }}</span>
              </label>
              <input v-model.number="quality" type="range" min="16" max="30" step="1" :disabled="running" />
            </div>
          </div>

          <div class="summary">
            <div class="summary-row"><span>Durasi</span><span>{{ formatSecondsShort(editor.duration) }}</span></div>
            <div class="summary-row">
              <span>Ukuran</span><span>{{ resolution.width }}×{{ resolution.height }}</span>
            </div>
            <div class="summary-row">
              <span>Estimasi</span><span>≈ {{ formatBytes(estimatedBytes) }}</span>
            </div>
          </div>

          <div v-if="running || progress > 0" class="progress-block">
            <div class="progress-head">
              <span>{{ message || 'Merender...' }}</span>
              <span class="value">{{ Math.round(progress * 100) }}%</span>
            </div>
            <div class="progress-track">
              <div class="progress-fill" :style="{ width: `${progress * 100}%` }" />
            </div>
          </div>

          <div v-if="error" class="error-text">{{ error }}</div>

          <div v-if="job && job.status === 'completed'" class="done-box">
            <div class="done-mark">✓</div>
            <div class="done-body">
              <strong>{{ job.output_name }}</strong>
              <span class="hint">Render selesai dalam {{ formatSecondsShort(editor.duration) }} timeline.</span>
            </div>
          </div>

          <div class="hint">
            Export memakai satu filter graph ffmpeg. Caption dibakar via ASS; blend mode caption hanya tampil di
            preview, bukan hasil export.
          </div>
        </div>

        <footer class="modal-footer">
          <button v-if="downloadHref" class="btn btn-ghost" @click="startExport">
            <Clapperboard :size="14" />
            Render ulang
          </button>
          <button v-if="downloadHref" class="btn" @click="revealOutput">
            <FolderOpen :size="14" />
            Buka folder
          </button>
          <a v-if="downloadHref" class="btn btn-primary" :href="downloadHref" :download="job?.output_name">
            <Download :size="14" />
            Unduh MP4
          </a>
          <button v-else class="btn btn-primary" :disabled="running || !editor.project" @click="startExport">
            <Clapperboard :size="14" />
            {{ running ? 'Merender...' : 'Mulai Export' }}
          </button>
        </footer>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.export-modal {
  max-width: 560px;
}

.filename-row {
  display: flex;
  align-items: center;
  gap: 0;
}

.filename-row .input {
  border-top-right-radius: 0;
  border-bottom-right-radius: 0;
}

.filename-ext {
  display: flex;
  align-items: center;
  height: 32px;
  padding: 0 10px;
  border: 1px solid var(--border);
  border-left: none;
  border-radius: 0 8px 8px 0;
  background: var(--panel-2);
  color: var(--muted);
  font-size: 12px;
}

.summary {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 10px 12px;
  border-radius: 9px;
  background: var(--panel-2);
  border: 1px solid var(--border-soft);
  margin: 4px 0 14px;
}

.summary-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 11.5px;
  color: var(--muted);
}

.summary-row span:last-child {
  color: var(--text);
  font-variant-numeric: tabular-nums;
}

.progress-block {
  margin: 4px 0 12px;
}

.progress-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 11.5px;
  color: var(--muted);
  margin-bottom: 6px;
}

.done-box {
  display: flex;
  align-items: center;
  gap: 11px;
  padding: 11px 13px;
  margin: 4px 0 12px;
  border-radius: 10px;
  border: 1px solid rgba(90, 216, 138, 0.25);
  background: rgba(90, 216, 138, 0.07);
}

.done-mark {
  width: 26px;
  height: 26px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(90, 216, 138, 0.16);
  color: var(--green);
  font-weight: 700;
}

.done-body {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.done-body strong {
  font-size: 12.5px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
