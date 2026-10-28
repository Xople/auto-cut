<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Music, Pause, Play, SkipBack, SkipForward, Volume2, VolumeX } from 'lucide-vue-next'
import { assetUrl } from '../api'
import { useEditor } from '../stores/editor'
import { formatTime, timecode } from '../lib/format'
import OverlayPreview from './OverlayPreview.vue'
import CaptionPreview from './CaptionPreview.vue'
import type { Asset, Clip } from '../types'

const editor = useEditor()
const shell = ref<HTMLElement | null>(null)
const stageSize = ref({ width: 0, height: 0 })
const shellSize = ref({ width: 0, height: 0 })
const videoEl = ref<HTMLVideoElement | null>(null)
const muted = ref(false)
const audioRefs = new Map<string, HTMLAudioElement>()

const project = computed(() => editor.project!)
const timeline = computed(() => project.value.timeline)

function contains(clip: Clip, time: number): boolean {
  return time >= clip.start - 0.0001 && time < clip.start + clip.duration - 0.0001
}

const activeVideo = computed(() => {
  for (const track of timeline.value.tracks) {
    if (track.kind !== 'video' || track.muted) continue
    for (const clip of track.clips) {
      if (contains(clip, editor.playhead)) {
        return { clip, asset: clip.asset_id ? editor.assetMap.get(clip.asset_id) : undefined }
      }
    }
  }
  return null
})

const activeAudio = computed(() => {
  const result: Array<{ clip: Clip; volume: number }> = []
  for (const track of timeline.value.tracks) {
    if (track.kind !== 'audio' || track.muted) continue
    for (const clip of track.clips) {
      if (contains(clip, editor.playhead)) result.push({ clip, volume: clip.volume })
    }
  }
  return result
})

const activeOverlays = computed(() => {
  const result: Array<{ clip: Clip; asset?: Asset }> = []
  for (const track of timeline.value.tracks) {
    if (track.kind !== 'overlay' || track.muted) continue
    for (const clip of track.clips) {
      if (contains(clip, editor.playhead)) {
        result.push({ clip, asset: clip.asset_id ? editor.assetMap.get(clip.asset_id) : undefined })
      }
    }
  }
  return result
})

const activeCaption = computed(() => {
  for (const track of timeline.value.tracks) {
    if (track.kind !== 'caption' || track.muted) continue
    for (const clip of track.clips) {
      if (contains(clip, editor.playhead)) return clip
    }
  }
  return null
})

const hasVideoClips = computed(() => timeline.value.tracks.some((t) => t.kind === 'video' && t.clips.length > 0))
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
  if (force || Math.abs(el.currentTime - expected) > 0.3) {
    try {
      el.currentTime = expected
    } catch {
      /* not seekable yet */
    }
  }
  if (editor.playing) {
    if (el.paused) el.play().catch(() => undefined)
  } else if (!el.paused) {
    el.pause()
  }
}

function syncAll(force = false) {
  const video = activeVideo.value
  if (video) syncElement(videoEl.value, video.clip, force)
  for (const item of activeAudio.value) {
    const el = audioRefs.get(item.clip.id) ?? null
    if (el) el.volume = muted.value ? 0 : Math.max(0, Math.min(1, item.volume))
    syncElement(el, item.clip, force)
  }
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
  () => activeVideo.value?.clip.id,
  () => nextTick(() => syncAll(true)),
)

watch(
  () => activeAudio.value.map((a) => a.clip.id).join(','),
  () => nextTick(() => syncAll(true)),
)
</script>

<template>
  <section class="preview-panel">
    <div ref="shell" class="preview-shell">
      <div class="stage" :style="stageStyle" @click="editor.togglePlay()">
        <video
          v-if="activeVideo?.asset"
          :key="`${activeVideo.clip.id}-${activeVideo.asset.id}`"
          ref="videoEl"
          class="stage-video"
          :src="assetUrl(project.id, activeVideo.asset.id)"
          muted
          playsinline
          @loadedmetadata="syncAll(true)"
          @canplay="syncAll(true)"
        />

        <div v-if="!hasVideoClips && hasAnyClip" class="audio-stage">
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

        <OverlayPreview
          :overlays="activeOverlays"
          :project="project"
          :project-id="project.id"
          :display-height="stageSize.height"
          :playing="editor.playing"
          :playhead="editor.playhead"
        />

        <CaptionPreview
          :style="timeline.caption_style"
          :clip="activeCaption"
          :display-height="stageSize.height"
          :playhead="editor.playhead"
        />

        <div class="stage-badge">{{ timecode(editor.playhead, project.fps) }}</div>
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
  background: #0c0c11;
}

.preview-shell {
  position: relative;
  flex: 1;
  min-height: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 17px;
  background: radial-gradient(circle at 50% 40%, #17171f, #0a0a0e 78%);
  overflow: hidden;
}

.stage {
  position: relative;
  background: #000;
  border-radius: 7px;
  overflow: hidden;
  box-shadow: 0 18px 60px rgba(0, 0, 0, 0.6), 0 0 0 1px rgba(255, 255, 255, 0.05);
  cursor: pointer;
}

.stage-video {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: contain;
  background: #000;
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
  background: radial-gradient(circle at 50% 42%, rgba(35, 213, 180, 0.12), transparent 60%);
}

.audio-orb {
  width: 58px;
  height: 58px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--teal);
  background: rgba(35, 213, 180, 0.1);
  border: 1px solid rgba(35, 213, 180, 0.25);
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
  border-radius: 5px;
  background: rgba(0, 0, 0, 0.6);
  color: rgba(255, 255, 255, 0.75);
  font-size: 10px;
  font-variant-numeric: tabular-nums;
  letter-spacing: 0.03em;
  backdrop-filter: blur(4px);
  pointer-events: none;
}

.transport {
  flex-shrink: 0;
  height: 52px;
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 0 14px;
  border-top: 1px solid var(--border-soft);
  background: #0b0b10;
}

.play-btn {
  width: 34px;
  height: 34px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  border: none;
  background: linear-gradient(135deg, #7c6cff, #5f8dff);
  color: #fff;
  box-shadow: 0 6px 18px rgba(124, 108, 255, 0.4);
  transition: transform 0.12s ease;
}

.play-btn:hover {
  transform: scale(1.05);
}

.play-btn:disabled {
  opacity: 0.4;
  box-shadow: none;
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
