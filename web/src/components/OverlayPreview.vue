<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch, type CSSProperties } from 'vue'
import { assetUrl } from '../api'
import { useEditor } from '../stores/editor'
import type { Asset, Clip, Project } from '../types'

type OverlayItem = { clip: Clip; asset?: Asset; trackId: string }

const props = defineProps<{
  overlays: OverlayItem[]
  project: Project
  projectId: string
  displayHeight: number
}>()

const editor = useEditor()
const layerEl = ref<HTMLElement | null>(null)
const videoRefs = ref<Record<string, HTMLVideoElement | null>>({})

const selectedId = computed(() => editor.selection?.clipId ?? null)

function isTextOverlay(item: OverlayItem): boolean {
  return !!item.clip.text?.trim()
}

type OverlayDrag = {
  mode: 'move' | 'scale'
  item: OverlayItem
  rect: DOMRect
  startX: number
  startY: number
  start: { x: number; y: number; scale: number }
  center: { x: number; y: number }
  startDistance: number
}

let overlayDrag: OverlayDrag | null = null

function beginOverlayDrag(event: PointerEvent, item: OverlayItem, mode: 'move' | 'scale') {
  if (!layerEl.value) return
  const track = editor.project?.timeline.tracks.find((entry) => entry.id === item.trackId)
  if (track?.locked) return
  event.preventDefault()
  event.stopPropagation()
  editor.select(item.trackId, item.clip.id)

  const rect = layerEl.value.getBoundingClientRect()
  const start = { x: item.clip.x, y: item.clip.y, scale: item.clip.scale || 1 }
  const center = { x: rect.left + start.x * rect.width, y: rect.top + start.y * rect.height }
  overlayDrag = {
    mode,
    item,
    rect,
    startX: event.clientX,
    startY: event.clientY,
    start,
    center,
    startDistance: Math.max(1, Math.hypot(event.clientX - center.x, event.clientY - center.y)),
  }
  window.addEventListener('pointermove', onOverlayDragMove)
  window.addEventListener('pointerup', onOverlayDragEnd, { once: true })
}

function onOverlayDragMove(event: PointerEvent) {
  const drag = overlayDrag
  if (!drag) return
  if (drag.mode === 'move') {
    const x = drag.start.x + (event.clientX - drag.startX) / drag.rect.width
    const y = drag.start.y + (event.clientY - drag.startY) / drag.rect.height
    editor.updateClip(
      drag.item.trackId,
      drag.item.clip.id,
      { x: Math.min(1, Math.max(0, x)), y: Math.min(1, Math.max(0, y)) },
      `overlay-move-${drag.item.clip.id}`,
    )
    return
  }
  const distance = Math.hypot(event.clientX - drag.center.x, event.clientY - drag.center.y)
  const scale = (drag.start.scale * distance) / drag.startDistance
  editor.updateClip(
    drag.item.trackId,
    drag.item.clip.id,
    { scale: Math.min(3, Math.max(0.1, scale)) },
    `overlay-scale-${drag.item.clip.id}`,
  )
}

function onOverlayDragEnd() {
  overlayDrag = null
  window.removeEventListener('pointermove', onOverlayDragMove)
}

function hexToRgba(hex: string, alpha: number): string {
  const value = (hex || '#000000').replace('#', '')
  const full = value.length === 3 ? value.split('').map((c) => c + c).join('') : value.padEnd(6, '0')
  const r = parseInt(full.slice(0, 2), 16) || 0
  const g = parseInt(full.slice(2, 4), 16) || 0
  const b = parseInt(full.slice(4, 6), 16) || 0
  return `rgba(${r}, ${g}, ${b}, ${Math.max(0, Math.min(1, alpha))})`
}

function wrapperStyle(clip: Clip): CSSProperties {
  return {
    left: `${clip.x * 100}%`,
    top: `${clip.y * 100}%`,
    transform: `translate(-50%, -50%) rotate(${clip.rotation}deg)`,
    opacity: clip.opacity,
    mixBlendMode: blendCss(clip.blend_mode) as CSSProperties['mixBlendMode'],
  }
}

function blendCss(mode: string): string {
  const map: Record<string, string> = {
    normal: 'normal',
    multiply: 'multiply',
    screen: 'screen',
    overlay: 'overlay',
    softlight: 'soft-light',
    hardlight: 'hard-light',
    addition: 'plus-lighter',
    difference: 'difference',
    darken: 'darken',
    lighten: 'lighten',
    dodge: 'color-dodge',
    burn: 'color-burn',
  }
  return map[mode] || 'normal'
}

function textStyle(clip: Clip) {
  const style = clip.style
  if (!style) return {}
  const px = Math.max(6, style.font_size * (props.displayHeight / 1080) * (clip.scale || 1))
  const stroke = Math.max(0, style.stroke_width * (props.displayHeight / 1080) * (clip.scale || 1))
  const shadow = Math.max(0, style.shadow * (props.displayHeight / 1080) * (clip.scale || 1))
  return {
    fontFamily: `"${style.font_family}", Arial, sans-serif`,
    fontSize: `${px}px`,
    fontWeight: style.bold ? '700' : '400',
    fontStyle: style.italic ? 'italic' : 'normal',
    color: style.color,
    textTransform: (style.uppercase ? 'uppercase' : 'none') as 'uppercase' | 'none',
    WebkitTextStroke: stroke > 0.1 ? `${stroke}px ${style.stroke_color}` : '0px transparent',
    textShadow: shadow > 0.1 ? `0 ${shadow}px ${shadow * 1.6}px ${style.shadow_color}` : 'none',
    background: style.background_alpha > 0 ? hexToRgba(style.background_color, style.background_alpha) : 'transparent',
    padding: style.background_alpha > 0 ? `${px * 0.18}px ${px * 0.32}px` : '0',
    borderRadius: style.background_alpha > 0 ? `${Math.max(2, px * 0.1)}px` : '0',
    lineHeight: 1.22,
    maxWidth: `${props.project.width * 0.86 * (props.displayHeight / props.project.height)}px`,
    whiteSpace: 'pre-wrap' as const,
    textAlign: 'center' as const,
  }
}

function mediaStyle(clip: Clip, asset?: Asset) {
  if (!asset || !asset.width || !asset.height) return { width: '100%' }
  const fit = Math.min(props.project.width / asset.width, props.project.height / asset.height)
  const width = asset.width * fit * (clip.scale || 1) * (props.displayHeight / props.project.height)
  return { width: `${width}px`, height: 'auto' }
}

function syncVideo(clip: Clip, element: HTMLVideoElement | null) {
  if (!element) return
  const expected = Math.min(Math.max(clip.in, clip.in + (editor.playhead - clip.start)), clip.out)
  const drift = element.currentTime - expected
  if (Math.abs(drift) > 0.45) {
    element.currentTime = expected
    element.playbackRate = 1
  } else if (editor.playing && Math.abs(drift) > 0.12) {
    // Nudge the rate instead of seeking; repeated seeks stall WebView2.
    element.playbackRate = Math.min(1.08, Math.max(0.92, 1 - drift * 0.5))
  } else if (element.playbackRate !== 1) {
    element.playbackRate = 1
  }
  if (editor.playing) {
    if (element.paused) element.play().catch(() => undefined)
  } else if (!element.paused) {
    element.pause()
  }
}

function setVideoRef(id: string, el: HTMLVideoElement | null) {
  videoRefs.value[id] = el
  if (el) {
    const found = props.overlays.find((o) => o.clip.id === id)
    if (found) syncVideo(found.clip, el)
  }
}

watch(
  () => [editor.playhead, editor.playing],
  () => {
    for (const overlay of props.overlays) {
      syncVideo(overlay.clip, videoRefs.value[overlay.clip.id] ?? null)
    }
  },
)

onMounted(() => {
  for (const overlay of props.overlays) syncVideo(overlay.clip, videoRefs.value[overlay.clip.id] ?? null)
})

onBeforeUnmount(() => {
  overlayDrag = null
  window.removeEventListener('pointermove', onOverlayDragMove)
  window.removeEventListener('pointerup', onOverlayDragEnd)
})

function startMove(event: PointerEvent, item: OverlayItem) {
  if (isTextOverlay(item)) beginOverlayDrag(event, item, 'move')
}

const visible = computed(() => props.overlays.filter((o) => o.clip.text?.trim() || o.asset))
</script>

<template>
  <div ref="layerEl" class="overlay-layer">
    <div
      v-for="overlay in visible"
      :key="overlay.clip.id"
      class="overlay-item"
      :class="{ text: isTextOverlay(overlay), selected: selectedId === overlay.clip.id }"
      :style="wrapperStyle(overlay.clip)"
      @pointerdown="startMove($event, overlay)"
      @click.stop
    >
      <div v-if="overlay.clip.text?.trim()" class="overlay-text" :style="textStyle(overlay.clip)">
        {{ overlay.clip.text }}
      </div>
      <img
        v-else-if="overlay.asset && overlay.asset.kind === 'image'"
        :src="assetUrl(projectId, overlay.asset.id)"
        :style="mediaStyle(overlay.clip, overlay.asset)"
        alt=""
        draggable="false"
      />
      <video
        v-else-if="overlay.asset"
        :ref="(el) => setVideoRef(overlay.clip.id, el as HTMLVideoElement | null)"
        :src="assetUrl(projectId, overlay.asset.id)"
        :style="mediaStyle(overlay.clip, overlay.asset)"
        muted
        playsinline
      />
      <template v-if="isTextOverlay(overlay) && selectedId === overlay.clip.id">
        <button
          v-for="corner in ['nw', 'ne', 'se', 'sw']"
          :key="corner"
          class="overlay-handle"
          :class="corner"
          title="Tarik untuk scale teks"
          @pointerdown.stop.prevent="beginOverlayDrag($event, overlay, 'scale')"
          @click.stop
        />
      </template>
    </div>
  </div>
</template>

<style scoped>
.overlay-layer {
  position: absolute;
  inset: 0;
  pointer-events: none;
  overflow: hidden;
}

.overlay-item {
  position: absolute;
  transform-origin: center;
  display: flex;
  align-items: center;
  justify-content: center;
}

.overlay-item.text {
  pointer-events: auto;
  cursor: grab;
  touch-action: none;
}

.overlay-item.text:active {
  cursor: grabbing;
}

.overlay-item.selected::after {
  content: '';
  position: absolute;
  inset: -7px;
  border: 1px dashed var(--accent);
  border-radius: 4px;
  pointer-events: none;
}

.overlay-handle {
  position: absolute;
  width: 12px;
  height: 12px;
  padding: 0;
  border-radius: 2px;
  border: 1px solid #08080b;
  background: var(--accent);
  cursor: nwse-resize;
}

.overlay-handle.nw {
  left: -6px;
  top: -6px;
}

.overlay-handle.ne {
  right: -6px;
  top: -6px;
  cursor: nesw-resize;
}

.overlay-handle.se {
  right: -6px;
  bottom: -6px;
}

.overlay-handle.sw {
  left: -6px;
  bottom: -6px;
  cursor: nesw-resize;
}

.overlay-text {
  user-select: none;
}

img,
video {
  display: block;
  max-width: none;
  user-select: none;
}
</style>
