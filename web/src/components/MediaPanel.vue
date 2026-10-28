<script setup lang="ts">
import { computed, ref } from 'vue'
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

async function onDrop(event: DragEvent) {
  dragging.value = false
  if (!event.dataTransfer?.files.length) return
  await editor.importFiles(Array.from(event.dataTransfer.files))
}

function onAssetDragStart(event: DragEvent, asset: Asset) {
  if (!event.dataTransfer) return
  event.dataTransfer.setData('application/x-autocut-asset', asset.id)
  event.dataTransfer.setData('text/plain', asset.filename)
  event.dataTransfer.effectAllowed = 'copy'
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
          <span>Video, audio, gambar</span>
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
          draggable="true"
          :title="`${asset.filename} — seret ke timeline atau klik dua kali`"
          @dragstart="onAssetDragStart($event, asset)"
          @dblclick="editor.addAsset(asset)"
        >
          <div class="asset-thumb" :class="asset.kind">
            <img
              v-if="asset.kind !== 'audio' && !brokenThumbs[asset.id]"
              :src="thumbSource(asset)"
              loading="lazy"
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
  background: #0b0b10;
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
  outline: 2px dashed var(--accent);
  outline-offset: -6px;
  background: rgba(124, 108, 255, 0.05);
}

.import-strip {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
  padding: 8px 10px;
  border-radius: 8px;
  background: var(--accent-soft);
  color: var(--accent-strong);
  font-size: 11.5px;
}

.mini-spinner {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  border: 2px solid rgba(124, 108, 255, 0.3);
  border-top-color: var(--accent);
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
  border-radius: 10px;
  border: 1px dashed #333342;
  background: #0c0c11;
  color: var(--muted);
  text-align: left;
  transition: all 0.15s ease;
}

.import-zone:hover {
  border-color: var(--accent);
  color: var(--text);
  background: rgba(124, 108, 255, 0.06);
}

.import-zone strong {
  display: block;
  font-size: 12px;
  color: var(--text);
  font-weight: 600;
}

.import-zone span {
  font-size: 10.5px;
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
  border-radius: 9px;
  background: #0c0c11;
  border: 1px solid var(--border-soft);
  overflow: hidden;
  transition: border-color 0.15s ease, transform 0.15s ease;
}

.asset-card:hover {
  border-color: rgba(124, 108, 255, 0.45);
  transform: translateY(-1px);
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
  background: linear-gradient(135deg, #191924, #101016);
  color: var(--muted);
  overflow: hidden;
}

.asset-thumb.audio {
  background: linear-gradient(135deg, rgba(35, 213, 180, 0.16), #101016);
  color: var(--teal);
}

.asset-thumb.image {
  background: linear-gradient(135deg, rgba(242, 178, 92, 0.14), #101016);
  color: var(--amber);
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
  font-size: 9.5px;
  background: rgba(0, 0, 0, 0.65);
  color: #e7e7f0;
}

.asset-actions {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  background: rgba(4, 4, 8, 0.55);
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
  border-radius: 7px;
  border: 1px solid rgba(255, 255, 255, 0.16);
  background: rgba(255, 255, 255, 0.1);
  color: #fff;
  backdrop-filter: blur(4px);
}

.mini-btn:hover {
  background: var(--accent);
  border-color: var(--accent);
}

.mini-btn.danger:hover {
  background: var(--danger);
  border-color: var(--danger);
}

.asset-name {
  padding: 6px 8px;
  font-size: 10.5px;
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
  border-radius: 12px;
  background: rgba(8, 8, 13, 0.88);
  color: var(--accent-strong);
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
  border-radius: 9px;
  border: 1px solid var(--border-soft);
  background: #0c0c11;
  text-align: left;
  transition: border-color 0.15s ease, background 0.15s ease;
}

.preset:hover {
  border-color: var(--accent);
  background: rgba(124, 108, 255, 0.06);
}

.preset span {
  font-size: 10px;
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
