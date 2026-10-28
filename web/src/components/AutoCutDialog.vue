<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Scissors, Sparkles, X } from 'lucide-vue-next'
import { api, subscribeTask } from '../api'
import { useEditor } from '../stores/editor'
import { formatSecondsShort } from '../lib/format'
import type { AnalysisResult, Asset, CutSegment, TaskEvent } from '../types'

const props = defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
  (e: 'applied'): void
}>()

const editor = useEditor()

const assetId = ref('')
const useAI = ref(true)
const useQA = ref(true)
const ripple = ref(true)
const running = ref(false)
const progress = ref(0)
const progressMessage = ref('')
const error = ref('')
const cuts = ref<CutSegment[]>([])
const transcriptMock = ref(false)
const transcriptWords = ref(0)
const applyBusy = ref(false)

let unsubscribe: (() => void) | null = null

const voiceAssets = computed<Asset[]>(() =>
  editor.assets.filter((a) => a.kind === 'audio' || (a.kind === 'video' && a.has_audio)),
)

const selectedAsset = computed(() => voiceAssets.value.find((a) => a.id === assetId.value) ?? null)

const checked = ref<Record<string, boolean>>({})

const activeCuts = computed(() => cuts.value.filter((c) => checked.value[c.id]))
const selectedSeconds = computed(() => activeCuts.value.reduce((sum, c) => sum + c.duration, 0))

const grouped = computed(() => {
  const map = new Map<string, CutSegment[]>()
  for (const cut of cuts.value) {
    const key = cut.reason || 'other'
    const list = map.get(key) ?? []
    list.push(cut)
    map.set(key, list)
  }
  return [...map.entries()]
})

function reasonLabel(reason: string): string {
  switch (reason) {
    case 'silence':
      return 'Senyap'
    case 'filler':
      return 'Filler'
    case 'retake':
      return 'Retake'
    case 'off_topic':
      return 'Melenceng'
    case 'keep':
      return 'Simpan'
    default:
      return reason
  }
}

function reasonClass(reason: string): string {
  switch (reason) {
    case 'silence':
      return 'silence'
    case 'filler':
      return 'amber'
    case 'retake':
      return 'danger'
    case 'off_topic':
      return 'accent'
    default:
      return ''
  }
}

function close() {
  emit('update:modelValue', false)
}

function stopStream() {
  if (unsubscribe) {
    unsubscribe()
    unsubscribe = null
  }
}

function hydrateFromAnalysis(result: AnalysisResult) {
  transcriptMock.value = !!result.transcript?.mock
  transcriptWords.value = result.transcript?.words?.length ?? 0
  cuts.value = (result.cuts ?? []).map((cut) => ({ ...cut }))
  const next: Record<string, boolean> = {}
  for (const cut of cuts.value) next[cut.id] = cut.is_cut
  checked.value = next
}

function applyAnalysis(result: AnalysisResult) {
  hydrateFromAnalysis(result)
  editor.analysis = result
  const asset = result.transcript?.asset_id
  if (asset) assetId.value = asset
}

function pickDefaultAsset() {
  const used = new Set<string>()
  for (const track of editor.project?.timeline.tracks ?? []) {
    if (track.kind === 'caption') continue
    for (const clip of track.clips) if (clip.asset_id) used.add(clip.asset_id)
  }
  const preferred = voiceAssets.value.find((a) => used.has(a.id)) ?? voiceAssets.value[0]
  if (preferred) assetId.value = preferred.id
}

watch(
  () => props.modelValue,
  (open) => {
    if (!open) {
      stopStream()
      running.value = false
      return
    }
    error.value = ''
    progress.value = 0
    progressMessage.value = ''
    if (editor.analysis) {
      applyAnalysis(editor.analysis)
      if (!assetId.value) pickDefaultAsset()
    } else {
      cuts.value = []
      checked.value = {}
      transcriptMock.value = false
      transcriptWords.value = 0
      pickDefaultAsset()
    }
  },
)

onBeforeUnmount(stopStream)

function onTaskEvent(event: TaskEvent<AnalysisResult>) {
  progress.value = Math.max(0, Math.min(1, event.progress ?? 0))
  if (event.message) progressMessage.value = event.message
  if (event.status === 'completed') {
    running.value = false
    stopStream()
    if (event.result) {
      applyAnalysis(event.result)
      const count = cuts.value.filter((c) => checked.value[c.id]).length
      editor.notify(`Analisis selesai: ${count} potongan disarankan.`, 'success')
    }
  } else if (event.status === 'failed') {
    running.value = false
    stopStream()
    error.value = event.error || 'Analisis gagal.'
  }
}

async function runAnalyze() {
  if (!editor.project || !assetId.value || running.value) return
  error.value = ''
  cuts.value = []
  checked.value = {}
  running.value = true
  progress.value = 0
  progressMessage.value = 'Menyiapkan analisis...'
  try {
    await api.analyze(editor.project.id, assetId.value, useAI.value, useQA.value)
    stopStream()
    unsubscribe = subscribeTask<AnalysisResult>('analyze', editor.project.id, onTaskEvent)
  } catch (err) {
    running.value = false
    error.value = (err as Error).message
  }
}

function toggleAll(value: boolean) {
  const next: Record<string, boolean> = {}
  for (const cut of cuts.value) next[cut.id] = value
  checked.value = next
}

async function applyCuts() {
  if (!editor.project || !assetId.value) return
  const payload = activeCuts.value
  if (payload.length === 0) {
    editor.notify('Pilih minimal satu potongan untuk diterapkan.', 'info')
    return
  }
  applyBusy.value = true
  try {
    const res = await api.applyCuts(editor.project.id, assetId.value, payload, ripple.value)
    editor.replaceProject(res.project)
    editor.notify(
      `Auto-cut diterapkan: ${formatSecondsShort(res.removed_seconds)} dipotong dari ${res.clips_affected} clip.`,
      'success',
    )
    emit('applied')
    close()
  } catch (err) {
    error.value = (err as Error).message
  } finally {
    applyBusy.value = false
  }
}

const sourceHint = computed(() => {
  const asset = selectedAsset.value
  if (!asset) return 'Impor audio atau video ber-audio terlebih dahulu.'
  return `${asset.filename} · ${asset.kind === 'video' ? 'video' : 'audio'} · ${formatSecondsShort(asset.duration)}`
})
</script>

<template>
  <Teleport to="body">
    <div v-if="modelValue" class="modal-backdrop" @click.self="close">
      <div class="modal autocut-modal">
        <header class="modal-header">
          <div class="modal-title">
            <Scissors :size="17" />
            Auto Cut
            <span class="badge accent">voice-over</span>
          </div>
          <button class="icon-btn" @click="close"><X :size="16" /></button>
        </header>

        <div class="modal-body">
          <div class="field">
            <label class="field-label">Sumber audio</label>
            <select v-model="assetId" class="select" :disabled="running">
              <option value="" disabled>Pilih asset...</option>
              <option v-for="asset in voiceAssets" :key="asset.id" :value="asset.id">
                {{ asset.filename }} ({{ formatSecondsShort(asset.duration) }})
              </option>
            </select>
            <span class="hint">{{ sourceHint }}</span>
          </div>

          <div class="options-row">
            <label class="toggle"><input v-model="useAI" type="checkbox" :disabled="running" /> AI cut (DeepSeek)</label>
            <label class="toggle"><input v-model="useQA" type="checkbox" :disabled="running" /> QA (Gemini)</label>
            <label class="toggle"><input v-model="ripple" type="checkbox" :disabled="running" /> Ripple timeline</label>
          </div>

          <div v-if="running || progress > 0" class="progress-block">
            <div class="progress-head">
              <span>{{ progressMessage || 'Menganalisis...' }}</span>
              <span class="value">{{ Math.round(progress * 100) }}%</span>
            </div>
            <div class="progress-track">
              <div class="progress-fill" :style="{ width: `${progress * 100}%` }" />
            </div>
          </div>

          <div v-if="error" class="error-text">{{ error }}</div>

          <div v-if="transcriptMock" class="warning-box">
            <Sparkles :size="15" style="flex-shrink: 0; margin-top: 1px" />
            <span>
              Whisper CLI tidak terdeteksi, jadi transkrip di bawah adalah <strong>contoh (mock)</strong>. Pasang
              whisper.cpp + model ggml untuk hasil sungguhan.
            </span>
          </div>

          <template v-if="cuts.length">
            <div class="section-title">
              <span>Saran potongan ({{ activeCuts.length }}/{{ cuts.length }})</span>
              <span class="row" style="gap: 6px">
                <button class="btn btn-ghost btn-mini" @click="toggleAll(true)">Semua</button>
                <button class="btn btn-ghost btn-mini" @click="toggleAll(false)">Kosongkan</button>
              </span>
            </div>

            <div class="cut-meta">
              <span class="badge">{{ transcriptWords }} kata transkrip</span>
              <span class="badge teal">{{ formatSecondsShort(selectedSeconds) }}</span>
              <span class="hint">dihapus</span>
            </div>

            <div class="cut-list">
              <div v-for="[reason, list] in grouped" :key="reason" class="cut-group">
                <div class="cut-group-title">
                  <span class="badge" :class="reasonClass(reason)">{{ reasonLabel(reason) }}</span>
                  <span class="hint">{{ list.length }} segmen</span>
                </div>
                <label v-for="cut in list" :key="cut.id" class="cut-row" :class="{ active: checked[cut.id] }">
                  <input v-model="checked[cut.id]" type="checkbox" />
                  <span class="cut-time">{{ formatSecondsShort(cut.start) }} → {{ formatSecondsShort(cut.end) }}</span>
                  <span class="cut-dur">{{ cut.duration.toFixed(2) }}s</span>
                  <span class="cut-text">{{ cut.text || '—' }}</span>
                  <span v-if="cut.needs_review" class="badge amber">review</span>
                </label>
              </div>
            </div>
          </template>

          <div v-else-if="!running" class="empty-state">
            <Scissors :size="22" />
            <span>Belum ada saran potongan.<br />Jalankan analisis untuk mendeteksi senyap, filler, dan retake.</span>
          </div>
        </div>

        <footer class="modal-footer">
          <span class="hint footer-hint">Potongan memakai waktu asset; clip di timeline dipadatkan otomatis.</span>
          <button class="btn" :disabled="running" @click="close">Tutup</button>
          <button class="btn" :disabled="running || !assetId" @click="runAnalyze">
            <Sparkles :size="14" />
            {{ running ? 'Menganalisis...' : cuts.length ? 'Analisis ulang' : 'Analisis' }}
          </button>
          <button class="btn btn-primary" :disabled="applyBusy || running || activeCuts.length === 0" @click="applyCuts">
            <Scissors :size="14" />
            Terapkan ({{ activeCuts.length }})
          </button>
        </footer>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.autocut-modal {
  max-width: 700px;
}

.options-row {
  display: flex;
  align-items: center;
  gap: 16px;
  flex-wrap: wrap;
  margin-bottom: 14px;
}

.progress-block {
  margin: 6px 0 14px;
}

.progress-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 11.5px;
  color: var(--muted);
  margin-bottom: 6px;
}

.progress-head .value {
  color: var(--text);
  font-variant-numeric: tabular-nums;
}

.cut-meta {
  display: flex;
  align-items: center;
  gap: 7px;
  margin-bottom: 10px;
}

.cut-list {
  display: flex;
  flex-direction: column;
  gap: 14px;
  max-height: 320px;
  overflow: auto;
  padding-right: 4px;
}

.cut-group-title {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}

.cut-row {
  display: grid;
  grid-template-columns: auto 96px 46px minmax(0, 1fr) auto;
  align-items: center;
  gap: 9px;
  padding: 6px 8px;
  border-radius: 7px;
  border: 1px solid transparent;
  cursor: pointer;
  font-size: 11.5px;
  color: var(--muted);
}

.cut-row:hover {
  background: var(--panel-2);
}

.cut-row.active {
  background: rgba(255, 93, 108, 0.06);
  border-color: rgba(255, 93, 108, 0.18);
  color: var(--text);
}

.cut-time {
  font-variant-numeric: tabular-nums;
  color: var(--text);
}

.cut-dur {
  font-variant-numeric: tabular-nums;
  color: var(--danger);
  font-size: 10.5px;
}

.cut-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-style: italic;
}

.btn-mini {
  height: 22px;
  padding: 0 8px;
  font-size: 10.5px;
}

.footer-hint {
  margin-right: auto;
  max-width: 300px;
}

.section-title {
  margin-top: 16px;
}
</style>
