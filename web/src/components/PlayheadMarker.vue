<script setup lang="ts">
import { computed } from 'vue'
import { useEditor } from '../stores/editor'

const props = defineProps<{ pps: number; variant: 'cap' | 'line' }>()
defineEmits<{ scrub: [event: PointerEvent] }>()

const editor = useEditor()
const left = computed(() => `${(editor.playhead * props.pps).toFixed(2)}px`)
</script>

<template>
  <div
    :class="variant === 'cap' ? 'playhead-cap' : 'playhead'"
    :style="{ left }"
    @pointerdown.stop="$emit('scrub', $event)"
  />
</template>

<style scoped>
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

.playhead {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 2px;
  margin-left: -0.25px;
  background: var(--accent);
  pointer-events: auto;
  cursor: ew-resize;
  z-index: 7;
}

.playhead::before {
  content: '';
  position: absolute;
  top: 0;
  bottom: 0;
  left: -5px;
  right: -5px;
}
</style>
