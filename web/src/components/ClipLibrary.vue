<script setup lang="ts">
import { computed, ref } from 'vue'
import {
  ArrowDown, ArrowLeft, ArrowRight, ArrowUp, Ban, Blend, EyeClosed, Maximize2,
  Minimize2, RotateCw, Shuffle, Zap,
} from 'lucide-vue-next'
import { useEditor } from '../stores/editor'
import { thumbUrl } from '../api'
import { TRANSITIONS, VIDEO_EFFECTS, videoEffectFilter, type TransitionType, type VideoEffect } from '../types'

const props = defineProps<{ tool: 'transitions' | 'effects' }>()
const editor = useEditor()
const scope = ref<'clip' | 'all'>('clip')
const video = computed(() => editor.selected?.track.kind === 'video' ? editor.selected : null)
const allScenes = computed(() => props.tool === 'transitions' && scope.value === 'all')
const enabled = computed(() => allScenes.value
  ? editor.project?.timeline.tracks.some(t => t.kind === 'video' && !t.locked && t.clips.some(c => c.asset_id && c.start >= 0.05))
  : !!video.value && !video.value.track.locked)
const thumbnail = computed(() => video.value?.clip.asset_id && editor.project
  ? thumbUrl(editor.project.id, video.value.clip.asset_id) : undefined)
const icons = {
  pull_in: Maximize2, pull_out: Minimize2, blink: EyeClosed, flash: Zap, fade: Blend,
  slide_left: ArrowLeft, slide_right: ArrowRight, slide_up: ArrowUp, slide_down: ArrowDown, spin_in: RotateCw,
}

function applyTransition(type: TransitionType | 'none') {
  if (!enabled.value) return
  if (allScenes.value) editor.applyTransitionToAllScenes(type, video.value?.clip.transition?.duration)
  else if (video.value) editor.setClipTransition(type, undefined, video.value.clip.id)
}

function applyEffect(effect?: VideoEffect) {
  if (!video.value || !enabled.value) return
  editor.updateClip(video.value.track.id, video.value.clip.id, { video_effect: effect, effect_intensity: effect ? 1 : undefined })
}
</script>

<template>
  <div class="library">
    <h3>{{ tool === 'transitions' ? 'Transisi masuk' : 'Efek warna' }}</h3>
    <div v-if="tool === 'transitions'" class="scope" aria-label="Target transisi">
      <button :aria-pressed="scope === 'clip'" @click="scope = 'clip'">Clip terpilih</button>
      <button :aria-pressed="scope === 'all'" @click="scope = 'all'">Semua scene</button>
    </div>
    <p v-if="!enabled" class="hint">{{ allScenes ? 'Tambahkan scene berikutnya di track video untuk memasang transisi.' : video?.track.locked ? 'Buka kunci track untuk mengedit clip.' : 'Pilih clip video di timeline.' }}</p>
    <div v-if="tool === 'transitions'" class="preset-grid">
      <button class="tile" :disabled="!enabled" :aria-pressed="!!video && !allScenes && !video.clip.transition" @click="applyTransition('none')">
        <Ban />Tanpa
      </button>
      <button v-if="allScenes" class="tile" :disabled="!enabled" @click="editor.applyTransitionToAllScenes('variety', video?.clip.transition?.duration)">
        <Shuffle />Auto variasi
      </button>
      <button v-for="item in TRANSITIONS" :key="item.value" class="tile" :disabled="!enabled"
        :title="item.hint" :aria-pressed="!allScenes && video?.clip.transition?.type === item.value"
        @click="applyTransition(item.value)">
        <component :is="icons[item.value]" />{{ item.label }}
      </button>
    </div>
    <div v-else class="preset-grid effects">
      <button v-for="item in [{ value: undefined, label: 'Asli' }, ...VIDEO_EFFECTS]" :key="item.label"
        class="tile" :disabled="!enabled" :aria-pressed="!!video && (video.clip.video_effect || undefined) === item.value"
        @click="applyEffect(item.value)">
        <span class="sample" :style="{ filter: videoEffectFilter(item.value) }">
          <img v-if="thumbnail" :src="thumbnail" alt="" />
        </span>
        {{ item.label }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.library h3 { font-size: 14px; margin: 0 0 12px; }
.scope { display: flex; padding: 3px; margin-bottom: 12px; background: var(--bg); border-radius: 6px; }
.scope button { flex: 1; padding: 6px; color: var(--muted); font-size: 12px; background: transparent; border: 0; border-radius: 4px; }
.scope button[aria-pressed="true"] { background: var(--panel-3); color: var(--text); }
.hint { color: var(--muted); line-height: 1.5; margin: 0 0 12px; }
.preset-grid { display: grid; grid-template-columns: repeat(3,minmax(0,1fr)); gap: 7px; }
.effects { grid-template-columns: repeat(2,minmax(0,1fr)); }
.tile { display: grid; justify-items: center; align-content: center; gap: 8px; min-height: 65px; padding: 8px 4px; font-size: 11px; font-weight: 550; background: var(--panel-2); border: 1px solid var(--border); border-radius: 6px; }
.tile svg { width: 20px; height: 20px; color: var(--muted); }
.tile:disabled { opacity: .55; cursor: default; }
.tile:hover:enabled { background: var(--panel-3); }
.tile[aria-pressed="true"] { border-color: var(--accent); }
.tile[aria-pressed="true"] svg { color: var(--accent); }
.effects .tile { overflow: hidden; padding: 0 0 8px; }
.sample { width: 100%; aspect-ratio: 2; overflow: hidden; background: linear-gradient(120deg, #bb925d 0 32%, #d7cbc0 32% 54%, #618c7d 54% 78%, #425466 78%); }
.sample img { display: block; width: 100%; height: 100%; object-fit: cover; }
</style>
