<script setup lang="ts">
import { computed, onMounted, ref, watch, type CSSProperties } from 'vue'
import { assetUrl } from '../api'
import type { Asset, Clip, Project } from '../types'

const props = defineProps<{
  overlays: Array<{ clip: Clip; asset?: Asset }>
  project: Project
  projectId: string
  displayHeight: number
  playing: boolean
  playhead: number
}>()

const videoRefs = ref<Record<string, HTMLVideoElement | null>>({})

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
    maxWidth: `${props.project.width * 0.86}px`,
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
  const expected = Math.min(Math.max(clip.in, clip.in + (props.playhead - clip.start)), clip.out)
  if (Math.abs(element.currentTime - expected) > 0.28) {
    element.currentTime = expected
  }
  if (props.playing) {
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
  () => [props.playhead, props.playing],
  () => {
    for (const overlay of props.overlays) {
      syncVideo(overlay.clip, videoRefs.value[overlay.clip.id] ?? null)
    }
  },
)

onMounted(() => {
  for (const overlay of props.overlays) syncVideo(overlay.clip, videoRefs.value[overlay.clip.id] ?? null)
})

const visible = computed(() => props.overlays.filter((o) => o.clip.text?.trim() || o.asset))
</script>

<template>
  <div class="overlay-layer">
    <div v-for="overlay in visible" :key="overlay.clip.id" class="overlay-item" :style="wrapperStyle(overlay.clip)">
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
