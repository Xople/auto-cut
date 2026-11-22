<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch, watchEffect, type CSSProperties } from 'vue'
import { Diamond, Music, Pause, Play, RotateCcw, SkipBack, SkipForward, Volume2, VolumeX } from 'lucide-vue-next'
import { assetUrl } from '../api'
import { useEditor } from '../stores/editor'
import { formatTime, timecode } from '../lib/format'
import { keyframeAt, transformAt } from '../lib/transform'
import OverlayPreview from './OverlayPreview.vue'
import CaptionPreview from './CaptionPreview.vue'
import { blendCSS, type Asset, type Clip } from '../types'

const editor = useEditor()
const shell = ref<HTMLElement | null>(null)
const stageEl = ref<HTMLElement | null>(null)
const stageSize = ref({ width: 0, height: 0 })
const shellSize = ref({ width: 0, height: 0 })
const videoRefs = new Map<string, HTMLVideoElement>()
const bgVideoRefs = new Map<string, HTMLVideoElement>()
const muted = ref(false)
const audioRefs = new Map<string, HTMLAudioElement>()

const project = computed(() => editor.project!)
const timeline = computed(() => project.value.timeline)

type VisualLayer =
  | { kind: 'video'; clip: Clip; asset?: Asset; trackId: string }
  | { kind: 'overlay'; clip: Clip; asset?: Asset; trackId: string }

function contains(clip: Clip, time: number): boolean {
  return time >= clip.start - 0.0001 && time < clip.start + clip.duration - 0.0001
}

// Video tracks are the base; overlay tracks always composite above them no
// matter where they sit in the track list. Within one kind the first track is
// the upper layer, so it is pushed last (later DOM nodes paint on top).
const visualLayers = computed<VisualLayer[]>(() => {
  const result: VisualLayer[] = []
  const tracks = timeline.value.tracks
  for (const kind of ['video', 'overlay'] as const) {
    for (let i = tracks.length - 1; i >= 0; i--) {
      const track = tracks[i]
      if (track.hidden || track.kind !== kind) continue
      for (const clip of track.clips) {
        if (!contains(clip, editor.playhead)) continue
        const asset = clip.asset_id ? editor.assetMap.get(clip.asset_id) : undefined
        if (kind === 'video') {
          if (asset?.kind === 'video') result.push({ kind: 'video', clip, asset, trackId: track.id })
        } else if (clip.text?.trim() || asset) {
          result.push({ kind: 'overlay', clip, asset, trackId: track.id })
        }
      }
    }
  }
  return result
})

const videoLayers = computed(() => visualLayers.value.filter((l) => l.kind === 'video'))

type OverlayItem = { clip: Clip; asset?: Asset; trackId: string }

// The overlay list only changes when a clip enters or leaves the playhead, so
// the overlay component does not re-render on every playback frame.
const overlayItems = shallowRef<OverlayItem[]>([])

watchEffect(() => {
  const next: OverlayItem[] = []
  for (const layer of visualLayers.value) {
    if (layer.kind === 'overlay') next.push({ clip: layer.clip, asset: layer.asset, trackId: layer.trackId })
  }
  const prev = overlayItems.value
  const same =
    prev.length === next.length &&
    prev.every((item, i) => item.clip === next[i].clip && item.asset === next[i].asset && item.trackId === next[i].trackId)
  if (!same) overlayItems.value = next
})

const selectedVideoLayer = computed(() => {
  const selection = editor.selection
  if (!selection) return null
  return videoLayers.value.find((layer) => layer.clip.id === selection.clipId) ?? null
})

const selectedTransform = computed(() =>
  selectedVideoLayer.value ? transformAt(selectedVideoLayer.value.clip, editor.playhead) : null,
)

const hasKeyframeHere = computed(() =>
  selectedVideoLayer.value ? !!keyframeAt(selectedVideoLayer.value.clip, editor.playhead) : false,
)

const keyframeCount = computed(() => selectedVideoLayer.value?.clip.keyframes?.length ?? 0)

const linkedAudioTargets = computed(() => {
  const ids = new Set<string>()
  for (const track of timeline.value.tracks) {
    if (track.kind !== 'audio') continue
    for (const clip of track.clips) if (clip.linked_id) ids.add(clip.linked_id)
  }
  return ids
})

// Legacy projects may keep an audio clip that mirrors a video clip's built-in
// audio without a linked_id. Skip the video's stream so the sound is not doubled.
const mirroredAudioClips = computed(() => {
  const ids = new Set<string>()
  for (const track of timeline.value.tracks) {
    if (track.kind !== 'video') continue
    for (const clip of track.clips) {
      if (!clip.asset_id) continue
      const twin = timeline.value.tracks.some(
        (audioTrack) =>
          audioTrack.kind === 'audio' &&
          audioTrack.clips.some(
            (audio) =>
              audio.asset_id === clip.asset_id &&
              Math.abs(audio.start - clip.start) <= 0.06 &&
              Math.abs(audio.duration - clip.duration) <= 0.06 &&
              Math.abs(audio.in - clip.in) <= 0.06,
          ),
      )
      if (twin) ids.add(clip.id)
    }
  }
  return ids
})

// Audio tracks mix with each video clip's built-in audio. Legacy projects that
// still pair a video with a linked audio clip keep using that audio clip.
const activeAudio = computed(() => {
  const result: Array<{ clip: Clip; volume: number }> = []
  for (const track of timeline.value.tracks) {
    if (track.hidden) continue
    if (track.kind === 'audio') {
      if (track.muted) continue
      for (const clip of track.clips) {
        if (contains(clip, editor.playhead)) result.push({ clip, volume: clip.volume })
      }
      continue
    }
    if (track.kind !== 'video' || track.muted) continue
    for (const clip of track.clips) {
      if (!contains(clip, editor.playhead)) continue
      if (linkedAudioTargets.value.has(clip.id) || mirroredAudioClips.value.has(clip.id)) continue
      const asset = clip.asset_id ? editor.assetMap.get(clip.asset_id) : undefined
      if (asset?.has_audio) result.push({ clip, volume: clip.volume })
    }
  }
  return result
})

const activeCaption = computed(() => {
  for (const track of timeline.value.tracks) {
    if (track.kind !== 'caption' || track.muted || track.hidden) continue
    for (const clip of track.clips) {
      if (contains(clip, editor.playhead)) return clip
    }
  }
  return null
})

const hasVisualLayers = computed(() => visualLayers.value.length > 0)
const hasAnyClip = computed(() => timeline.value.tracks.some((t) => t.clips.length > 0))
const audioOnlyName = computed(() => {
  const first = activeAudio.value[0]?.clip
  const asset = first?.asset_id ? editor.assetMap.get(first.asset_id) : undefined
  return asset?.filename ?? project.value.name
})

const stageStyle = computed(() => {
  if (!stageSize.value.width) return { width: '100%', height: '100%' }
  return { width: `${stageSize.value.width}px`, height: `${stageSize.value.height}px` }
})

const ratio = computed(() => project.value.width / project.value.height)

let observer: ResizeObserver | null = null
function measure() {
  if (!shell.value) return
  const width = shell.value.clientWidth
  const height = shell.value.clientHeight
  shellSize.value = { width, height }
  const availableW = Math.max(80, width - 34)
  const availableH = Math.max(60, height - 34)
  let w = availableW
  let h = w / ratio.value
  if (h > availableH) {
    h = availableH
    w = h * ratio.value
  }
  stageSize.value = { width: Math.round(w), height: Math.round(h) }
}

onMounted(() => {
  measure()
  if (shell.value) {
    observer = new ResizeObserver(measure)
    observer.observe(shell.value)
  }
})

onBeforeUnmount(() => {
  observer?.disconnect()
  transformDrag = null
  window.removeEventListener('pointermove', onTransformDragMove)
  videoRefs.forEach((el) => el.pause())
  videoRefs.clear()
  audioRefs.forEach((el) => el.pause())
  audioRefs.clear()
})

function expectedTime(clip: Clip): number {
  const relative = editor.playhead - clip.start
  return Math.min(Math.max(clip.in, clip.in + relative), Math.max(clip.in, clip.out - 0.02))
}

function syncElement(el: HTMLMediaElement | null, clip: Clip, force = false) {
  if (!el) return
  const expected = expectedTime(clip)
  const drift = el.currentTime - expected
  if (force || Math.abs(drift) > 0.45) {
    try {
      el.currentTime = expected
      el.playbackRate = 1
    } catch {
      /* not seekable yet */
    }
  } else if (editor.playing && Math.abs(drift) > 0.12) {
    // Correct drift by nudging the rate; seeking every frame stalls playback.
    el.playbackRate = Math.min(1.08, Math.max(0.92, 1 - drift * 0.5))
  } else if (el.playbackRate !== 1) {
    el.playbackRate = 1
  }
  if (editor.playing) {
    if (el.paused) el.play().catch(() => undefined)
  } else if (!el.paused) {
    el.pause()
  }
}

function syncAll(force = false) {
  for (const layer of videoLayers.value) {
    syncElement(videoRefs.get(layer.clip.id) ?? null, layer.clip, force)
    if (layer.clip.background_fill === 'blur') {
      syncElement(bgVideoRefs.get(layer.clip.id) ?? null, layer.clip, force)
    }
  }
  for (const item of activeAudio.value) {
    const el = audioRefs.get(item.clip.id) ?? null
    if (el) el.volume = muted.value ? 0 : Math.max(0, Math.min(1, item.volume))
    syncElement(el, item.clip, force)
  }
}

function setVideoRef(id: string, el: HTMLVideoElement | null) {
  if (el) {
    videoRefs.set(id, el)
    syncAll(true)
  } else {
    videoRefs.delete(id)
  }
}

function setBgVideoRef(id: string, el: HTMLVideoElement | null) {
  if (el) {
    bgVideoRefs.set(id, el)
    syncAll(true)
  } else {
    bgVideoRefs.delete(id)
  }
}

function bgVideoLayerStyle(clip: Clip): CSSProperties {
  const blurPx = Math.max(8, Math.round((clip.background_blur ?? 24) * 0.75))
  const brightness = Math.max(0.3, 1 - (clip.background_dim ?? 0.14))
  return {
    opacity: clip.opacity,
    objectFit: 'cover',
    filter: `blur(${blurPx}px) brightness(${brightness.toFixed(2)})`,
    transform: 'scale(1.12)',
  }
}

function transitionProgress(clip: Clip): number | null {
  const transition = clip.transition
  if (!transition || transition.duration <= 0) return null
  const elapsed = editor.playhead - clip.start
  if (elapsed < 0 || elapsed >= transition.duration) return null
  return Math.min(1, Math.max(0, elapsed / transition.duration))
}

function transformCSS(clip: Clip): string {
  const value = transformAt(clip, editor.playhead)
  return (
    `translate(${((value.x - 0.5) * 100).toFixed(3)}%, ${((value.y - 0.5) * 100).toFixed(3)}%) ` +
    `scale(${value.scale.toFixed(4)}) rotate(${value.rotation.toFixed(2)}deg)`
  )
}

function videoLayerStyle(clip: Clip): CSSProperties {
  const style: CSSProperties = {
    opacity: clip.opacity,
    mixBlendMode: blendCSS(clip.blend_mode) as CSSProperties['mixBlendMode'],
    transformOrigin: 'center center',
  }
  const progress = transitionProgress(clip)
  let scaleFactor = 1
  let slide = 0
  if (progress !== null && clip.transition) {
    const eased = 1 - Math.pow(1 - progress, 3)
    switch (clip.transition.type) {
      case 'pull_in':
        scaleFactor = 1.18 - 0.18 * eased
        style.opacity = clip.opacity * (0.35 + 0.65 * eased)
        break
      case 'slide_right':
        slide = (1 - eased) * 100
        break
      case 'slide_left':
        slide = -(1 - eased) * 100
        break
    }
  }
  const value = transformAt(clip, editor.playhead)
  style.transform =
    `translate(${((value.x - 0.5) * 100).toFixed(3)}%, ${((value.y - 0.5) * 100).toFixed(3)}%) ` +
    `translateX(${slide.toFixed(3)}%) scale(${(value.scale * scaleFactor).toFixed(4)}) ` +
    `rotate(${value.rotation.toFixed(2)}deg)`
  return style
}

const frameStyle = computed<CSSProperties>(() => {
  const layer = selectedVideoLayer.value
  if (!layer) return {}
  return { transform: transformCSS(layer.clip) }
})

type DragMode = 'move' | 'scale'

let transformDrag:
  | {
      mode: DragMode
      startX: number
      startY: number
      rect: DOMRect
      start: { x: number; y: number; scale: number; rotation: number }
      center: { x: number; y: number }
      startDistance: number
      moved: boolean
    }
  | null = null

function beginTransformDrag(event: PointerEvent, mode: DragMode) {
  const layer = selectedVideoLayer.value
  if (!layer || !stageEl.value) return
  const rect = stageEl.value.getBoundingClientRect()
  const start = transformAt(layer.clip, editor.playhead)
  const center = {
    x: rect.left + rect.width / 2 + (start.x - 0.5) * rect.width,
    y: rect.top + rect.height / 2 + (start.y - 0.5) * rect.height,
  }
  transformDrag = {
    mode,
    startX: event.clientX,
    startY: event.clientY,
    rect,
    start,
    center,
    startDistance: Math.max(1, Math.hypot(event.clientX - center.x, event.clientY - center.y)),
    moved: false,
  }
  event.preventDefault()
  event.stopPropagation()
  window.addEventListener('pointermove', onTransformDragMove)
  window.addEventListener('pointerup', onTransformDragEnd, { once: true })
}

function onTransformDragMove(event: PointerEvent) {
  const drag = transformDrag
  const layer = selectedVideoLayer.value
  const selection = editor.selection
  if (!drag || !layer || !selection) return
  if (Math.hypot(event.clientX - drag.startX, event.clientY - drag.startY) > 3) drag.moved = true
  if (drag.mode === 'move') {
    const dx = (event.clientX - drag.startX) / drag.rect.width
    const dy = (event.clientY - drag.startY) / drag.rect.height
    editor.setClipTransform(selection.trackId, layer.clip.id, {
      x: drag.start.x + dx,
      y: drag.start.y + dy,
    })
    return
  }
  const distance = Math.hypot(event.clientX - drag.center.x, event.clientY - drag.center.y)
  editor.setClipTransform(selection.trackId, layer.clip.id, {
    scale: (drag.start.scale * distance) / drag.startDistance,
  })
}

function onTransformDragEnd() {
  const drag = transformDrag
  transformDrag = null
  window.removeEventListener('pointermove', onTransformDragMove)
  if (drag && drag.mode === 'move' && !drag.moved) editor.togglePlay()
}

function toggleSelectedKeyframe() {
  const selection = editor.selection
  if (!selection) return
  editor.toggleClipKeyframe(selection.trackId, selection.clipId)
}

function resetSelectedTransform() {
  const selection = editor.selection
  if (!selection) return
  editor.clearClipTransform(selection.trackId, selection.clipId)
}

function setAudioRef(id: string, el: HTMLAudioElement | null) {
  if (el) {
    audioRefs.set(id, el)
    syncAll(true)
  } else {
    const existing = audioRefs.get(id)
    existing?.pause()
    audioRefs.delete(id)
  }
}

watch(
  () => [editor.playhead, editor.playing, muted.value],
  () => syncAll(false),
  { flush: 'post' },
)

watch(
  () => videoLayers.value.map((l) => l.clip.id).join(','),
  () => nextTick(() => syncAll(true)),
)

watch(
  () => activeAudio.value.map((a) => a.clip.id).join(','),
  () => nextTick(() => syncAll(true)),
)

watch(
  () => activeAudio.value.map((a) => `${a.clip.id}:${a.volume}`).join(','),
  () => syncAll(false),
  { flush: 'post' },
)
</script>

<template>
  <section class="preview-panel">
    <div ref="shell" class="preview-shell">
      <div ref="stageEl" class="stage" :style="stageStyle" @click="editor.togglePlay()">
        <template v-for="layer in visualLayers" :key="`${layer.kind}-${layer.clip.id}`">
          <video
            v-if="layer.kind === 'video' && layer.asset && layer.clip.background_fill === 'blur'"
            :ref="(el) => setBgVideoRef(layer.clip.id, el as HTMLVideoElement | null)"
            class="stage-video stage-video-bg"
            :style="bgVideoLayerStyle(layer.clip)"
            :src="assetUrl(project.id, layer.asset.id)"
            muted
            playsinline
            @loadedmetadata="syncAll(true)"
            @canplay="syncAll(true)"
          />
          <video
            v-if="layer.kind === 'video' && layer.asset"
            :ref="(el) => setVideoRef(layer.clip.id, el as HTMLVideoElement | null)"
            class="stage-video"
            :style="videoLayerStyle(layer.clip)"
            :src="assetUrl(project.id, layer.asset.id)"
            muted
            playsinline
            @loadedmetadata="syncAll(true)"
            @canplay="syncAll(true)"
          />
        </template>

        <OverlayPreview
          :overlays="overlayItems"
          :project="project"
          :project-id="project.id"
          :display-height="stageSize.height"
        />

        <div v-if="!hasVisualLayers && hasAnyClip" class="audio-stage">
          <div class="audio-orb"><Music :size="26" /></div>
          <div class="audio-stage-title">Audio timeline</div>
          <div class="audio-stage-name">{{ audioOnlyName }}</div>
          <div class="audio-bars">
            <span v-for="i in 22" :key="i" :style="{ height: `${18 + ((i * 37) % 40)}%` }" />
          </div>
        </div>

        <div v-if="!hasAnyClip" class="stage-empty">
          <div class="stage-empty-title">Timeline kosong</div>
          <div>Impor media lalu seret ke track di bawah.</div>
        </div>

        <CaptionPreview
          :style="timeline.caption_style"
          :clip="activeCaption"
          :display-height="stageSize.height"
          :playhead="editor.playhead"
        />

        <div
          v-if="selectedVideoLayer && selectedTransform"
          class="transform-frame"
          :style="frameStyle"
          @pointerdown="beginTransformDrag($event, 'move')"
          @click.stop
        >
          <button
            v-for="corner in ['nw', 'ne', 'se', 'sw']"
            :key="corner"
            class="transform-handle"
            :class="corner"
            :title="`Skala ${Math.round((selectedTransform?.scale ?? 1) * 100)}%`"
            @pointerdown.stop="beginTransformDrag($event, 'scale')"
            @click.stop
          />
        </div>

        <div class="stage-badge">{{ timecode(editor.playhead, project.fps) }}</div>
      </div>

      <div v-if="selectedVideoLayer" class="stage-tools" @pointerdown.stop @click.stop>
        <button
          class="stage-tool"
          :class="{ active: hasKeyframeHere, animated: keyframeCount > 0 }"
          :title="hasKeyframeHere ? 'Hapus keyframe di playhead' : 'Tambah keyframe transform di playhead'"
          @click="toggleSelectedKeyframe"
        >
          <Diamond :size="12" :fill="hasKeyframeHere ? 'currentColor' : 'none'" />
          <span>{{ keyframeCount > 0 ? `${keyframeCount} keyframe` : 'Keyframe' }}</span>
        </button>
        <span class="stage-tool-meta">
          {{ Math.round((selectedTransform?.scale ?? 1) * 100) }}% ·
          {{ Math.round((selectedTransform?.x ?? 0.5) * 100) }},{{ Math.round((selectedTransform?.y ?? 0.5) * 100) }}
        </span>
        <button class="stage-tool icon" title="Reset transform" @click="resetSelectedTransform">
          <RotateCcw :size="12" />
        </button>
      </div>

      <audio
        v-for="item in activeAudio"
        :key="item.clip.id"
        :ref="(el) => setAudioRef(item.clip.id, el as HTMLAudioElement | null)"
        :src="item.clip.asset_id ? assetUrl(project.id, item.clip.asset_id) : undefined"
        preload="auto"
        @loadedmetadata="syncAll(true)"
      />
    </div>

    <div class="transport">
      <button class="icon-btn" title="Mundur 5 detik" @click="editor.seek(editor.playhead - 5)">
        <SkipBack :size="16" />
      </button>
      <button class="play-btn" :disabled="editor.duration <= 0" @click="editor.togglePlay()">
        <Pause v-if="editor.playing" :size="17" fill="currentColor" />
        <Play v-else :size="17" fill="currentColor" />
      </button>
      <button class="icon-btn" title="Maju 5 detik" @click="editor.seek(editor.playhead + 5)">
        <SkipForward :size="16" />
      </button>

      <div class="time-read">
        <span class="time-now">{{ formatTime(editor.playhead) }}</span>
        <span class="time-total">/ {{ formatTime(editor.duration) }}</span>
      </div>

      <div class="transport-meta">
        <span class="badge">{{ project.width }}×{{ project.height }}</span>
        <span class="badge">{{ Math.round(project.fps) }} fps</span>
        <span class="badge accent">PREVIEW</span>
        <button class="icon-btn" :title="muted ? 'Nyalakan suara' : 'Bisukan preview'" @click="muted = !muted">
          <VolumeX v-if="muted" :size="16" />
          <Volume2 v-else :size="16" />
        </button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.preview-panel {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  background: #0d0d0d;
}

.preview-shell {
  position: relative;
  flex: 1;
  min-height: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 17px;
  background: #050505;
  overflow: hidden;
}

.stage {
  position: relative;
  background: #000;
  border: 1px solid var(--border-soft);
  border-radius: 4px;
  overflow: hidden;
  cursor: pointer;
}

.stage-video {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: contain;
  background: transparent;
  pointer-events: none;
  transform-origin: center center;
  will-change: transform, opacity;
}

.stage-empty,
.audio-stage {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: var(--muted-2);
  font-size: 12px;
  text-align: center;
  padding: 20px;
}

.stage-empty-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--muted);
}

.audio-stage {
  background: #000;
}

.audio-orb {
  width: 58px;
  height: 58px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--muted);
  background: var(--panel-2);
  border: 1px solid var(--border);
}

.audio-stage-title {
  font-size: 13px;
  font-weight: 650;
  color: var(--text);
}

.audio-stage-name {
  max-width: 80%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.audio-bars {
  display: flex;
  align-items: flex-end;
  gap: 3px;
  height: 36px;
  margin-top: 6px;
  opacity: 0.55;
}

.audio-bars span {
  width: 3px;
  border-radius: 2px;
  background: var(--teal);
}

.stage-badge {
  position: absolute;
  right: 8px;
  bottom: 8px;
  padding: 3px 7px;
  border-radius: 4px;
  background: rgba(0, 0, 0, 0.72);
  color: var(--text);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  pointer-events: none;
  z-index: 6;
}

.transform-frame {
  position: absolute;
  inset: 0;
  z-index: 5;
  border: 1px solid var(--accent);
  box-shadow: 0 0 0 1px rgba(0, 0, 0, 0.55) inset;
  transform-origin: center center;
  cursor: grab;
  touch-action: none;
}

.transform-frame:active {
  cursor: grabbing;
}

.transform-handle {
  position: absolute;
  width: 12px;
  height: 12px;
  padding: 0;
  border-radius: 2px;
  border: 1px solid #08080b;
  background: var(--accent);
}

.transform-handle.nw {
  left: -6px;
  top: -6px;
  cursor: nwse-resize;
}

.transform-handle.ne {
  right: -6px;
  top: -6px;
  cursor: nesw-resize;
}

.transform-handle.se {
  right: -6px;
  bottom: -6px;
  cursor: nwse-resize;
}

.transform-handle.sw {
  left: -6px;
  bottom: -6px;
  cursor: nesw-resize;
}

.stage-tools {
  position: absolute;
  top: 12px;
  right: 12px;
  z-index: 8;
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: rgba(8, 8, 11, 0.86);
  backdrop-filter: blur(6px);
}

.stage-tool {
  display: flex;
  align-items: center;
  gap: 5px;
  height: 24px;
  padding: 0 8px;
  border: 1px solid transparent;
  border-radius: 5px;
  background: transparent;
  color: var(--muted);
  font-size: 10.5px;
  font-weight: 600;
}

.stage-tool:hover {
  background: var(--panel-3);
  color: var(--text);
}

.stage-tool.active {
  color: var(--accent);
  border-color: rgba(198, 242, 78, 0.4);
  background: rgba(198, 242, 78, 0.1);
}

.stage-tool.animated {
  color: var(--accent);
}

.stage-tool.icon {
  padding: 0 6px;
}

.stage-tool-meta {
  font-size: 10px;
  color: var(--muted-2);
  font-variant-numeric: tabular-nums;
}

.transport {
  flex-shrink: 0;
  height: 52px;
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 0 14px;
  border-top: 1px solid var(--border-soft);
  background: #0d0d0d;
}

.play-btn {
  width: 34px;
  height: 34px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  border: none;
  background: var(--text);
  color: #000;
  transition: background 0.15s ease;
}

.play-btn:hover {
  background: #e6e6e6;
}

.play-btn:disabled {
  opacity: 0.4;
}

.time-read {
  display: flex;
  align-items: baseline;
  gap: 4px;
  margin-left: 8px;
  font-variant-numeric: tabular-nums;
}

.time-now {
  font-size: 13px;
  font-weight: 600;
  color: var(--text);
}

.time-total {
  font-size: 11px;
  color: var(--muted-2);
}

.transport-meta {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 6px;
}
</style>
