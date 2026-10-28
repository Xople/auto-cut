<script setup lang="ts">
import { useEditor } from '../stores/editor'

const editor = useEditor()
</script>

<template>
  <Teleport to="body">
    <Transition name="toast">
      <div v-if="editor.toast" :key="editor.toast.id" class="toast-host" :class="editor.toast.kind">
        <span class="toast-dot" />
        <span class="toast-text">{{ editor.toast.message }}</span>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.toast-host {
  position: fixed;
  left: 50%;
  bottom: 26px;
  z-index: 120;
  transform: translateX(-50%);
  display: flex;
  align-items: center;
  gap: 9px;
  max-width: min(560px, 86vw);
  padding: 9px 14px;
  border-radius: 10px;
  border: 1px solid var(--border);
  background: rgba(22, 22, 29, 0.96);
  box-shadow: 0 12px 34px rgba(0, 0, 0, 0.5);
  backdrop-filter: blur(8px);
  font-size: 12px;
  color: var(--text);
}

.toast-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--accent-strong);
  flex-shrink: 0;
}

.toast-host.success .toast-dot {
  background: var(--green);
}

.toast-host.error .toast-dot {
  background: var(--danger);
}

.toast-host.info .toast-dot {
  background: var(--amber);
}

.toast-text {
  line-height: 1.45;
}

.toast-enter-active,
.toast-leave-active {
  transition: opacity 0.18s ease, transform 0.18s ease;
}

.toast-enter-from,
.toast-leave-to {
  opacity: 0;
  transform: translateX(-50%) translateY(8px);
}
</style>
