<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch, watchEffect, type CSSProperties } from 'vue'
import { ChevronDown, Diamond, Type, Maximize, Minimize, Music, Pause, Play, RotateCcw, SkipBack, SkipForward, Volume2, VolumeX } from 'lucide-vue-next'
import { assetUrl, thumbUrl } from '../api'
import { useEditor } from '../stores/editor'
import { formatTime, timecode } from '../lib/format'
import { keyframeAt, transformAt } from '../lib/transform'
import { usePreviewTextDrag } from '../lib/previewTextDrag'
import OverlayPreview from './OverlayPreview.vue'
import CaptionPreview from './CaptionPreview.vue'
import { blendCSS, videoEffectFilter, type Asset, type Clip } from '../types'

const editor = useEditor()
const { draft: textDraft, begin: beginTextDrag } = usePreviewTextDrag()
let pickedTextId: string | null = null
const shell = ref<HTMLElement | null>(null)
const stageEl = ref<HTMLElement | null>(null)
const stageSize = ref({ width: 0, height: 0 })
const player = ref<HTMLElement | null>(null)
const expanded = ref(false)
const windowPreview = ref(false)

async function toggleFullscreen() {
  if (windowPreview.value) {
    windowPreview.value = false
    expanded.value = false
  } else if (document.fullscreenElement === player.value) {
    await document.exitFullscreen()
  } else if (player.value) {
    try {
      await player.value.requestFullscreen()
    } catch {
      windowPreview.value = true
      expanded.value = true
    }
  }
}

function syncFullscreen() {
  expanded.value = document.fullscreenElement === player.value || windowPreview.value
}

function onPreviewKey(event: KeyboardEvent) {
  if (!expanded.value || event.key !== 'Escape') return
  event.stopImmediatePropagation()
  void toggleFullscreen()
}
const videoRefs = new Map<string, HTMLVideoElement>()
const muted = ref(false)
const audioRefs = new Map<string, HTMLAudioElement>()

const project = computed(() => editor.project!)
const timeline = computed(() => project.value.timeline)

type VisualLayer =
  | { kind: 'video'; clip: Clip; asset?: Asset; trackId: string; active: boolean }
  | { kind: 'overlay'; clip: Clip; asset?: Asset; trackId: string; active: boolean }

function contains(clip: Clip, time: number): boolean {
  return time >= clip.start - 0.0001 && time < clip.start + clip.duration - 0.0001
}

// Pre-mount upcoming video clips 2.5s before their start time so the browser
// finishes loading metadata and seeking to clip.in (even on long AV1/H.264 MKV
// GOPs) before the cut happens.
const VIDEO_PREWARM_SECONDS = 2.5
const AUDIO_PREWARM_SECONDS = 1.5

function shouldMountVideo(clip: Clip, time: number): boolean {
  if (contains(clip, time)) return true
  return time < clip.start && clip.start - time <= VIDEO_PREWARM_SECONDS
}

function shouldMountAudio(clip: Clip, time: number): boolean {
  if (contains(clip, time)) return true
  return time < clip.start && clip.start - time <= AUDIO_PREWARM_SECONDS
}

// Video tracks are the base; overlay tracks always composite above them no
// matter where they sit in the track list. Within one kind the first track is
// the upper layer, so it is pushed last (later DOM nodes paint on top).
const visualLayers = shallowRef<VisualLayer[]>([])

watchEffect(() => {
  const result: VisualLayer[] = []
  const tracks = timeline.value.tracks
  const t = editor.playhead
  for (const kind of ['video', 'overlay'] as const) {
    for (let i = tracks.length - 1; i >= 0; i--) {
      const track = tracks[i]
      if (track.hidden || track.kind !== kind) continue
      for (const clip of track.clips) {
        const active = contains(clip, t)
        if (kind === 'video') {
          if (!shouldMountVideo(clip, t)) continue
          const asset = clip.asset_id ? editor.assetMap.get(clip.asset_id) : undefined
          if (asset?.kind === 'video') result.push({ kind: 'video', clip, asset, trackId: track.id, active })
        } else {
          if (!active) continue
          const asset = clip.asset_id ? editor.assetMap.get(clip.asset_id) : undefined
          if (clip.text?.trim() || asset) {
            result.push({ kind: 'overlay', clip, asset, trackId: track.id, active: true })
          }
        }
      }
    }
  }
  const prev = visualLayers.value
  const same =
    prev.length === result.length &&
    prev.every(
      (item, i) =>
        item.kind === result[i].kind &&
        item.clip === result[i].clip &&
        item.asset === result[i].asset &&
        item.trackId === result[i].trackId &&
        item.active === result[i].active,
    )
  if (!same) visualLayers.value = result
})

const videoLayers = computed(() => visualLayers.value.filter((l) => l.kind === 'video'))
const activeVideoLayers = computed(() => videoLayers.value.filter((l) => l.active))

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
  return activeVideoLayers.value.find((layer) => layer.clip.id === selection.clipId) ?? null
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

// Audio tracks play via <audio> elements (pre-warmed 1.5s ahead of clip.start so
// short 0.35s transition impact SFX fire with zero decode latency); video tracks
// play their own built-in audio directly on their <video> element.
const activeAudio = shallowRef<Array<{ clip: Clip; volume: number; active: boolean }>>([])

watchEffect(() => {
  const result: Array<{ clip: Clip; volume: number; active: boolean }> = []
  const t = editor.playhead
  for (const track of timeline.value.tracks) {
    if (track.hidden || track.muted || track.kind !== 'audio') continue
    for (const clip of track.clips) {
      if (clip.volume <= 0.0001) continue
      if (!shouldMountAudio(clip, t)) continue
      const active = contains(clip, t)
      result.push({ clip, volume: clip.volume, active })
    }
  }
  const prev = activeAudio.value
  const same =
    prev.length === result.length &&
    prev.every(
      (item, i) =>
        item.clip === result[i].clip && item.volume === result[i].volume && item.active === result[i].active,
    )
  if (!same) activeAudio.value = result
})

function videoClipVolume(clip: Clip, trackId: string): number {
  if (muted.value) return 0
  const track = timeline.value.tracks.find((t) => t.id === trackId)
  if (!track || track.muted || track.hidden) return 0
  if (linkedAudioTargets.value.has(clip.id) || mirroredAudioClips.value.has(clip.id)) return 0
  const asset = clip.asset_id ? editor.assetMap.get(clip.asset_id) : undefined
  if (!asset?.has_audio) return 0
  return Math.max(0, Math.min(1, clip.volume))
}

const activeCaptions = computed(() => {
  const items: { clip: Clip; trackId: string }[] = []
  for (const track of [...timeline.value.tracks].reverse()) {
    if (track.kind !== 'caption' || track.muted || track.hidden) continue
    for (const clip of track.clips) {
      if ((clip.text || clip.words?.length) && contains(clip, editor.playhead)) items.push({ clip, trackId: track.id })
    }
  }
  return items
})

const visibleText = computed(() => [
  ...activeCaptions.value,
  ...overlayItems.value.filter(item => item.clip.text?.trim()),
])

function selectPreviewText(event: Event) {
  const id = (event.target as HTMLSelectElement).value
  const item = visibleText.value.find(item => item.clip.id === id)
  if (!item) return
  editor.pause()
  editor.select(item.trackId, id)
  pickedTextId = id
}

// The picker gives a covered text item priority on the next drag inside its bounds.
function startPickedText(event: PointerEvent) {
  const id = pickedTextId
  pickedTextId = null
  if (!id || expanded.value || id !== editor.selectedClip?.id || !editor.selectedTrack) return
  const hits = document.elementsFromPoint(event.clientX, event.clientY)
  const match = hits.find((el): el is HTMLElement => el instanceof HTMLElement && el.dataset.previewText === id)
  if (match && !match.contains(event.target as Node)) {
    beginTextDrag(event, editor.selectedTrack.id, editor.selectedClip, 'move', match)
  }
}

const hasVisualLayers = computed(() => activeVideoLayers.value.length > 0 || overlayItems.value.length > 0 || activeCaptions.value.length > 0)
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
  const inset = expanded.value ? 0 : 16
  const availableW = Math.max(1, width - inset)
  const availableH = Math.max(1, height - inset)
  let w = availableW
  let h = w / ratio.value
  if (h > availableH) {
    h = availableH
    w = h * ratio.value
  }
  stageSize.value = { width: Math.round(w), height: Math.round(h) }
}

watch([ratio, expanded], measure, { flush: 'post' })

onMounted(() => {
  document.addEventListener('fullscreenchange', syncFullscreen)
  window.addEventListener('keydown', onPreviewKey, true)
  measure()
  if (shell.value) {
    observer = new ResizeObserver(measure)
    observer.observe(shell.value)
  }
})

onBeforeUnmount(() => {
  document.removeEventListener('fullscreenchange', syncFullscreen)
  window.removeEventListener('keydown', onPreviewKey, true)
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

function syncElement(el: HTMLMediaElement | null, clip: Clip, force = false, isAudio = false, active = true) {
  if (!el || el.readyState < 1) return
  const expected = active ? expectedTime(clip) : clip.in
  const drift = el.currentTime - expected
  const rate = clip.speed && clip.speed > 0 ? clip.speed : 1

  if (editor.playing && active) {
    // Never interrupt an in-flight seek while playing unless explicitly forced
    if (!el.seeking && (force || Math.abs(drift) > (isAudio ? 0.35 : 0.45))) {
      try {
        el.currentTime = expected
      } catch {
        /* not seekable yet */
      }
    }
    // Avoid continuous playbackRate oscillation on audio which causes robotic pitch-stretching artifacts
    if (isAudio) {
      if (el.playbackRate !== rate) el.playbackRate = rate
    } else if (!el.seeking && Math.abs(drift) > 0.14 && Math.abs(drift) <= 0.45) {
      el.playbackRate = Math.min(rate * 1.06, Math.max(rate * 0.94, rate - drift * 0.35))
    } else if (el.playbackRate !== rate) {
      el.playbackRate = rate
    }
    if (el.paused) el.play().catch(() => undefined)
  } else {
    if (!el.paused) el.pause()
    if (!el.seeking && (force || Math.abs(drift) > 0.03)) {
      try {
        el.currentTime = expected
      } catch {
        /* not seekable yet */
      }
    }
    if (el.playbackRate !== rate) el.playbackRate = rate
  }
}

function syncAll(force = false) {
  for (const layer of videoLayers.value) {
    const fg = videoRefs.get(layer.clip.id) ?? null
    if (fg) {
      const vol = layer.active ? videoClipVolume(layer.clip, layer.trackId) : 0
      fg.muted = vol <= 0.0001
      fg.volume = vol
    }
    syncElement(fg, layer.clip, force, false, layer.active)
  }
  for (const item of activeAudio.value) {
    const el = audioRefs.get(item.clip.id) ?? null
    if (el) {
      const vol = item.active && !muted.value ? Math.max(0, Math.min(1, item.volume)) : 0
      el.muted = vol <= 0.0001
      el.volume = vol
    }
    syncElement(el, item.clip, force, true, item.active)
  }
}

const videoRefBinders = new Map<string, (el: unknown) => void>()
const audioRefBinders = new Map<string, (el: unknown) => void>()

function bindVideoRef(id: string): (el: unknown) => void {
  let fn = videoRefBinders.get(id)
  if (!fn) {
    fn = (raw: unknown) => {
      const el = raw as HTMLVideoElement | null
      if (el) {
        const prev = videoRefs.get(id)
        videoRefs.set(id, el)
        if (prev !== el) {
          const layer = videoLayers.value.find((l) => l.clip.id === id)
          if (layer) {
            const vol = layer.active ? videoClipVolume(layer.clip, layer.trackId) : 0
            el.muted = vol <= 0.0001
            el.volume = vol
            syncElement(el, layer.clip, true, false, layer.active)
          }
        }
      } else {
        const existing = videoRefs.get(id)
        existing?.pause()
        videoRefs.delete(id)
      }
    }
    videoRefBinders.set(id, fn)
  }
  return fn
}

function bindAudioRef(id: string): (el: unknown) => void {
  let fn = audioRefBinders.get(id)
  if (!fn) {
    fn = (raw: unknown) => {
      const el = raw as HTMLAudioElement | null
      if (el) {
        const prev = audioRefs.get(id)
        audioRefs.set(id, el)
        if (prev !== el) {
          const item = activeAudio.value.find((a) => a.clip.id === id)
          if (item) {
            const vol = item.active && !muted.value ? Math.max(0, Math.min(1, item.volume)) : 0
            el.muted = vol <= 0.0001
            el.volume = vol
            syncElement(el, item.clip, true, true, item.active)
          }
        }
      } else {
        const existing = audioRefs.get(id)
        existing?.pause()
        audioRefs.delete(id)
      }
    }
    audioRefBinders.set(id, fn)
  }
  return fn
}

function bgVideoLayerStyle(clip: Clip): CSSProperties {
  const blurPx = Math.max(8, Math.round((clip.background_blur ?? 24) * 0.75))
  const brightness = Math.max(0.3, 1 - (clip.background_dim ?? 0.14))
  return {
    opacity: clip.opacity,
    objectFit: 'cover',
    filter: `${videoEffectFilter(clip.video_effect, clip.effect_intensity)} blur(${blurPx}px) brightness(${brightness.toFixed(2)})`,
    transform: 'scale(1.12)',
  }
}

function outgoingTransitionFor(clip: Clip) {
  const clipEnd = clip.start + clip.duration
  for (const track of timeline.value.tracks) {
    if (track.kind !== 'video' || track.hidden) continue
    const idx = track.clips.findIndex((c) => c.id === clip.id)
    if (idx < 0) continue
    for (const other of track.clips) {
      if (other.id === clip.id || !other.transition || other.transition.duration <= 0) continue
      if (Math.abs(other.start - clipEnd) <= 0.12) return other.transition
    }
  }
  return null
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
  let scaleFactor = 1
  let slideX = 0
  let slideY = 0
  let rotOffset = 0

  // Phase B (Incoming): CapCut decelerating ease-out from the cut boundary
  const inTrans = clip.transition
  if (inTrans && inTrans.duration > 0) {
    const inHalf = Math.min(Math.max(0.08, inTrans.duration * 0.58), clip.duration * 0.55)
    const elapsed = editor.playhead - clip.start
    if (elapsed >= 0 && elapsed < inHalf) {
      const r = Math.min(1, Math.max(0, 1 - elapsed / inHalf))
      const rQuad = r * r
      switch (inTrans.type) {
        case 'pull_in':
          scaleFactor *= 1 + 0.28 * rQuad
          break
        case 'pull_out':
          scaleFactor *= 1 + 0.34 * rQuad
          break
        case 'blink':
          style.filter = `brightness(${(1 - rQuad).toFixed(3)})`
          style.clipPath = `inset(${(38 * rQuad).toFixed(2)}% 0 ${(38 * rQuad).toFixed(2)}% 0)`
          break
        case 'flash':
          scaleFactor *= 1 + 0.1 * rQuad
          style.filter = `brightness(${(1 + 2.8 * rQuad).toFixed(3)}) contrast(${(1 - 0.35 * r).toFixed(3)})`
          break
        case 'fade':
          style.filter = `brightness(${(1 - r).toFixed(3)})`
          break
        case 'slide_up':
          scaleFactor *= 1 + 0.14 * rQuad
          slideY += 42 * rQuad
          break
        case 'slide_down':
          scaleFactor *= 1 + 0.14 * rQuad
          slideY -= 42 * rQuad
          break
        case 'slide_left':
          scaleFactor *= 1 + 0.14 * rQuad
          slideX += 42 * rQuad
          break
        case 'slide_right':
          scaleFactor *= 1 + 0.14 * rQuad
          slideX -= 42 * rQuad
          break
        case 'spin_in':
          scaleFactor *= 1 + 0.28 * rQuad
          rotOffset -= 16 * rQuad
          break
      }
    }
  }

  // Phase A (Outgoing): CapCut accelerating ease-in into the next adjacent clip's transition
  const outTrans = outgoingTransitionFor(clip)
  if (outTrans && outTrans.duration > 0) {
    const outHalf = Math.min(Math.max(0.08, outTrans.duration * 0.52), clip.duration * 0.5)
    const remToEnd = clip.start + clip.duration - editor.playhead
    if (remToEnd >= 0 && remToEnd < outHalf) {
      const u = Math.min(1, Math.max(0, 1 - remToEnd / outHalf))
      const uQuad = u * u
      switch (outTrans.type) {
        case 'pull_in':
          scaleFactor *= 1 + 0.36 * uQuad
          break
        case 'pull_out':
          scaleFactor *= 1 + 0.18 * uQuad
          break
        case 'blink':
          style.filter = `brightness(${(1 - uQuad).toFixed(3)})`
          style.clipPath = `inset(${(38 * uQuad).toFixed(2)}% 0 ${(38 * uQuad).toFixed(2)}% 0)`
          break
        case 'flash':
          scaleFactor *= 1 + 0.08 * uQuad
          style.filter = `brightness(${(1 + 2.8 * uQuad).toFixed(3)}) contrast(${(1 - 0.35 * u).toFixed(3)})`
          break
        case 'fade':
          style.filter = `brightness(${(1 - u).toFixed(3)})`
          break
        case 'slide_up':
          scaleFactor *= 1 + 0.14 * uQuad
          slideY -= 42 * uQuad
          break
        case 'slide_down':
          scaleFactor *= 1 + 0.14 * uQuad
          slideY += 42 * uQuad
          break
        case 'slide_left':
          scaleFactor *= 1 + 0.14 * uQuad
          slideX -= 42 * uQuad
          break
        case 'slide_right':
          scaleFactor *= 1 + 0.14 * uQuad
          slideX += 42 * uQuad
          break
        case 'spin_in':
          scaleFactor *= 1 + 0.28 * uQuad
          rotOffset += 16 * uQuad
          break
      }
    }
  }

  const value = transformAt(clip, editor.playhead)
  const s = (value.scale * scaleFactor).toFixed(4)
  style.transform =
    `translate(${((value.x - 0.5) * 100).toFixed(3)}%, ${((value.y - 0.5) * 100).toFixed(3)}%) ` +
    `translate(${slideX.toFixed(3)}%, ${slideY.toFixed(3)}%) scale(${s}) ` +
    `rotate(${(value.rotation + rotOffset).toFixed(2)}deg)`
  style.filter = [videoEffectFilter(clip.video_effect, clip.effect_intensity), style.filter].filter(Boolean).join(' ')
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

function onMediaReady(clip: Clip, el: EventTarget | null, isAudio = false, active = true) {
  const media = el as HTMLMediaElement | null
  if (!media) return
  if (isAudio) {
    media.volume = muted.value ? 0 : Math.max(0, Math.min(1, clip.volume))
  }
  syncElement(media, clip, true, isAudio, active)
}

watch(
  () => [editor.playhead, editor.playing, muted.value],
  () => syncAll(false),
  { flush: 'post' },
)

watch(
  () => videoLayers.value.map((l) => `${l.clip.id}:${l.active ? 1 : 0}`).join(','),
  () => nextTick(() => syncAll(false)),
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
  <section ref="player" class="preview-panel" :class="{ expanded, 'window-preview': windowPreview }">
    <div ref="shell" class="preview-shell">
      <div ref="stageEl" class="stage" :style="stageStyle" @pointerdown.capture="startPickedText" @click="editor.togglePlay()">
        <template v-for="layer in visualLayers" :key="`${layer.kind}-${layer.clip.id}`">
          <img
            v-if="layer.kind === 'video' && layer.asset && layer.active && layer.clip.background_fill === 'blur'"
            class="stage-video stage-video-bg"
            :style="bgVideoLayerStyle(layer.clip)"
            :src="thumbUrl(project.id, layer.asset.id)"
            alt=""
          />
          <video
            v-if="layer.kind === 'video' && layer.asset"
            :ref="bindVideoRef(layer.clip.id)"
            class="stage-video"
            :style="
              layer.active
                ? videoLayerStyle(layer.clip)
                : { opacity: 0, pointerEvents: 'none', visibility: 'hidden' }
            "
            :src="assetUrl(project.id, layer.asset.id)"
            preload="auto"
            :muted="!layer.active || videoClipVolume(layer.clip, layer.trackId) <= 0.0001"
            playsinline
            @loadedmetadata="onMediaReady(layer.clip, $event.target, false, layer.active)"
          />
        </template>

        <div
          v-if="!expanded && selectedVideoLayer && selectedTransform"
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

        <OverlayPreview
          :inert="expanded" :class="{ 'clean-overlay': expanded }"
          :overlays="overlayItems" :draft="textDraft" @drag="beginTextDrag"
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
          v-for="item in activeCaptions" :key="item.clip.id"
          :draft="textDraft" @drag="beginTextDrag"
          :clip="item.clip" :track-id="item.trackId" :editable="!expanded"
          :display-height="stageSize.height"
          :playhead="editor.playhead"
        />

        <template v-if="!expanded && textDraft?.mode === 'move'">
          <div v-if="textDraft.x === 0.5" class="center-guide guide-x" aria-hidden="true" />
          <div v-if="textDraft.y === 0.5" class="center-guide guide-y" aria-hidden="true" />
        </template>
        <div v-if="!expanded" class="stage-badge">{{ timecode(editor.playhead, project.fps) }}</div>
      </div>

      <div v-if="!expanded && selectedVideoLayer" class="stage-tools" @pointerdown.stop @click.stop>
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
        :ref="bindAudioRef(item.clip.id)"
        :src="item.clip.asset_id ? assetUrl(project.id, item.clip.asset_id) : undefined"
        preload="auto"
        @loadedmetadata="onMediaReady(item.clip, $event.target, true)"
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
        <label v-if="!expanded && visibleText.length" class="text-picker" title="Pilih teks di preview">
          <Type :size="15" /><span>Pilih teks</span><ChevronDown :size="11" />
          <select aria-label="Pilih teks di preview" :value="visibleText.some(item => item.clip.id === editor.selection?.clipId) ? editor.selection?.clipId : ''"
            @pointerdown="editor.pause()" @keydown="editor.pause()" @change="selectPreviewText">
            <option disabled value="">Pilih teks</option>
            <option v-for="item in visibleText" :key="item.clip.id" :value="item.clip.id">
              {{ editor.project?.timeline.tracks.find(t => t.id === item.trackId)?.name }} · {{ item.clip.text || item.clip.words?.map(w => w.word).join(' ') }}
            </option>
          </select>
        </label>
        <span class="badge">{{ project.width }}×{{ project.height }}</span>
        <span class="badge">{{ Math.round(project.fps) }} fps</span>
        <button class="icon-btn" :title="muted ? 'Nyalakan suara' : 'Bisukan preview'" @click="muted = !muted">
          <VolumeX v-if="muted" :size="16" />
          <Volume2 v-else :size="16" />
        </button>
        <button class="icon-btn" :title="expanded ? 'Keluar layar penuh' : 'Preview layar penuh'" :aria-pressed="expanded" @click="toggleFullscreen">
          <Minimize v-if="expanded" :size="17" />
          <Maximize v-else :size="17" />
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

.window-preview {
  position: fixed;
  inset: 0;
  z-index: 100;
}

.expanded .stage { border: 0; border-radius: 0; }
.clean-overlay :deep(.overlay-item.selected::after),
.clean-overlay :deep(.overlay-handle) { display: none; }

.preview-shell {
  position: relative;
  flex: 1;
  min-height: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0;
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

.center-guide {
  position: absolute;
  z-index: 5;
  pointer-events: none;
  background: var(--accent);
  box-shadow: 0 0 1px 1px rgb(0 0 0 / 45%);
}

.guide-x { inset: 0 auto 0 50%; width: 1px; }
.guide-y { inset: 50% 0 auto; height: 1px; }

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

.text-picker {
  position: relative;
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 5px;
  border: 1px solid var(--border);
  border-radius: 5px;
  color: var(--text);
  white-space: nowrap;
  font-size: 11px;
}
.text-picker select {
  position: absolute;
  inset: 0;
  width: 100%;
  opacity: 0;
  cursor: pointer;
  background: var(--panel);
  color: var(--text);
}
.text-picker:focus-within { outline: 1px solid var(--accent); }
@container (max-width: 560px) {
  .text-picker span, .transport-meta .badge { display: none; }
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
  container-type: inline-size;
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
@container (max-width: 430px) {
  .transport-meta .badge { display: none; }
  .time-read { margin-left: 0; }
}
</style>
