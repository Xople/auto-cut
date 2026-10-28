<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { waveformUrl } from '../api'

const props = defineProps<{
  projectId: string
  assetId: string
  inSec: number
  outSec: number
}>()

interface WaveformData {
  peaks: number[]
  duration: number
}

const cache = new Map<string, Promise<WaveformData>>()

const data = ref<WaveformData | null>(null)
const failed = ref(false)

async function load() {
  if (!props.assetId) return
  failed.value = false
  const key = `${props.projectId}:${props.assetId}`
  let promise = cache.get(key)
  if (!promise) {
    promise = fetch(waveformUrl(props.projectId, props.assetId)).then((res) => {
      if (!res.ok) throw new Error('waveform unavailable')
      return res.json() as Promise<WaveformData>
    })
    cache.set(key, promise)
  }
  try {
    data.value = await promise
  } catch {
    cache.delete(key)
    data.value = null
    failed.value = true
  }
}

onMounted(load)
watch(() => props.assetId, load)

const path = computed(() => {
  const current = data.value
  if (!current || !current.peaks.length) return ''
  const total = current.peaks.length
  const duration = current.duration || total
  const startRatio = Math.max(0, Math.min(1, props.inSec / duration))
  const endRatio = Math.max(startRatio, Math.min(1, props.outSec / duration))
  let start = Math.floor(startRatio * total)
  let end = Math.ceil(endRatio * total)
  if (end - start < 2) end = Math.min(total, start + 2)

  const buckets = 160
  const step = (end - start) / buckets
  const tops: string[] = []
  const bottoms: string[] = []
  for (let i = 0; i < buckets; i++) {
    const from = Math.floor(start + i * step)
    const to = Math.max(from + 1, Math.floor(start + (i + 1) * step))
    let peak = 0
    for (let j = from; j < to && j < total; j++) peak = Math.max(peak, current.peaks[j])
    const height = Math.max(0.02, Math.min(1, peak)) * 44
    const x = (i / buckets) * 1000
    tops.push(`${x.toFixed(1)},${(50 - height).toFixed(1)}`)
    bottoms.unshift(`${x.toFixed(1)},${(50 + height).toFixed(1)}`)
  }
  return `M ${tops.join(' L ')} L ${bottoms.join(' L ')} Z`
})
</script>

<template>
  <svg
    v-if="path"
    class="waveform"
    viewBox="0 0 1000 100"
    preserveAspectRatio="none"
    aria-hidden="true"
  >
    <path :d="path" />
  </svg>
  <div v-else-if="failed" class="waveform-fallback" />
  <div v-else class="waveform-loading" />
</template>

<style scoped>
.waveform {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  color: currentColor;
  pointer-events: none;
}

.waveform path {
  fill: currentColor;
  opacity: 0.55;
}

.waveform-fallback,
.waveform-loading {
  position: absolute;
  left: 0;
  right: 0;
  top: 50%;
  height: 1px;
  background: currentColor;
  opacity: 0.25;
  pointer-events: none;
}
</style>
