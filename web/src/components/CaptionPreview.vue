<script setup lang="ts">
import { computed } from 'vue'
import { blendCSS, type CaptionStyle, type Clip } from '../types'

const props = defineProps<{
  style: CaptionStyle
  clip: Clip | null
  displayHeight: number
  playhead: number
}>()

const activeWord = computed(() => {
  const clip = props.clip
  if (!clip?.words?.length) return -1
  let active = -1
  for (let i = 0; i < clip.words.length; i++) {
    const word = clip.words[i]
    if (props.playhead >= word.start && props.playhead < word.end) return i
    if (props.playhead >= word.end) active = i
  }
  return active
})

function joined(words: Array<{ word: string }>): string {
  let text = ''
  for (const item of words) {
    const token = item.word
    if (text && !/^[,.;:!?%)\]]/.test(token)) text += ' '
    text += token
  }
  return text
}

const text = computed(() => {
  const clip = props.clip
  if (!clip) return ''
  if (clip.words?.length) return joined(clip.words)
  return clip.text ?? ''
})

const fontSize = computed(() => Math.max(8, props.style.font_size * (props.displayHeight / 1080)))
const strokeWidth = computed(() => Math.max(0, props.style.stroke_width * (props.displayHeight / 1080)))
const shadow = computed(() => Math.max(0, props.style.shadow * (props.displayHeight / 1080)))

function hexToRgba(hex: string, alpha: number): string {
  const clean = (hex || '#000000').replace('#', '')
  const value = clean.length === 3
    ? clean.split('').map((c) => c + c).join('')
    : clean.padEnd(6, '0').slice(0, 6)
  const num = parseInt(value, 16)
  const r = (num >> 16) & 255
  const g = (num >> 8) & 255
  const b = num & 255
  return `rgba(${r}, ${g}, ${b}, ${Math.max(0, Math.min(1, alpha))})`
}

const containerStyle = computed(() => {
  const bgAlpha = props.style.background_alpha ?? 0
  return {
    top: `${props.style.position_y * 100}%`,
    opacity: props.style.opacity,
    mixBlendMode: blendCSS(props.style.blend_mode) as any,
    fontFamily: `"${props.style.font_family}", Arial, sans-serif`,
    fontSize: `${fontSize.value}px`,
    fontWeight: props.style.bold ? '700' : '400',
    fontStyle: props.style.italic ? 'italic' : 'normal',
    color: props.style.color,
    WebkitTextStroke: strokeWidth.value > 0.1 && bgAlpha <= 0 ? `${strokeWidth.value}px ${props.style.stroke_color}` : '0px transparent',
    textShadow: shadow.value > 0.1 && bgAlpha <= 0 ? `0 ${shadow.value}px ${shadow.value * 1.7}px ${props.style.shadow_color}` : 'none',
    textTransform: (props.style.uppercase ? 'uppercase' : 'none') as 'uppercase' | 'none',
    backgroundColor: bgAlpha > 0 ? hexToRgba(props.style.background_color ?? '#000000', bgAlpha) : 'transparent',
    padding: bgAlpha > 0 ? `${Math.round(fontSize.value * 0.18)}px ${Math.round(fontSize.value * 0.38)}px` : '0',
    borderRadius: bgAlpha > 0 ? `${Math.round(fontSize.value * 0.16)}px` : '0',
    width: bgAlpha > 0 ? 'auto' : '90%',
    maxWidth: '90%',
  }
})

const words = computed(() => {
  const clip = props.clip
  if (!clip?.words?.length) return []
  return props.style.highlight ? clip.words : []
})
</script>

<template>
  <div v-if="clip" class="caption-layer">
    <div class="caption-line" :style="containerStyle">
      <template v-if="words.length">
        <span
          v-for="(word, index) in words"
          :key="index"
          class="word"
          :class="{ active: index === activeWord }"
          :style="index === activeWord ? { color: style.highlight_color, transform: 'scale(1.06)' } : undefined"
        >{{ word.word }}</span>
      </template>
      <template v-else>{{ text }}</template>
    </div>
  </div>
</template>

<style scoped>
.caption-layer {
  position: absolute;
  inset: 0;
  pointer-events: none;
  overflow: hidden;
}

.caption-line {
  position: absolute;
  left: 50%;
  transform: translate(-50%, -50%);
  width: 90%;
  text-align: center;
  line-height: 1.2;
  letter-spacing: 0.01em;
  white-space: pre-wrap;
}

.word {
  display: inline-block;
  margin: 0 0.07em;
  transition: color 0.06s linear, transform 0.08s ease;
}
</style>
