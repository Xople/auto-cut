<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { Film, Image as ImageIcon, Music, Plus, Trash2, Type, Upload, Wand2 } from 'lucide-vue-next'
import { thumbUrl } from '../api'
import { useEditor } from '../stores/editor'
import { defaultTextStyle, type Asset, type TextStyle } from '../types'
import { formatSecondsShort } from '../lib/format'

const editor = useEditor()
const tab = ref<'media' | 'text'>('media')
const fileInput = ref<HTMLInputElement | null>(null)
const dragging = ref(false)
const textDraft = ref('')
const brokenThumbs = ref<Record<string, boolean>>({})
const dragGhost = ref<{ asset: Asset; x: number; y: number } | null>(null)

const projectId = computed(() => editor.project?.id ?? '')

function pickFiles() {
  fileInput.value?.click()
}

async function onFilesPicked(event: Event) {
  const input = event.target as HTMLInputElement
  if (!input.files) return
  const files = Array.from(input.files)
  input.value = ''
  await editor.importFiles(files)
}

function filesFromDrop(event: DragEvent): File[] {
  const transfer = event.dataTransfer
  if (!transfer) return []
  if (transfer.files.length) return Array.from(transfer.files)
  return Array.from(transfer.items)
    .filter((item) => item.kind === 'file')
    .map((item) => item.getAsFile())
    .filter((file): file is File => !!file)
}

async function onDrop(event: DragEvent) {
  dragging.value = false
  const files = filesFromDrop(event)
  if (files.length) await editor.importFiles(files)
}

function preventWindowDrop(event: DragEvent) {
  event.preventDefault()
}

onMounted(() => {
  window.addEventListener('dragover', preventWindowDrop)
  window.addEventListener('drop', preventWindowDrop)
})

onBeforeUnmount(() => {
  window.removeEventListener('dragover', preventWindowDrop)
  window.removeEventListener('drop', preventWindowDrop)
  cancelAssetDrag()
})

let press: { asset: Asset; x: number; y: number } | null = null

function onAssetPointerDown(event: PointerEvent, asset: Asset) {
  if (event.button !== 0) return
  if ((event.target as HTMLElement).closest('button')) return
  press = { asset, x: event.clientX, y: event.clientY }
  window.addEventListener('pointermove', onAssetPointerMove)
  window.addEventListener('pointerup', onAssetPointerUp)
  window.addEventListener('pointercancel', cancelAssetDrag)
}

function onAssetPointerMove(event: PointerEvent) {
  if (!press) return
  if (!dragGhost.value && Math.hypot(event.clientX - press.x, event.clientY - press.y) < 5) return
  dragGhost.value = { asset: press.asset, x: event.clientX, y: event.clientY }
  document.body.classList.add('asset-dragging')
}

function onAssetPointerUp(event: PointerEvent) {
  const asset = press?.asset
  const wasDragging = !!dragGhost.value
  cancelAssetDrag()
  if (!wasDragging || !asset) return
  const element = document.elementFromPoint(event.clientX, event.clientY) as HTMLElement | null
  const lane =
    (element?.closest('.lane') as HTMLElement | null) ??
    (document
      .elementsFromPoint(event.clientX, event.clientY)
      .map((item) => item.closest('.lane') as HTMLElement | null)
      .find((item): item is HTMLElement => !!item) ??
      null)
  const content = document.querySelector('.timeline-content') as HTMLElement | null
  const left = content?.getBoundingClientRect().left ?? 0
  const pps = 44 * editor.zoom
  const time = Math.max(0, (event.clientX - left) / pps)
  if (!lane) {
    if (asset.kind === 'video') dropOnNewVideoLayer(asset, time, event)
    return
  }
  const trackId = lane.dataset.trackId
  if (!trackId) return
  const track = editor.project?.timeline.tracks.find((item) => item.id === trackId)
  if (!track || track.locked) return
  const wanted = asset.kind === 'audio' ? 'audio' : asset.kind === 'image' ? 'overlay' : 'video'
  if (track.kind !== wanted) {
    editor.notify(`Asset ${asset.kind} tidak cocok untuk track ${track.name}.`, 'error')
    return
  }
  editor.addAsset(asset, time, track.id)
}

// Dropping above the top video lane means "new layer on top".
function dropOnNewVideoLayer(asset: Asset, time: number, event: PointerEvent) {
  const content = document.querySelector('.timeline-content') as HTMLElement | null
  if (!content) return
  const rect = content.getBoundingClientRect()
  if (event.clientX < rect.left || event.clientX > rect.right) return
  const videoLane = [...document.querySelectorAll('.lane')].find(
    (item) => {
      const track = editor.project?.timeline.tracks.find((t) => t.id === (item as HTMLElement).dataset.trackId)
      return track?.kind === 'video'
    },
  ) as HTMLElement | undefined
  if (!videoLane) return
  const laneRect = videoLane.getBoundingClientRect()
  if (event.clientY < rect.top || event.clientY > laneRect.top) return
  editor.addAssetOnNewTrack(asset, time)
}

function cancelAssetDrag() {
  press = null
  dragGhost.value = null
  document.body.classList.remove('asset-dragging')
  window.removeEventListener('pointermove', onAssetPointerMove)
  window.removeEventListener('pointerup', onAssetPointerUp)
  window.removeEventListener('pointercancel', cancelAssetDrag)
}

function assetIcon(kind: Asset['kind']) {
  return kind === 'audio' ? Music : kind === 'image' ? ImageIcon : Film
}

function thumbSource(asset: Asset): string {
  return thumbUrl(projectId.value, asset.id)
}

function addText(stylePatch: Partial<TextStyle> = {}) {
  const text = textDraft.value.trim() || 'Teks baru'
  editor.addTextOverlay(text)
  const selected = editor.selected
  if (selected && selected.track.kind === 'overlay' && selected.clip.text === text) {
    editor.updateClip(selected.track.id, selected.clip.id, {
      style: { ...defaultTextStyle(), ...stylePatch },
    })
  }
  textDraft.value = ''
}

function addPreset(patch: Partial<TextStyle>, sample: string) {
  textDraft.value = sample
  addText(patch)
}
</script>

<template>
  <aside class="media-panel">
    <div class="panel-head">
      <div class="tabs">
        <button class="tab" :class="{ active: tab === 'media' }" @click="tab = 'media'">Media</button>
        <button class="tab" :class="{ active: tab === 'text' }" @click="tab = 'text'">Teks</button>
      </div>
      <button v-if="tab === 'media'" class="icon-btn" title="Impor media" @click="pickFiles">
        <Plus :size="16" />
      </button>
    </div>

    <input
      ref="fileInput"
      type="file"
      multiple
      hidden
      accept="video/*,audio/*,image/*,.mkv,.mov,.avi,.webm,.m4v,.wav,.mp3,.m4a,.aac,.flac,.ogg"
      @change="onFilesPicked"
    />

    <div
      v-if="tab === 'media'"
      class="panel-body"
      :class="{ dragging }"
      @dragover.prevent="dragging = true"
      @dragleave.prevent="dragging = false"
      @drop.prevent="onDrop"
    >
      <div v-if="editor.importing" class="import-strip">
        <div class="mini-spinner" />
        Mengimpor media...
      </div>

      <button class="import-zone" @click="pickFiles">
        <Upload :size="18" />
        <div>
          <strong>Impor media</strong>
          <span>Klik atau tarik file ke panel ini</span>
        </div>
      </button>

      <div v-if="editor.assets.length === 0" class="empty-state">
        <div class="empty-illustration">
          <Film :size="20" />
          <Music :size="20" />
          <ImageIcon :size="20" />
        </div>
        Tarik file ke sini atau klik impor.<br />
        Semua file disalin ke folder project.
      </div>

      <div v-else class="asset-grid">
        <div
          v-for="asset in editor.assets"
          :key="asset.id"
          class="asset-card"
          :title="`${asset.filename} — seret ke timeline atau klik dua kali`"
          @pointerdown="onAssetPointerDown($event, asset)"
          @dblclick="editor.addAsset(asset)"
        >
          <div class="asset-thumb" :class="asset.kind">
            <img
              v-if="asset.kind !== 'audio' && !brokenThumbs[asset.id]"
              :src="thumbSource(asset)"
              loading="lazy"
              draggable="false"
              @error="brokenThumbs[asset.id] = true"
            />
            <div v-else class="asset-icon">
              <component :is="assetIcon(asset.kind)" :size="22" />
            </div>
            <span class="asset-duration">{{ formatSecondsShort(asset.duration) }}</span>
            <div class="asset-actions">
              <button class="mini-btn" title="Tambah ke timeline" @click.stop="editor.addAsset(asset)">
                <Plus :size="13" />
              </button>
              <button class="mini-btn danger" title="Hapus asset" @click.stop="editor.removeAsset(asset)">
                <Trash2 :size="13" />
              </button>
            </div>
          </div>
          <div class="asset-name">{{ asset.filename }}</div>
        </div>
      </div>

      <div v-if="dragging" class="drop-overlay">
        <Upload :size="26" />
        <span>Lepaskan file untuk impor</span>
      </div>
    </div>

    <div v-else class="panel-body text-tab">
      <div class="field">
        <label class="field-label">Isi teks overlay</label>
        <textarea v-model="textDraft" class="input" rows="3" placeholder="Tulis teks judul, kutipan, atau catatan..." />
      </div>
      <button class="btn btn-primary full" @click="addText()">
        <Type :size="15" />
        Tambah teks ke timeline
      </button>

      <div class="section-title" style="margin-top: 18px">Preset cepat</div>
      <div class="text-presets">
        <button
          class="preset"
          @click="addPreset({ font_size: 118, bold: true, stroke_width: 6, uppercase: true }, 'JUDUL BESAR')"
        >
          <strong class="preset-title">JUDUL BESAR</strong>
          <span>Impact · stroke tebal</span>
        </button>
        <button
          class="preset"
          @click="addPreset({ font_size: 64, bold: false, stroke_width: 3 }, 'Subtitle penjelas di bawah')"
        >
          <strong class="preset-sub">Subtitle</strong>
          <span>Ringan · mudah dibaca</span>
        </button>
        <button
          class="preset"
          @click="
            addPreset(
              { font_size: 84, bold: true, color: '#0B0B10', background_color: '#FFD400', background_alpha: 1, stroke_width: 0, shadow: 0 },
              'HIGHLIGHT PENTING',
            )
          "
        >
          <strong class="preset-highlight">HIGHLIGHT</strong>
          <span>Kotak kuning · tanpa stroke</span>
        </button>
      </div>

      <div class="hint" style="margin-top: 14px">
        Overlay teks ikut tampil di preview dan dibakar ke video saat export. Blend mode tersedia di panel Properti.
      </div>
      <button class="btn full" style="margin-top: 12px" @click="editor.addTextOverlay('Catatan editor')">
        <Wand2 :size="14" />
        Tambah cepat di playhead
      </button>
    </div>

    <Teleport to="body">
      <div
        v-if="dragGhost"
        class="asset-drag-ghost"
        :style="{ left: `${dragGhost.x + 14}px`, top: `${dragGhost.y + 12}px` }"
      >
        <component :is="assetIcon(dragGhost.asset.kind)" :size="14" />
        <span>{{ dragGhost.asset.filename }}</span>
      </div>
    </Teleport>
  </aside>
</template>

<style scoped>
.media-panel {
  display: flex;
  flex-direction: column;
  min-height: 0;
  background: var(--panel);
}

.panel-head {
  height: 46px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 10px 0 12px;
  border-bottom: 1px solid var(--border-soft);
}

.tabs {
  display: flex;
  gap: 2px;
  background: #0d0d0d;
  padding: 3px;
  border-radius: 8px;
  border: 1px solid var(--border-soft);
}

.tab {
  height: 26px;
  padding: 0 13px;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: var(--muted);
  font-size: 11.5px;
  font-weight: 600;
}

.tab.active {
  background: var(--panel-3);
  color: var(--text);
}

.panel-body {
  position: relative;
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 12px;
}

.panel-body.dragging {
  outline: 2px dashed #6b6b6b;
  outline-offset: -6px;
  background: rgba(255, 255, 255, 0.02);
}

.import-strip {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
  padding: 8px 10px;
  border-radius: 8px;
  background: var(--panel-2);
  color: var(--muted);
  font-size: 12px;
}

.mini-spinner {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  border: 2px solid #3a3a3a;
  border-top-color: var(--text);
  animation: spin 0.7s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

.import-zone {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 11px;
  padding: 11px 12px;
  margin-bottom: 13px;
  border-radius: 8px;
  border: 1px dashed #3a3a3a;
  background: #0d0d0d;
  color: var(--muted);
  text-align: left;
  transition: border-color 0.15s ease, background 0.15s ease;
}

.import-zone:hover {
  border-color: #6b6b6b;
  color: var(--text);
  background: var(--panel-2);
}

.import-zone strong {
  display: block;
  font-size: 12px;
  color: var(--text);
  font-weight: 600;
}

.import-zone span {
  font-size: 11px;
  color: var(--muted-2);
}

.empty-illustration {
  display: flex;
  gap: 10px;
  color: var(--muted-2);
  margin-bottom: 4px;
}

.asset-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 9px;
}

.asset-card {
  cursor: grab;
  touch-action: none;
  user-select: none;
  border-radius: 8px;
  background: #0d0d0d;
  border: 1px solid var(--border-soft);
  overflow: hidden;
  transition: border-color 0.15s ease;
}

.asset-drag-ghost {
  position: fixed;
  z-index: 2000;
  display: flex;
  align-items: center;
  gap: 6px;
  max-width: 280px;
  padding: 6px 10px;
  border-radius: 6px;
  border: 1px solid var(--accent);
  background: #0d0d0d;
  color: var(--text);
  font-size: 11.5px;
  white-space: nowrap;
  overflow: hidden;
  pointer-events: none;
  box-shadow: 0 10px 26px rgba(0, 0, 0, 0.55);
}

:global(body.asset-dragging) {
  cursor: grabbing;
  user-select: none;
}

.asset-card:hover {
  border-color: #4a4a4a;
}

.asset-card:active {
  cursor: grabbing;
}

.asset-thumb {
  position: relative;
  height: 76px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--panel-2);
  color: var(--muted);
  overflow: hidden;
}

.asset-thumb.audio,
.asset-thumb.image {
  background: var(--panel-2);
  color: var(--muted);
}

.asset-thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.asset-duration {
  position: absolute;
  right: 5px;
  bottom: 5px;
  padding: 1px 5px;
  border-radius: 4px;
  font-size: 11px;
  background: rgba(0, 0, 0, 0.72);
  color: var(--text);
}

.asset-actions {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  background: rgba(0, 0, 0, 0.72);
  opacity: 0;
  transition: opacity 0.15s ease;
}

.asset-card:hover .asset-actions {
  opacity: 1;
}

.mini-btn {
  width: 26px;
  height: 26px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 6px;
  border: 1px solid #4a4a4a;
  background: var(--panel-3);
  color: var(--text);
}

.mini-btn:hover {
  background: var(--text);
  border-color: var(--text);
  color: #000;
}

.mini-btn.danger:hover {
  background: var(--danger);
  border-color: var(--danger);
  color: var(--text);
}

.asset-name {
  padding: 6px 8px;
  font-size: 11px;
  color: var(--muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.drop-overlay {
  position: absolute;
  inset: 8px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  border-radius: 8px;
  background: rgba(0, 0, 0, 0.9);
  color: var(--text);
  font-weight: 600;
  pointer-events: none;
}

.text-tab {
  display: flex;
  flex-direction: column;
}

.full {
  width: 100%;
}

.text-presets {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.preset {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 3px;
  padding: 10px 12px;
  border-radius: 8px;
  border: 1px solid var(--border-soft);
  background: #0d0d0d;
  text-align: left;
  transition: border-color 0.15s ease, background 0.15s ease;
}

.preset:hover {
  border-color: #4a4a4a;
  background: var(--panel-2);
}

.preset span {
  font-size: 11px;
  color: var(--muted-2);
}

.preset-title {
  font-size: 15px;
  letter-spacing: 0.04em;
  color: var(--text);
}

.preset-sub {
  font-size: 13px;
  font-weight: 500;
  color: var(--text);
}

.preset-highlight {
  font-size: 12px;
  padding: 1px 6px;
  border-radius: 4px;
  background: #ffd400;
  color: #0b0b10;
}
</style>
