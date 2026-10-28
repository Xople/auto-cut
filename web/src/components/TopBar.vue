<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ArrowLeft, Captions, Download, Redo2, Scissors, Undo2, Wand2 } from 'lucide-vue-next'
import { useEditor } from '../stores/editor'

const emit = defineEmits<{
  (e: 'back'): void
  (e: 'open-autocut'): void
  (e: 'open-export'): void
  (e: 'generate-captions'): void
}>()

const editor = useEditor()
const name = ref(editor.project?.name ?? '')

watch(
  () => editor.project?.name,
  (value) => (name.value = value ?? ''),
)

const saveLabel = computed(() => {
  switch (editor.saveState) {
    case 'saving':
      return 'Menyimpan...'
    case 'error':
      return 'Gagal menyimpan'
    default:
      return 'Tersimpan'
  }
})

const projectMeta = computed(() => {
  const p = editor.project
  if (!p) return ''
  return `${p.width}×${p.height} · ${Math.round(p.fps)} fps`
})

async function commitName() {
  const next = name.value.trim()
  if (!next || next === editor.project?.name) {
    name.value = editor.project?.name ?? ''
    return
  }
  try {
    await editor.renameProject(next)
  } catch (err) {
    editor.notify('Gagal mengganti nama: ' + (err as Error).message, 'error')
  }
}
</script>

<template>
  <header class="topbar">
    <button class="back-btn" title="Kembali ke daftar project" @click="emit('back')">
      <ArrowLeft :size="15" />
    </button>

    <div class="brand">
      <div class="brand-mark">
        <Scissors :size="15" />
      </div>
    </div>

    <div class="project-block">
      <input
        v-model="name"
        class="project-name"
        spellcheck="false"
        @blur="commitName"
        @keydown.enter="($event.target as HTMLInputElement).blur()"
      />
      <div class="project-sub">
        <span>{{ projectMeta }}</span>
        <span class="dot-sep">·</span>
        <span class="save-state" :class="editor.saveState">{{ saveLabel }}</span>
      </div>
    </div>

    <div class="top-actions">
      <button class="icon-btn" :disabled="!editor.canUndo" title="Undo (Ctrl+Z)" @click="editor.undo()">
        <Undo2 :size="16" />
      </button>
      <button class="icon-btn" :disabled="!editor.canRedo" title="Redo (Ctrl+Y)" @click="editor.redo()">
        <Redo2 :size="16" />
      </button>

      <div class="sep" />

      <button class="btn" title="Analisis voice-over & auto cut" @click="emit('open-autocut')">
        <Wand2 :size="15" />
        Auto Cut
      </button>
      <button class="btn" title="Generate caption dari transkrip" @click="emit('generate-captions')">
        <Captions :size="15" />
        Captions
      </button>
      <button class="btn btn-primary" @click="emit('open-export')">
        <Download :size="15" />
        Export
      </button>
    </div>
  </header>
</template>

<style scoped>
.topbar {
  height: 56px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 12px;
  background: #0b0b10;
  border-bottom: 1px solid var(--border-soft);
}

.back-btn {
  width: 30px;
  height: 30px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 8px;
  border: 1px solid var(--border-soft);
  background: transparent;
  color: var(--muted);
  transition: all 0.15s ease;
}

.back-btn:hover {
  color: var(--text);
  background: var(--panel-2);
}

.brand-mark {
  width: 30px;
  height: 30px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 9px;
  background: linear-gradient(135deg, #7c6cff, #4c7dff);
  color: #fff;
  box-shadow: 0 4px 16px rgba(124, 108, 255, 0.35);
}

.project-block {
  display: flex;
  flex-direction: column;
  justify-content: center;
  min-width: 200px;
  margin-left: 2px;
}

.project-name {
  border: 1px solid transparent;
  background: transparent;
  color: var(--text);
  font-size: 13px;
  font-weight: 650;
  padding: 1px 5px;
  margin-left: -5px;
  border-radius: 6px;
  outline: none;
  width: 260px;
  transition: background 0.15s ease, border-color 0.15s ease;
}

.project-name:hover {
  background: var(--panel-2);
}

.project-name:focus {
  background: #0a0a0f;
  border-color: var(--accent);
}

.project-sub {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 10.5px;
  color: var(--muted-2);
  padding-left: 0;
}

.dot-sep {
  opacity: 0.5;
}

.save-state {
  color: var(--green);
}

.save-state.saving {
  color: var(--amber);
}

.save-state.error {
  color: var(--danger);
}

.top-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 7px;
}

.sep {
  width: 1px;
  height: 22px;
  background: var(--border-soft);
  margin: 0 5px;
}
</style>
