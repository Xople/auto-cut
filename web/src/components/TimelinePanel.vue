<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import {
  Captions,
  Film,
  Layers,
  Lock,
  Magnet,
  Music,
  Scissors,
  Trash2,
  Type,
  Unlock,
  Volume2,
  VolumeX,
  ZoomIn,
  ZoomOut,
} from 'lucide-vue-next'
import { thumbUrl } from '../api'
import { useEditor } from '../stores/editor'
import { clamp, formatTime, snapTime } from '../lib/format'
import Waveform from './Waveform.vue'
import type { Asset, Clip, Track, TrackKind } from '../types'

const editor = useEditor()
const scrollEl = ref<HTMLElement | null>(null)
const contentEl = ref<HTMLElement | null>(null)
const snapping = ref(true)

const LANE = 52
const RULER = 26

const pps = computed(() => 44 * editor.zoom)
const projectId = computed(() => editor.project?.id ?? '')
const tracks = computed(() => editor.project?.timeline.tracks ?? [])

const contentWidth = computed(() => {
  const natural = (editor.duration + 8) * pps.value
  const viewport = scrollEl.value?.clientWidth ?? 900
  return Math.max(natural, viewport + 40)
})

const tickStep = computed(() => {
  const steps = [0.25, 0.5, 1, 2, 5, 10, 15, 30, 60, 120, 300]
  for (const step of steps) {
    if (step * pps.value >= 74) return step
  }
  return 600
})

const ticks = computed(() => {
  const total = contentWidth.value / pps.value
  const step = tickStep.value
  const list: Array<{ time: number; left: number; label: string }> = []
  for (let t = 0; t <= total; t += step) {
    list.push({ time: t, left: t * pps.value, label: tickLabel(t) })
  }
  return list
})

const gridStyle = computed(() => ({
  backgroundSize: `${tickStep.value * pps.value}px 100%`,
}))

function tickLabel(time: number): string {
  const minutes = Math.floor(time / 60)
  const seconds = Math.floor(time % 60)
  if (minutes >= 60) {
    const hours = Math.floor(minutes / 60)
    return `${hours}:${String(minutes % 60).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`
  }
  return `${minutes}:${String(seconds).padStart(2, '0')}`
}

function trackIcon(kind: TrackKind) {
  switch (kind) {
    case 'audio':
      return Music
    case 'overlay':
      return Layers
    case 'caption':
      return Captions
    default:
      return Film
  }
}

function clipAsset(clip: Clip): Asset | undefined {
  return clip.asset_id ? editor.assetMap.get(clip.asset_id) : undefined
}

function clipLabel(clip: Clip): string {
  if (clip.text?.trim()) return clip.text.trim()
  if (clip.words?.length) return clip.words.map((w) => w.word).join(' ')
  return clipAsset(clip)?.filename ?? 'Clip'
}

function clippedThumb(clip: Clip): string {
  const asset = clipAsset(clip)
  if (!asset || asset.kind === 'audio') return ''
  return `url("${thumbUrl(projectId.value, asset.id)}")`
}

function clipStyle(clip: Clip) {
  return {
    left: `${clip.start * pps.value}px`,
    width: `${Math.max(3, clip.duration * pps.value)}px`,
  }
}

// ---- playhead & scrubbing ---------------------------------------------
function timeAt(clientX: number): number {
  const element = contentEl.value
  if (!element) return 0
  const rect = element.getBoundingClientRect()
  return clamp((clientX - rect.left) / pps.value, 0, Math.max(editor.duration, 0.01))
}

function beginScrub(event: PointerEvent) {
  if (event.button !== 0) return
  event.preventDefault()
  editor.seek(timeAt(event.clientX))
  const onMove = (ev: PointerEvent) => editor.seek(timeAt(ev.clientX))
  const onUp = () => {
    window.removeEventListener('pointermove', onMove)
    window.removeEventListener('pointerup', onUp)
  }
  window.addEventListener('pointermove', onMove)
  window.addEventListener('pointerup', onUp)
}

function onLanePointerDown(event: PointerEvent) {
  if (event.button !== 0) return
  editor.clearSelection()
  editor.seek(timeAt(event.clientX))
  beginScrub(event)
}

// ---- clip dragging -----------------------------------------------------
interface DragState {
  mode: 'move' | 'trim-l' | 'trim-r'
  trackId: string
  clipId: string
  startX: number
  origStart: number
  origIn: number
  origOut: number
  origDuration: number
  maxOut: number
}

let drag: DragState | null = null

function candidates(excludeId: string): number[] {
  const list = [0, editor.playhead]
  for (const track of tracks.value) {
    for (const clip of track.clips) {
      if (clip.id === excludeId) continue
      list.push(clip.start, clip.start + clip.duration)
    }
  }
  return list
}

function onClipPointerDown(event: PointerEvent, track: Track, clip: Clip) {
  if (event.button !== 0) return
  event.stopPropagation()
  editor.select(track.id, clip.id)
  if (track.locked) return

  const target = event.target as HTMLElement
  const handle = target.dataset.handle as 'l' | 'r' | undefined
  const asset = clipAsset(clip)
  drag = {
    mode: handle === 'l' ? 'trim-l' : handle === 'r' ? 'trim-r' : 'move',
    trackId: track.id,
    clipId: clip.id,
    startX: event.clientX,
    origStart: clip.start,
    origIn: clip.in,
    origOut: clip.out,
    origDuration: clip.duration,
    maxOut: asset && asset.kind !== 'image' ? asset.duration : Number.POSITIVE_INFINITY,
  }
  editor.pushHistory(`drag-${clip.id}`)
  window.addEventListener('pointermove', onDragMove)
  window.addEventListener('pointerup', onDragEnd, { once: true })
}

function onDragMove(event: PointerEvent) {
  if (!drag) return
  const track = tracks.value.find((t) => t.id === drag!.trackId)
  const clip = track?.clips.find((c) => c.id === drag!.clipId)
  if (!clip) return

  const delta = (event.clientX - drag.startX) / pps.value
  const threshold = 8 / pps.value
  const pool = candidates(drag.clipId)

  if (drag.mode === 'move') {
    let start = Math.max(0, drag.origStart + delta)
    if (snapping.value) {
      const end = start + drag.origDuration
      const snappedEnd = snapTime(end, pool, threshold)
      if (snappedEnd !== end) {
        start = snappedEnd - drag.origDuration
      } else {
        start = snapTime(start, pool, threshold)
      }
    }
    clip.start = Math.max(0, start)
    return
  }

  if (drag.mode === 'trim-l') {
    let newIn = clamp(drag.origIn + delta, 0, drag.origOut - 0.1)
    let newStart = drag.origStart + (newIn - drag.origIn)
    if (newStart < 0) {
      newIn += -newStart
      newStart = 0
    }
    if (snapping.value) {
      const snapped = snapTime(newStart, pool, threshold)
      newIn += snapped - newStart
      newStart = snapped
    }
    newIn = clamp(newIn, 0, drag.origOut - 0.1)
    newStart = Math.max(0, drag.origStart + (newIn - drag.origIn))
    clip.start = newStart
    clip.in = newIn
    clip.out = drag.origOut
    clip.duration = drag.origOut - newIn
    return
  }

  let newOut = clamp(drag.origOut + delta, drag.origIn + 0.1, drag.maxOut)
  if (snapping.value) newOut = clamp(snapTime(newOut, pool, threshold), drag.origIn + 0.1, drag.maxOut)
  clip.out = newOut
  clip.duration = newOut - drag.origIn
}

function onDragEnd() {
  window.removeEventListener('pointermove', onDragMove)
  drag = null
  editor.scheduleSave()
}

onBeforeUnmount(() => {
  window.removeEventListener('pointermove', onDragMove)
  window.removeEventListener('pointerup', onDragEnd)
})

// ---- drag & drop asset -------------------------------------------------
function assetDropKind(asset: Asset): TrackKind {
  return asset.kind === 'audio' ? 'audio' : asset.kind === 'image' ? 'overlay' : 'video'
}

function onLaneDragOver(event: DragEvent, track: Track) {
  if (track.locked) return
  if (!event.dataTransfer?.types.includes('application/x-autocut-asset')) return
  event.preventDefault()
  event.dataTransfer.dropEffect = 'copy'
}

function onLaneDrop(event: DragEvent, track: Track) {
  if (track.locked) return
  const assetId = event.dataTransfer?.getData('application/x-autocut-asset')
  if (!assetId) return
  event.preventDefault()
  const asset = editor.assetMap.get(assetId)
  if (!asset) return
  if (assetDropKind(asset) !== track.kind) {
    editor.notify(`Asset ${asset.kind} tidak cocok untuk track ${track.name}.`, 'error')
    return
  }
  editor.addAsset(asset, Math.max(0, timeAt(event.clientX)), track.id)
}

// ---- toolbar -----------------------------------------------------------
function setZoom(value: number) {
  editor.zoom = clamp(value, 0.2, 4)
}

function fitZoom() {
  const width = scrollEl.value?.clientWidth ?? 900
  const seconds = Math.max(editor.duration + 1, 5)
  editor.zoom = clamp((width - 40) / (seconds * 44), 0.2, 4)
  if (scrollEl.value) scrollEl.value.scrollLeft = 0
}

function onWheel(event: WheelEvent) {
  if (event.ctrlKey || event.metaKey) return
  if (!scrollEl.value) return
  event.preventDefault()
  scrollEl.value.scrollLeft += event.deltaY + event.deltaX
}

watch(
  () => editor.playhead,
  (time) => {
    if (!editor.playing || !scrollEl.value) return
    const x = time * pps.value
    const element = scrollEl.value
    if (x < element.scrollLeft + 30 || x > element.scrollLeft + element.clientWidth - 70) {
      element.scrollLeft = Math.max(0, x - element.clientWidth * 0.3)
    }
  },
)
</script>

<template>
  <section class="timeline-panel">
    <div class="timeline-toolbar">
      <button class="icon-btn" title="Split di playhead (Ctrl+B)" @click="editor.splitAtPlayhead()">
        <Scissors :size="15" />
      </button>
      <button
        class="icon-btn"
        title="Hapus clip terpilih (Del)"
        :disabled="!editor.selectedClip"
        @click="editor.deleteSelected()"
      >
        <Trash2 :size="15" />
      </button>
      <button
        class="icon-btn"
        :class="{ active: snapping }"
        title="Snapping"
        @click="snapping = !snapping"
      >
        <Magnet :size="15" />
      </button>
      <button class="icon-btn" title="Tambah teks di playhead" @click="editor.addTextOverlay('Teks baru')">
        <Type :size="15" />
      </button>

      <div class="toolbar-info">
        <span class="badge">{{ tracks.length }} track</span>
        <span class="badge teal">{{ formatTime(editor.duration) }}</span>
      </div>

      <div class="zoom-block">
        <button class="icon-btn small" @click="setZoom(editor.zoom / 1.25)">
          <ZoomOut :size="14" />
        </button>
        <input
          type="range"
          min="0.2"
          max="4"
          step="0.05"
          :value="editor.zoom"
          @input="setZoom(Number(($event.target as HTMLInputElement).value))"
        />
        <button class="icon-btn small" @click="setZoom(editor.zoom * 1.25)">
          <ZoomIn :size="14" />
        </button>
        <button class="btn btn-ghost fit-btn" @click="fitZoom">{{ Math.round(editor.zoom * 100) }}%</button>
      </div>
    </div>

    <div class="timeline-body">
      <div class="track-headers">
        <div class="ruler-spacer" :style="{ height: RULER + 'px' }" />
        <div v-for="track in tracks" :key="track.id" class="track-head" :style="{ height: LANE + 'px' }">
          <div class="track-kind" :class="track.kind">
            <component :is="trackIcon(track.kind)" :size="13" />
          </div>
          <div class="track-info">
            <div class="track-name">{{ track.name }}</div>
            <div class="track-sub">{{ track.clips.length }} clip</div>
          </div>
          <div class="track-tools">
            <button
              class="icon-btn tiny"
              :class="{ off: track.muted }"
              :title="track.muted ? 'Unmute track' : 'Mute track'"
              @click="editor.setTrackMuted(track.id, !track.muted)"
            >
              <VolumeX v-if="track.muted" :size="12" />
              <Volume2 v-else :size="12" />
            </button>
            <button
              class="icon-btn tiny"
              :class="{ active: track.locked }"
              :title="track.locked ? 'Unlock track' : 'Lock track'"
              @click="editor.setTrackLocked(track.id, !track.locked)"
            >
              <Lock v-if="track.locked" :size="12" />
              <Unlock v-else :size="12" />
            </button>
          </div>
        </div>
      </div>

      <div ref="scrollEl" class="timeline-scroll" @wheel="onWheel">
        <div ref="contentEl" class="timeline-content" :style="{ width: contentWidth + 'px' }">
          <div class="ruler" :style="{ height: RULER + 'px' }" @pointerdown="beginScrub">
            <div
              v-for="tick in ticks"
              :key="tick.time"
              class="tick"
              :style="{ left: tick.left + 'px' }"
            >
              <span>{{ tick.label }}</span>
            </div>
            <div class="playhead-cap" :style="{ left: editor.playhead * pps + 'px' }" />
          </div>

          <div
            v-for="track in tracks"
            :key="track.id"
            class="lane"
            :class="[track.kind, { locked: track.locked }]"
            :style="{ height: LANE + 'px', ...gridStyle }"
            @pointerdown="onLanePointerDown"
            @dragover="onLaneDragOver($event, track)"
            @drop="onLaneDrop($event, track)"
          >
            <div
              v-for="clip in track.clips"
              :key="clip.id"
              class="clip"
              :class="[
                clip.asset_id ? clipAsset(clip)?.kind ?? 'video' : 'text',
                {
                  selected: editor.selection?.clipId === clip.id,
                  muted: track.muted,
                },
              ]"
              :style="clipStyle(clip)"
              @pointerdown="onClipPointerDown($event, track, clip)"
            >
              <div
                v-if="clippedThumb(clip)"
                class="clip-thumbs"
                :style="{ backgroundImage: clippedThumb(clip) }"
              />
              <Waveform
                v-if="clipAsset(clip)?.kind === 'audio'"
                :project-id="projectId"
                :asset-id="clip.asset_id ?? ''"
                :in-sec="clip.in"
                :out-sec="clip.out"
                class="clip-wave"
              />

              <div class="clip-label">
                <span class="clip-name">{{ clipLabel(clip) }}</span>
                <span class="clip-duration">{{ clip.duration.toFixed(2) }}s</span>
              </div>

              <div
                v-if="editor.selection?.clipId === clip.id && !track.locked"
                class="trim-handle left"
                data-handle="l"
              />
              <div
                v-if="editor.selection?.clipId === clip.id && !track.locked"
                class="trim-handle right"
                data-handle="r"
              />
            </div>

            <div v-if="track.kind === 'video' && track.clips.length === 0" class="lane-hint">
              Seret video ke sini
            </div>
            <div v-if="track.kind === 'audio' && track.clips.length === 0" class="lane-hint">
              Voice-over / musik
            </div>
          </div>

          <div class="playhead" :style="{ left: editor.playhead * pps + 'px' }" />
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.timeline-panel {
  display: flex;
  flex-direction: column;
  min-height: 0;
  background: var(--panel);
  border-top: 1px solid var(--border-soft);
  overflow: hidden;
}

.timeline-toolbar {
  height: 40px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 3px;
  padding: 0 10px;
  border-bottom: 1px solid var(--border-soft);
  background: #0c0c11;
}

.icon-btn.small {
  width: 26px;
  height: 26px;
}

.toolbar-info {
  display: flex;
  gap: 6px;
  margin-left: 8px;
}

.zoom-block {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 7px;
}

.zoom-block input[type='range'] {
  width: 96px;
}

.fit-btn {
  height: 26px;
  font-size: 10.5px;
  font-variant-numeric: tabular-nums;
}

.timeline-body {
  flex: 1;
  min-height: 0;
  display: flex;
  overflow: hidden;
}

.track-headers {
  width: 132px;
  flex-shrink: 0;
  border-right: 1px solid var(--border-soft);
  background: #0b0b10;
  z-index: 3;
}

.ruler-spacer {
  border-bottom: 1px solid var(--border-soft);
}

.track-head {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 0 6px 0 9px;
  border-bottom: 1px solid var(--border-soft);
}

.track-kind {
  width: 22px;
  height: 22px;
  flex-shrink: 0;
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--panel-3);
  color: var(--muted);
}

.track-kind.video {
  color: #8c9cff;
}

.track-kind.audio {
  color: var(--teal);
}

.track-kind.overlay {
  color: var(--amber);
}

.track-kind.caption {
  color: #d99cff;
}

.track-info {
  min-width: 0;
  flex: 1;
}

.track-name {
  font-size: 11px;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.track-sub {
  font-size: 9.5px;
  color: var(--muted-2);
}

.track-tools {
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.icon-btn.tiny {
  width: 18px;
  height: 16px;
  border-radius: 4px;
}

.icon-btn.tiny.off {
  color: var(--danger);
}

.timeline-scroll {
  flex: 1;
  overflow-x: auto;
  overflow-y: hidden;
  background: #09090d;
}

.timeline-content {
  position: relative;
  min-height: 100%;
}

.ruler {
  position: relative;
  border-bottom: 1px solid var(--border-soft);
  background: #0b0b10;
  cursor: col-resize;
}

.tick {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 1px;
  background: #22222c;
}

.tick span {
  position: absolute;
  top: 4px;
  left: 5px;
  font-size: 9.5px;
  color: var(--muted-2);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.playhead-cap {
  position: absolute;
  top: 0;
  width: 0;
  height: 0;
  border-left: 6px solid transparent;
  border-right: 6px solid transparent;
  border-top: 8px solid var(--accent-strong);
  transform: translateX(-6px);
  pointer-events: none;
  z-index: 6;
}

.lane {
  position: relative;
  border-bottom: 1px solid var(--border-soft);
  background-image: linear-gradient(90deg, #1c1c25 1px, transparent 1px);
  background-position: 0 0;
  overflow: hidden;
}

.lane.locked {
  background-color: rgba(255, 255, 255, 0.015);
}

.lane.overlay {
  background-color: rgba(242, 178, 92, 0.015);
}

.lane.audio {
  background-color: rgba(35, 213, 180, 0.015);
}

.lane.caption {
  background-color: rgba(217, 156, 255, 0.02);
}

.lane-hint {
  position: absolute;
  left: 12px;
  top: 50%;
  transform: translateY(-50%);
  font-size: 10.5px;
  color: #4b4b5a;
  pointer-events: none;
}

.clip {
  position: absolute;
  top: 4px;
  bottom: 4px;
  border-radius: 6px;
  overflow: hidden;
  cursor: grab;
  background: linear-gradient(180deg, #2b2b44, #222236);
  border: 1px solid rgba(140, 156, 255, 0.4);
  box-shadow: 0 2px 10px rgba(0, 0, 0, 0.35);
  touch-action: none;
}

.clip.audio {
  background: linear-gradient(180deg, #113a35, #0e2b28);
  border-color: rgba(35, 213, 180, 0.42);
  color: #7ff0d9;
}

.clip.image {
  background: linear-gradient(180deg, #3b2d19, #2a2012);
  border-color: rgba(242, 178, 92, 0.45);
}

.clip.text {
  background: linear-gradient(180deg, #35261a, #261b12);
  border-color: rgba(242, 178, 92, 0.45);
}

.clip.video {
  color: #cfd3ff;
}

.clip.selected {
  border-color: #fff;
  box-shadow: 0 0 0 1px rgba(255, 255, 255, 0.75), 0 4px 18px rgba(0, 0, 0, 0.5);
  z-index: 4;
}

.clip.muted {
  opacity: 0.45;
}

.clip-thumbs {
  position: absolute;
  inset: 0;
  background-repeat: repeat-x;
  background-position: left center;
  background-size: auto 100%;
  opacity: 0.5;
  pointer-events: none;
}

.clip-wave {
  opacity: 1;
}

.clip-label {
  position: absolute;
  left: 6px;
  right: 6px;
  bottom: 3px;
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 10px;
  pointer-events: none;
  text-shadow: 0 1px 3px rgba(0, 0, 0, 0.8);
}

.clip-name {
  flex: 1;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  opacity: 0.92;
}

.clip-duration {
  flex-shrink: 0;
  opacity: 0.55;
  font-variant-numeric: tabular-nums;
  font-size: 9px;
}

.trim-handle {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 8px;
  cursor: ew-resize;
  background: rgba(255, 255, 255, 0.28);
  z-index: 5;
}

.trim-handle.left {
  left: 0;
  border-radius: 5px 0 0 5px;
}

.trim-handle.right {
  right: 0;
  border-radius: 0 5px 5px 0;
}

.trim-handle:hover {
  background: rgba(255, 255, 255, 0.55);
}

.playhead {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 1.5px;
  background: var(--accent-strong);
  box-shadow: 0 0 8px rgba(148, 135, 255, 0.65);
  pointer-events: none;
  z-index: 5;
}
</style>
