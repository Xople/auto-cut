<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, useId, type Component } from 'vue'
import { X } from 'lucide-vue-next'

defineProps<{ heading: string; icon: Component; detail?: string; danger?: boolean }>()
const emit = defineEmits<{ dismiss: [] }>()
const dialog = ref<HTMLDialogElement>()
const headingId = useId()
let backdropPress = false

function outside(event: MouseEvent) {
  if (event.target !== dialog.value || !dialog.value) return false
  const rect = dialog.value.getBoundingClientRect()
  return event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom
}

onMounted(() => dialog.value?.showModal())
onBeforeUnmount(() => dialog.value?.close())
</script>

<template>
  <dialog ref="dialog" class="modal" :class="{ destructive: danger }" :aria-labelledby="headingId"
    @cancel.prevent="emit('dismiss')" @pointerdown="backdropPress = outside($event)"
    @click="backdropPress && outside($event) && emit('dismiss')">
    <header class="modal-header">
      <span class="modal-symbol"><component :is="icon" :size="20" /></span>
      <h2 :id="headingId">{{ heading }}</h2>
      <span v-if="detail" class="modal-detail">{{ detail }}</span>
      <button class="icon-btn modal-close" type="button" aria-label="Tutup dialog" data-tooltip @click="emit('dismiss')"><X :size="17" /></button>
    </header>
    <div class="modal-body"><slot /></div>
    <footer class="modal-footer"><slot name="footer" /></footer>
  </dialog>
</template>
