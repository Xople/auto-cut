<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  ArrowLeftRight,
  Captions,
  ChevronDown,
  ChevronUp,
  ClipboardPaste,
  Copy,
  Diamond,
  Eye,
  EyeOff,
  Film,
  Layers,
  Lock,
  Magnet,
  Maximize2,
  MoreVertical,
  MoveLeft,
  MoveRight,
  Music,
  Plus,
  Scissors,
  Slice,
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
import { clamp, formatTime } from '../lib/format'
import Waveform from './Waveform.vue'
import PlayheadMarker from './PlayheadMarker.vue'
import {
  TRANSITIONS,
  type Asset,
  type Clip,
  type Track,
  type TrackKind,
  type Transition,
  type TransitionType,
} from '../types'

const editor = useEditor()
const scrollEl = ref<HTMLElement | null>(null)
const contentEl = ref<HTMLElement | null>(null)
const headerListEl = ref<HTMLElement | null>(null)
const snapping = ref(true)
const bladeMode = ref(false)
const menu = ref<string | null>(null)
const dragTarget = ref<string | null>(null)
const dragConflict = ref(false)
const guide = ref<number | null>(null)
const marquee = ref<{ left: number; top: number; width: number; height: number } | null>(null)
const contextMenu = ref<{ x: number; y: number; trackId: string; clipId: string; kind: 'clip' | 'lane'; time: number } | null>(null)

const RULER = 26
const TRACK_KINDS: TrackKind[] = ['video', 'overlay', 'audio', 'caption']

const pps = computed(() => 44 * editor.zoom)
const projectId = computed(() => editor.project?.id ?? '')
const tracks = computed(() => editor.project?.timeline.tracks ?? [])

// Clips that share space on one track are data errors; surface them instead of
// hiding them. Layers on different tracks are expected.
const conflictIds = computed(() => {
  const ids = new Set<string>()
  for (const track of tracks.value) {
    const clips = [...track.clips].sort((a, b) => a.start - b.start)
    for (let i = 1; i < clips.length; i++) {
      const previous = clips[i - 1]
      const current = clips[i]
      if (
        previous.duration > 0 &&
        current.duration > 0 &&
        current.start < previous.start + previous.duration - 1e-4
      ) {
        ids.add(previous.id)
        ids.add(current.id)
      }
    }
  }
  return ids
})

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

const kindLabels: Record<TrackKind, string> = {
  video: 'Video',
  overlay: 'Overlay',
  audio: 'Audio',
  caption: 'Caption',
}

function addTrack(kind: TrackKind) {
  menu.value = null
  const track = editor.addTrack(kind)
  if (!track) return
  nextTick(() => {
    if (!scrollEl.value) return
    const index = tracks.value.findIndex((t) => t.id === track.id)
    const top = RULER + tracks.value.slice(0, Math.max(0, index)).reduce((sum, t) => sum + t.height, 0)
    const visible = scrollEl.value.clientHeight - RULER
    if (top + track.height > scrollEl.value.scrollTop + visible) {
      scrollEl.value.scrollTop = Math.max(0, top + track.height - visible + 6)
    }
    syncHeaderScroll()
  })
}

function toggleMenu(key: string) {
  menu.value = menu.value === key ? null : key
}

function onGlobalPointerDown(event: PointerEvent) {
  const target = event.target as HTMLElement | null
  if (!target?.closest('.menu-anchor')) menu.value = null
  if (!target?.closest('.context-menu')) contextMenu.value = null
}

function focusPanel(event: PointerEvent) {
  ;(event.currentTarget as HTMLElement).focus({ preventScroll: true })
}

interface ResizeState {
  trackId: string
  startY: number
  orig: number
}

let resizeState: ResizeState | null = null

function beginResize(event: PointerEvent, track: Track) {
  if (event.button !== 0) return
  event.preventDefault()
  event.stopPropagation()
  resizeState = { trackId: track.id, startY: event.clientY, orig: track.height }
  window.addEventListener('pointermove', onResizeMove)
  window.addEventListener('pointerup', onResizeEnd, { once: true })
}

function onResizeMove(event: PointerEvent) {
  if (!resizeState) return
  editor.setTrackHeight(resizeState.trackId, resizeState.orig + event.clientY - resizeState.startY)
}

function onResizeEnd() {
  window.removeEventListener('pointermove', onResizeMove)
  const state = resizeState
  resizeState = null
  if (!state) return
  const track = tracks.value.find((t) => t.id === state.trackId)
  if (track) editor.setTrackHeight(state.trackId, track.height, true)
}

onMounted(() => window.addEventListener('pointerdown', onGlobalPointerDown))

function clipAsset(clip: Clip): Asset | undefined {
  return clip.asset_id ? editor.assetMap.get(clip.asset_id) : undefined
}

function clipById(clipId: string): { track: Track; clip: Clip } | null {
  for (const track of tracks.value) {
    const clip = track.clips.find((c) => c.id === clipId)
    if (clip) return { track, clip }
  }
  return null
}

function clipKindClass(track: Track, clip: Clip): string {
  if (!clip.asset_id) return 'text'
  if (track.kind === 'audio') return 'audio'
  return clipAsset(clip)?.kind ?? 'video'
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

function transitionTitle(transition: Transition): string {
  const label = TRANSITIONS.find((item) => item.value === transition.type)?.label ?? transition.type
  return `Transisi ${label} · ${transition.duration.toFixed(2)}s`
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
  contextMenu.value = null
  const startX = event.clientX
  const startY = event.clientY
  const additive = event.shiftKey
  const base = additive ? [...editor.selectionIds] : []
  let moved = false
  const onMove = (ev: PointerEvent) => {
    if (!moved && Math.hypot(ev.clientX - startX, ev.clientY - startY) < 5) return
    moved = true
    const element = contentEl.value
    if (!element) return
    const rect = element.getBoundingClientRect()
    const x1 = Math.min(startX, ev.clientX)
    const x2 = Math.max(startX, ev.clientX)
    const y1 = Math.min(startY, ev.clientY)
    const y2 = Math.max(startY, ev.clientY)
    marquee.value = { left: x1 - rect.left, top: y1 - rect.top, width: x2 - x1, height: y2 - y1 }
    applyMarquee(rect, x1, x2, y1, y2, base)
  }
  const onUp = (ev: PointerEvent) => {
    window.removeEventListener('pointermove', onMove)
    window.removeEventListener('pointerup', onUp)
    marquee.value = null
    if (!moved) {
      if (!additive) editor.clearSelection()
      editor.seek(timeAt(ev.clientX))
    }
  }
  window.addEventListener('pointermove', onMove)
  window.addEventListener('pointerup', onUp)
}

function applyMarquee(
  contentRect: DOMRect,
  minX: number,
  maxX: number,
  minY: number,
  maxY: number,
  base: string[],
) {
  const ids = new Set(base)
  const t1 = (minX - contentRect.left) / pps.value
  const t2 = (maxX - contentRect.left) / pps.value
  for (const track of tracks.value) {
    const lane = contentEl.value?.querySelector<HTMLElement>(`.lane[data-track-id="${track.id}"]`)
    if (!lane) continue
    const rect = lane.getBoundingClientRect()
    if (rect.bottom < minY || rect.top > maxY) continue
    for (const clip of track.clips) {
      if (clip.start <= t2 && clip.start + clip.duration >= t1) ids.add(clip.id)
    }
  }
  const entries = [...ids]
    .map((clipId) => clipById(clipId))
    .filter((found): found is { track: Track; clip: Clip } => Boolean(found))
    .map((found) => ({ trackId: found.track.id, clipId: found.clip.id }))
  editor.setSelection(entries)
}

// ---- clip dragging -----------------------------------------------------
interface DragItem {
  track: Track
  clip: Clip
  start: number
  in: number
  out: number
  duration: number
  maxOut: number
}

interface DragState {
  mode: 'move' | 'trim-l' | 'trim-r'
  trackId: string
  clipId: string
  startX: number
  items: DragItem[]
  linked: DragItem[]
}

let drag: DragState | null = null

function snapshotClip(track: Track, clip: Clip): DragItem {
  const asset = clipAsset(clip)
  return {
    track,
    clip,
    start: clip.start,
    in: clip.in,
    out: clip.out,
    duration: clip.duration,
    maxOut: asset && asset.kind !== 'image' ? asset.duration : Number.POSITIVE_INFINITY,
  }
}

function candidates(excludeIds: Set<string>): number[] {
  const list = [0, editor.playhead]
  for (const track of tracks.value) {
    for (const clip of track.clips) {
      if (excludeIds.has(clip.id)) continue
      list.push(clip.start, clip.start + clip.duration)
    }
  }
  return list
}

function snapWithGuide(value: number, pool: number[], threshold: number): number {
  let best = value
  let bestDelta = threshold
  for (const candidate of pool) {
    const delta = Math.abs(candidate - value)
    if (delta < bestDelta) {
      best = candidate
      bestDelta = delta
    }
  }
  if (best !== value) guide.value = best
  return best
}

function onClipPointerDown(event: PointerEvent, track: Track, clip: Clip) {
  if (event.button !== 0) return
  event.stopPropagation()
  contextMenu.value = null
  if (bladeMode.value) {
    if (!track.locked) editor.splitClipAt(track.id, clip.id, timeAt(event.clientX))
    return
  }
  if (event.shiftKey) {
    editor.select(track.id, clip.id, true)
  } else if (!editor.isSelected(clip.id)) {
    editor.select(track.id, clip.id)
  } else {
    editor.selection = { trackId: track.id, clipId: clip.id }
  }
  if (!editor.isSelected(clip.id) || track.locked) return

  const target = event.target as HTMLElement
  const handle = target.dataset.handle as 'l' | 'r' | undefined
  const primary = { track, clip }
  const others = editor.selectedEntries
    .filter((entry) => entry.clip.id !== clip.id && !entry.track.locked)
    .map((entry) => snapshotClip(entry.track, entry.clip))
  const items = [snapshotClip(primary.track, primary.clip), ...others]
  const linked: DragItem[] = []
  const seen = new Set(items.map((item) => item.clip.id))
  if (!event.altKey) {
    for (const item of items) {
      const partner = item.clip.linked_id ? clipById(item.clip.linked_id) : null
      if (!partner || partner.track.locked || seen.has(partner.clip.id)) continue
      seen.add(partner.clip.id)
      linked.push(snapshotClip(partner.track, partner.clip))
    }
  }
  drag = {
    mode: handle === 'l' ? 'trim-l' : handle === 'r' ? 'trim-r' : 'move',
    trackId: track.id,
    clipId: clip.id,
    startX: event.clientX,
    items,
    linked,
  }
  editor.pushHistory(`drag-${clip.id}`)
  dragTarget.value = null
  guide.value = null
  window.addEventListener('pointermove', onDragMove)
  window.addEventListener('pointerup', onDragEnd, { once: true })
}

function laneAt(clientX: number, clientY: number): Track | null {
  const element = document.elementFromPoint(clientX, clientY) as HTMLElement | null
  const lane = element?.closest('.lane') as HTMLElement | null
  const id = lane?.dataset.trackId
  return id ? tracks.value.find((t) => t.id === id) ?? null : null
}

function onDragMove(event: PointerEvent) {
  if (!drag) return
  const primary = drag.items[0]
  if (!primary || !primary.track.clips.some((c) => c.id === primary.clip.id)) return
  const all = [...drag.items, ...drag.linked]
  const moving = new Set(all.map((item) => item.clip.id))
  const delta = (event.clientX - drag.startX) / pps.value
  const threshold = 8 / pps.value
  const pool = candidates(moving)
  guide.value = null

  const previousEnd = (item: DragItem): number => {
    let boundary = 0
    for (const other of item.track.clips) {
      if (moving.has(other.id)) continue
      const end = other.start + other.duration
      if (end <= item.start + 1e-4 && end > boundary) boundary = end
    }
    return boundary
  }
  const nextStart = (item: DragItem): number => {
    let boundary = Number.POSITIVE_INFINITY
    for (const other of item.track.clips) {
      if (moving.has(other.id)) continue
      if (other.start >= item.start + item.duration - 1e-4 && other.start < boundary) boundary = other.start
    }
    return boundary
  }

  if (drag.mode === 'move') {
    const candidate = laneAt(event.clientX, event.clientY)
    dragTarget.value =
      drag.items.length === 1 &&
      candidate &&
      candidate.id !== primary.track.id &&
      candidate.kind === primary.track.kind &&
      !candidate.locked
        ? candidate.id
        : null

    let start = Math.max(0, primary.start + delta)
    if (snapping.value) {
      const end = start + primary.duration
      const snappedEnd = snapWithGuide(end, pool, threshold)
      if (snappedEnd !== end) {
        start = snappedEnd - primary.duration
      } else {
        start = snapWithGuide(start, pool, threshold)
      }
    }
    const minStart = Math.min(...all.map((item) => item.start))
    const shift = Math.max(start - primary.start, -minStart)
    for (const item of all) item.clip.start = Math.max(0, item.start + shift)
    dragConflict.value = dragCollides()
    return
  }

  if (drag.mode === 'trim-l') {
    const low = Math.max(
      ...all.map((item) => Math.max(-item.start, -item.in, previousEnd(item) - item.start)),
    )
    const high = Math.min(...all.map((item) => item.out - 0.1 - item.in))
    let shift = clamp(delta, low, high)
    if (snapping.value) {
      let newStart = primary.start + shift
      const snapped = snapWithGuide(newStart, pool, threshold)
      shift = clamp(snapped - primary.start, low, high)
      newStart = primary.start + shift
    }
    for (const item of all) {
      item.clip.in = item.in + shift
      item.clip.start = item.start + shift
      item.clip.out = item.out
      item.clip.duration = item.duration - shift
    }
    dragConflict.value = false
    return
  }

  const low = Math.max(...all.map((item) => -(item.duration - 0.1)))
  const high = Math.min(
    ...all.map((item) => Math.min(item.maxOut - item.out, nextStart(item) - (item.start + item.duration))),
  )
  let shift = clamp(delta, low, high)
  if (snapping.value) {
    const newOut = primary.start + primary.duration + shift
    const snapped = snapWithGuide(newOut, pool, threshold)
    shift = clamp(snapped - (primary.start + primary.duration), low, high)
  }
  for (const item of all) {
    item.clip.out = item.out + shift
    item.clip.duration = item.duration + shift
  }
  dragConflict.value = false
}

function dragCollides(): boolean {
  if (!drag) return false
  const all = [...drag.items, ...drag.linked]
  const moving = new Set(all.map((item) => item.clip.id))
  for (let i = 0; i < all.length; i++) {
    const item = all[i]
    let track = item.track
    if (i === 0 && dragTarget.value) {
      track = tracks.value.find((t) => t.id === dragTarget.value) ?? item.track
    }
    if (editor.findCollision(track, item.clip.start, item.clip.duration, moving)) return true
  }
  return false
}

function onDragEnd() {
  window.removeEventListener('pointermove', onDragMove)
  const current = drag
  const target = dragTarget.value
  const conflicting = dragConflict.value
  drag = null
  dragTarget.value = null
  dragConflict.value = false
  guide.value = null
  if (current) {
    if (conflicting) {
      for (const item of [...current.items, ...current.linked]) {
        item.clip.start = item.start
        item.clip.in = item.in
        item.clip.out = item.out
        item.clip.duration = item.duration
      }
      editor.notify('Clip bertabrakan dengan clip lain di track ini.', 'error')
      return
    }
    if (current.mode === 'move' && current.items.length === 1 && target && target !== current.trackId) {
      editor.moveClipToTrack(current.clipId, current.trackId, target)
    }
  }
  editor.scheduleSave()
  void editor.flushSave()
}

// ---- context menu ------------------------------------------------------
function openClipMenu(event: MouseEvent, track: Track, clip: Clip) {
  event.preventDefault()
  event.stopPropagation()
  if (!editor.isSelected(clip.id)) editor.select(track.id, clip.id)
  contextMenu.value = {
    x: Math.min(event.clientX, window.innerWidth - 210),
    y: Math.min(event.clientY, window.innerHeight - 340),
    trackId: track.id,
    clipId: clip.id,
    kind: 'clip',
    time: timeAt(event.clientX),
  }
}

const menuClip = computed(() => {
  const context = contextMenu.value
  if (!context || context.kind !== 'clip') return null
  return clipById(context.clipId)
})

const menuCanTransition = computed(() => {
  const found = menuClip.value
  return !!found && found.track.kind === 'video' && clipAsset(found.clip)?.kind === 'video'
})

const menuHasAudio = computed(() => {
  const found = menuClip.value
  if (!found || found.track.kind !== 'video') return false
  const asset = clipAsset(found.clip)
  return asset?.kind === 'video' && !!asset.has_audio
})

function clipHasAudio(clip: Clip): boolean {
  if (!clip.asset_id) return false
  const asset = editor.assetMap.get(clip.asset_id)
  return asset?.kind === 'video' && !!asset.has_audio
}

function setMenuVolume(value: number) {
  const found = menuClip.value
  if (!found) return
  editor.updateClip(found.track.id, found.clip.id, { volume: clamp(value, 0, 2) }, `vol-${found.clip.id}`)
}

function toggleMenuMute() {
  const found = menuClip.value
  if (!found) return
  const next = found.clip.volume > 0 ? 0 : 1
  editor.updateClip(found.track.id, found.clip.id, { volume: next }, `vol-${found.clip.id}`)
}

function applyTransition(type: TransitionType | 'none') {
  const clipId = contextMenu.value?.clipId
  contextMenu.value = null
  if (clipId) editor.setClipTransition(type, undefined, clipId)
}

function openLaneMenu(event: MouseEvent, track: Track) {
  if ((event.target as HTMLElement).closest('.clip')) return
  event.preventDefault()
  contextMenu.value = {
    x: Math.min(event.clientX, window.innerWidth - 190),
    y: Math.min(event.clientY, window.innerHeight - 150),
    trackId: track.id,
    clipId: '',
    kind: 'lane',
    time: timeAt(event.clientX),
  }
}

function runMenuAction(action: 'split' | 'copy' | 'cut' | 'paste' | 'delete' | 'ripple') {
  contextMenu.value = null
  switch (action) {
    case 'split':
      editor.splitAtPlayhead()
      break
    case 'copy':
      editor.copySelected()
      break
    case 'cut':
      editor.cutSelected()
      break
    case 'paste':
      editor.pasteAtPlayhead()
      break
    case 'delete':
      editor.deleteSelected(false)
      break
    case 'ripple':
      editor.deleteSelected(true)
      break
  }
}

onBeforeUnmount(() => {
  window.removeEventListener('pointermove', onDragMove)
  window.removeEventListener('pointerup', onDragEnd)
  window.removeEventListener('pointermove', onResizeMove)
  window.removeEventListener('pointerdown', onGlobalPointerDown)
})

// ---- toolbar -----------------------------------------------------------
function syncHeaderScroll() {
  if (!headerListEl.value || !scrollEl.value) return
  headerListEl.value.style.transform = `translateY(${-scrollEl.value.scrollTop}px)`
}

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
  const element = scrollEl.value
  if (!element) return

  if (event.ctrlKey || event.metaKey) {
    event.preventDefault()
    const rect = element.getBoundingClientRect()
    const pointerX = event.clientX - rect.left
    const time = (element.scrollLeft + pointerX) / pps.value
    const factor = Math.exp(-event.deltaY * 0.002)
    editor.zoom = clamp(editor.zoom * factor, 0.2, 4)
    nextTick(() => {
      element.scrollLeft = Math.max(0, time * pps.value - pointerX)
      syncHeaderScroll()
    })
    return
  }

  if (event.shiftKey) {
    event.preventDefault()
    element.scrollLeft += event.deltaY
    return
  }

  if (event.deltaX !== 0) return

  const canScrollVertically = element.scrollHeight > element.clientHeight + 1
  if (canScrollVertically) return

  event.preventDefault()
  element.scrollLeft += event.deltaY
}

watch(
  () => tracks.value.map((track) => track.height).join(','),
  () => nextTick(syncHeaderScroll),
)

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
  <section class="timeline-panel" tabindex="-1" @pointerdown.capture="focusPanel">
    <div class="timeline-toolbar">
      <button class="icon-btn" title="Split di playhead (Ctrl+B)" @click="editor.splitAtPlayhead()">
        <Scissors :size="15" />
      </button>
      <button
        class="icon-btn"
        :class="{ active: bladeMode }"
        title="Blade: klik clip untuk memotong di kursor"
        @click="bladeMode = !bladeMode"
      >
        <Slice :size="15" />
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
        :class="{ active: editor.rippleDelete }"
        title="Ripple delete: tutup gap setelah hapus"
        @click="editor.rippleDelete = !editor.rippleDelete"
      >
        <ArrowLeftRight :size="15" />
      </button>
      <button
        class="icon-btn"
        :disabled="!editor.canPaste"
        title="Tempel di playhead (Ctrl+V)"
        @click="editor.pasteAtPlayhead()"
      >
        <ClipboardPaste :size="15" />
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
      <div class="menu-anchor track-add">
        <button class="icon-btn" title="Tambah track" @click.stop="toggleMenu('add')">
          <Plus :size="15" />
        </button>
        <div v-if="menu === 'add'" class="track-menu add-menu" @click.stop>
          <button v-for="kind in TRACK_KINDS" :key="kind" @click="addTrack(kind)">
            <component :is="trackIcon(kind)" :size="13" />
            {{ kindLabels[kind] }}
          </button>
        </div>
      </div>
      <button
        class="btn btn-ghost edit-mode"
        :title="editor.editMode === 'overwrite' ? 'Mode overwrite: clip baru menimpa area' : 'Mode insert: clip baru mendorong clip lain'"
        @click="editor.editMode = editor.editMode === 'overwrite' ? 'insert' : 'overwrite'"
      >
        {{ editor.editMode === 'overwrite' ? 'Overwrite' : 'Insert' }}
      </button>

      <div class="toolbar-info">
        <span v-if="editor.selectionCount > 1" class="badge accent">{{ editor.selectionCount }} dipilih</span>
        <span v-if="conflictIds.size" class="badge danger" title="Clip bertumpuk di track yang sama">
          {{ conflictIds.size }} bentrok
        </span>
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
        <div class="track-head-viewport">
          <div ref="headerListEl" class="track-head-list">
            <div
              v-for="(track, trackIndex) in tracks"
              :key="track.id"
              class="track-head menu-anchor"
              :class="{ hidden: track.hidden }"
              :style="{ height: track.height + 'px' }"
            >
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
                  :class="{ off: track.hidden }"
                  :title="track.hidden ? 'Tampilkan track' : 'Sembunyikan track'"
                  @click.stop="editor.setTrackHidden(track.id, !track.hidden)"
                >
                  <EyeOff v-if="track.hidden" :size="12" />
                  <Eye v-else :size="12" />
                </button>
                <button class="icon-btn tiny" title="Menu track" @click.stop="toggleMenu(track.id)">
                  <MoreVertical :size="12" />
                </button>
              </div>

              <div v-if="menu === track.id" class="track-menu" @click.stop>
                <button
                  v-if="track.kind === 'audio' || track.kind === 'video'"
                  @click="editor.setTrackMuted(track.id, !track.muted); menu = null"
                >
                  <Volume2 v-if="track.muted" :size="13" />
                  <VolumeX v-else :size="13" />
                  {{ track.muted ? 'Nyalakan suara' : 'Bisukan' }}
                </button>
                <button @click="editor.setTrackLocked(track.id, !track.locked); menu = null">
                  <Lock v-if="track.locked" :size="13" />
                  <Unlock v-else :size="13" />
                  {{ track.locked ? 'Buka kunci' : 'Kunci track' }}
                </button>
                <button :disabled="trackIndex === 0" @click="editor.moveTrack(track.id, -1); menu = null">
                  <ChevronUp :size="13" /> Naik
                </button>
                <button :disabled="trackIndex === tracks.length - 1" @click="editor.moveTrack(track.id, 1); menu = null">
                  <ChevronDown :size="13" /> Turun
                </button>
                <button class="danger" @click="editor.removeTrack(track.id); menu = null">
                  <Trash2 :size="13" /> Hapus track
                </button>
              </div>

              <div class="track-resize" @pointerdown="beginResize($event, track)" />
            </div>
          </div>
        </div>
      </div>

      <div ref="scrollEl" class="timeline-scroll" @wheel="onWheel" @scroll="syncHeaderScroll">
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
            <PlayheadMarker variant="cap" :pps="pps" />
          </div>

          <div
            v-for="track in tracks"
            :key="track.id"
            class="lane"
            :class="[
              track.kind,
              {
                locked: track.locked,
                hidden: track.hidden,
                blade: bladeMode,
                'drop-target': dragTarget === track.id,
                'drag-conflict': dragConflict,
              },
            ]"
            :data-track-id="track.id"
            :style="{ height: track.height + 'px', ...gridStyle }"
            @pointerdown="onLanePointerDown"
            @contextmenu="openLaneMenu($event, track)"
          >
            <div
              v-for="clip in track.clips"
              :key="clip.id"
              class="clip"
              :class="[
                clipKindClass(track, clip),
                {
                  selected: editor.isSelected(clip.id),
                  primary: editor.selection?.clipId === clip.id,
                  muted: track.muted,
                  'track-hidden': track.hidden,
                  conflict: conflictIds.has(clip.id),
                },
              ]"
              :style="clipStyle(clip)"
              @pointerdown="onClipPointerDown($event, track, clip)"
              @contextmenu="openClipMenu($event, track, clip)"
            >
              <div
                v-if="track.kind !== 'audio' && clippedThumb(clip)"
                class="clip-thumbs"
                :style="{ backgroundImage: clippedThumb(clip) }"
              />
              <div v-if="clip.transition" class="clip-transition" :title="transitionTitle(clip.transition)">
                <Maximize2 v-if="clip.transition.type === 'pull_in'" :size="10" />
                <MoveRight v-else-if="clip.transition.type === 'slide_right'" :size="10" />
                <MoveLeft v-else :size="10" />
              </div>
              <Waveform
                v-if="track.kind === 'audio' && clip.asset_id"
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

              <span
                v-if="clip.keyframes?.length"
                class="clip-kf"
                :title="`${clip.keyframes.length} keyframe transform`"
              >
                <Diamond :size="8" fill="currentColor" />
                {{ clip.keyframes.length }}
              </span>

              <button
                v-if="clipHasAudio(clip)"
                class="clip-audio"
                :class="{ off: clip.volume === 0 }"
                :title="`Audio bawaan video · ${Math.round(clip.volume * 100)}% — klik untuk atur`"
                @pointerdown.stop
                @click.stop="openClipMenu($event, track, clip)"
              >
                <Volume2 :size="10" />
                <span>{{ Math.round(clip.volume * 100) }}</span>
              </button>

              <div
                v-if="editor.isSelected(clip.id) && !track.locked"
                class="trim-handle left"
                data-handle="l"
              />
              <div
                v-if="editor.isSelected(clip.id) && !track.locked"
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

          <div v-if="guide !== null" class="snap-guide" :style="{ left: guide * pps + 'px' }" />
          <div
            v-if="marquee"
            class="marquee"
            :style="{
              left: marquee.left + 'px',
              top: marquee.top + 'px',
              width: marquee.width + 'px',
              height: marquee.height + 'px',
            }"
          />
          <PlayheadMarker variant="line" :pps="pps" @scrub="beginScrub" />
        </div>
      </div>
    </div>

    <Teleport to="body">
      <div
        v-if="contextMenu"
        class="context-menu"
        :style="{ left: contextMenu.x + 'px', top: contextMenu.y + 'px' }"
        @pointerdown.stop
      >
        <template v-if="contextMenu.kind === 'clip'">
          <button @click="runMenuAction('split')">Potong di playhead</button>
          <template v-if="menuCanTransition">
            <div class="menu-label">Transisi masuk</div>
            <button @click="editor.applyTransitionToAllScenes('variety'); contextMenu = null">
              ✨ Auto variasi semua scene
            </button>
            <button
              v-for="transition in TRANSITIONS"
              :key="transition.value"
              :class="{ active: menuClip?.clip.transition?.type === transition.value }"
              :title="transition.hint"
              @click="applyTransition(transition.value)"
            >
              {{ transition.label }}
            </button>
            <button
              v-if="menuClip?.clip.transition"
              @click="editor.applyTransitionToAllScenes(menuClip.clip.transition.type, menuClip.clip.transition.duration); contextMenu = null"
            >
              Terapkan {{ menuClip.clip.transition.type }} ke semua scene
            </button>
            <button v-if="menuClip?.clip.transition" @click="applyTransition('none')">Tanpa transisi</button>
          </template>
          <template v-if="menuHasAudio">
            <div class="menu-label">Audio bawaan video</div>
            <div class="menu-volume">
              <Volume2 :size="12" />
              <input
                type="range"
                min="0"
                max="2"
                step="0.01"
                :value="menuClip?.clip.volume ?? 1"
                @pointerdown.stop
                @input="setMenuVolume(Number(($event.target as HTMLInputElement).value))"
              />
              <span>{{ Math.round((menuClip?.clip.volume ?? 1) * 100) }}%</span>
            </div>
            <button @click="toggleMenuMute">
              {{ (menuClip?.clip.volume ?? 1) > 0 ? 'Bisukan audio' : 'Nyalakan audio' }}
            </button>
          </template>
          <button @click="runMenuAction('copy')"><Copy :size="13" /> Salin</button>
          <button @click="runMenuAction('cut')">Potong ke clipboard</button>
          <button @click="runMenuAction('delete')">Hapus</button>
          <button class="danger" @click="runMenuAction('ripple')">Hapus + rapatkan</button>
        </template>
        <template v-else>
          <button :disabled="!editor.canPaste" @click="runMenuAction('paste')">Tempel di playhead</button>
        </template>
      </div>
    </Teleport>
  </section>
</template>

<style scoped>
.timeline-panel {
  container-type: inline-size;
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
  background: #0d0d0d;
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
  font-size: 11px;
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
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--border-soft);
  background: #0d0d0d;
  z-index: 3;
}

.ruler-spacer {
  flex-shrink: 0;
  border-bottom: 1px solid var(--border-soft);
}

.track-head-viewport {
  flex: 1;
  min-height: 0;
  position: relative;
  overflow: hidden;
}

.track-head-list {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  will-change: transform;
}

.track-head {
  position: relative;
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 0 6px 0 9px;
  border-bottom: 1px solid var(--border-soft);
}

.track-head.hidden {
  opacity: 0.55;
}

.track-add {
  position: relative;
  display: flex;
}

.track-menu {
  position: absolute;
  top: 30px;
  left: calc(100% - 4px);
  z-index: 30;
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 152px;
  padding: 4px;
  border-radius: 8px;
  background: var(--panel-2);
  border: 1px solid var(--border);
}

.track-menu.add-menu {
  left: 0;
}

.track-menu button {
  display: flex;
  align-items: center;
  gap: 7px;
  width: 100%;
  padding: 6px 8px;
  border: none;
  border-radius: 5px;
  background: transparent;
  color: var(--text);
  font-size: 11px;
  text-align: left;
  cursor: pointer;
}

.track-menu button:hover:not(:disabled) {
  background: rgba(255, 255, 255, 0.07);
}

.track-menu button:disabled {
  opacity: 0.35;
  cursor: default;
}

.track-menu button.danger {
  color: var(--danger);
}

.track-resize {
  position: absolute;
  left: 0;
  right: 0;
  bottom: -2px;
  height: 5px;
  cursor: ns-resize;
  z-index: 7;
}

.track-resize:hover {
  background: #6b6b6b;
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

.track-kind.video,
.track-kind.audio,
.track-kind.overlay,
.track-kind.caption {
  color: var(--muted);
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
  font-size: 11px;
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
  overflow-y: auto;
  background: #050505;
}

.timeline-content {
  position: relative;
  min-height: 100%;
}

.ruler {
  position: sticky;
  top: 0;
  z-index: 8;
  border-bottom: 1px solid var(--border-soft);
  background: #0d0d0d;
  cursor: col-resize;
}

.tick {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 1px;
  background: #242424;
}

.tick span {
  position: absolute;
  top: 4px;
  left: 5px;
  font-size: 11px;
  color: var(--muted-2);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.lane {
  position: relative;
  border-bottom: 1px solid var(--border-soft);
  background: #050505;
  overflow: hidden;
}

.lane.locked,
.lane.hidden {
  background: #0d0d0d;
}

.lane.drop-target {
  background: var(--panel-2);
  box-shadow: inset 0 0 0 2px #6b6b6b;
}

.lane.drag-conflict {
  background: #150d0d;
  box-shadow: inset 0 0 0 2px var(--danger);
}

.lane-hint {
  position: absolute;
  left: 12px;
  top: 50%;
  transform: translateY(-50%);
  font-size: 11px;
  color: #6b6b6b;
  pointer-events: none;
}

.clip {
  position: absolute;
  top: 4px;
  bottom: 4px;
  border-radius: 6px;
  overflow: hidden;
  cursor: grab;
  background: var(--panel-3);
  border: 1px solid #3a3a3a;
  color: var(--text);
  touch-action: none;
}

.clip.audio,
.clip.image,
.clip.text,
.clip.video {
  background: var(--panel-3);
  border-color: #3a3a3a;
  color: var(--text);
}

.clip.selected {
  border-color: var(--text);
  box-shadow: 0 0 0 1px var(--text);
  z-index: 4;
}

.clip.conflict {
  border-color: var(--danger);
  box-shadow: 0 0 0 1px var(--danger);
}

.clip-transition {
  position: absolute;
  top: 3px;
  left: 4px;
  display: flex;
  align-items: center;
  padding: 1px 3px;
  border-radius: 3px;
  background: rgba(0, 0, 0, 0.68);
  color: var(--accent);
  pointer-events: none;
  z-index: 3;
}

.clip-audio {
  position: absolute;
  top: 3px;
  right: 4px;
  display: flex;
  align-items: center;
  gap: 3px;
  height: 16px;
  padding: 0 4px;
  border-radius: 3px;
  border: 1px solid rgba(198, 242, 78, 0.4);
  background: rgba(0, 0, 0, 0.68);
  color: var(--accent);
  font-size: 9px;
  font-weight: 600;
  line-height: 1;
  z-index: 3;
}

.clip-audio:hover {
  background: var(--accent);
  color: #000;
}

.clip-kf {
  position: absolute;
  left: 4px;
  bottom: 3px;
  display: flex;
  align-items: center;
  gap: 2px;
  height: 15px;
  padding: 0 4px;
  border-radius: 3px;
  background: rgba(0, 0, 0, 0.68);
  color: var(--accent);
  font-size: 9px;
  font-weight: 600;
  line-height: 1;
  pointer-events: none;
  z-index: 3;
}

.clip-audio.off {
  color: var(--muted);
  border-color: rgba(255, 255, 255, 0.16);
}

.clip.primary {
  box-shadow: inset 0 0 0 1px var(--text);
}

.lane.blade,
.lane.blade .clip {
  cursor: crosshair;
}

.badge.accent {
  background: var(--panel-3);
  color: var(--muted);
  border-color: var(--border);
}

.edit-mode {
  height: 24px;
  padding: 0 9px;
  font-size: 11px;
}

.snap-guide {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 1px;
  background: var(--accent);
  pointer-events: none;
  z-index: 4;
}

.marquee {
  position: absolute;
  border: 1px solid var(--text);
  background: rgba(255, 255, 255, 0.08);
  border-radius: 2px;
  pointer-events: none;
  z-index: 6;
}

.context-menu {
  position: fixed;
  z-index: 90;
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 176px;
  padding: 4px;
  border-radius: 8px;
  background: var(--panel-2);
  border: 1px solid var(--border);
}

.context-menu button {
  display: flex;
  align-items: center;
  gap: 7px;
  width: 100%;
  padding: 6px 9px;
  border: none;
  border-radius: 5px;
  background: transparent;
  color: var(--text);
  font-size: 11px;
  text-align: left;
  cursor: pointer;
}

.context-menu button:hover:not(:disabled) {
  background: rgba(255, 255, 255, 0.07);
}

.context-menu button.active {
  background: var(--panel-3);
  color: var(--text);
}

.menu-volume {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 4px 9px 7px;
  color: var(--muted);
}

.menu-volume input[type='range'] {
  flex: 1;
  min-width: 90px;
  accent-color: var(--accent);
}

.menu-volume span {
  min-width: 32px;
  text-align: right;
  font-size: 10.5px;
  color: var(--text);
  font-variant-numeric: tabular-nums;
}

.menu-label {
  padding: 6px 9px 2px;
  font-size: 10px;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--muted-2);
}

.badge.danger {
  color: var(--danger);
  border-color: #52302f;
}

.context-menu button:disabled {
  opacity: 0.35;
  cursor: default;
}

.context-menu button.danger {
  color: var(--danger);
}

.clip.muted {
  opacity: 0.45;
}

.clip.track-hidden {
  opacity: 0.35;
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
  font-size: 11px;
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
  opacity: 0.7;
  font-variant-numeric: tabular-nums;
  font-size: 11px;
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

:global(body.asset-dragging) .lane {
  cursor: copy;
}

:global(body.asset-dragging) .lane:hover {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
}
@container (max-width: 830px) {
  .toolbar-info .badge:not(.accent, .danger) { display: none; }
  .zoom-block { gap: 3px; }
  .zoom-block input[type='range'] { width: 62px; }
}
@container (max-width: 660px) {
  .timeline-toolbar { height: auto; min-height: 40px; flex-wrap: wrap; padding-block: 3px; }
}
</style>
