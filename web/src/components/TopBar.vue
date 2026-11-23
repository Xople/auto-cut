<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { ArrowLeft, Captions, Check, ChevronDown, Download, LayoutPanelTop, PanelLeft, PanelRight, RotateCcw, Redo2, Scissors, Undo2, Wand2 } from 'lucide-vue-next'
import { useEditor } from '../stores/editor'

import { layouts, type Layout } from '../lib/workspaceLayout'

const props = defineProps<{ layout: Layout }>()
const emit = defineEmits<{
  (e: 'layout', layout: Layout): void
  (e: 'reset-layout'): void
  (e: 'back'): void
  (e: 'open-autocut'): void
  (e: 'open-export'): void
  (e: 'generate-captions'): void
}>()

const layoutMenu = ref<HTMLElement | null>(null)
const layoutButton = ref<HTMLButtonElement | null>(null)
const menuOpen = ref(false)

function closeMenu(restoreFocus = false) {
  menuOpen.value = false
  if (restoreFocus) layoutButton.value?.focus()
}
function onOutside(event: PointerEvent) {
  if (!layoutMenu.value?.contains(event.target as Node)) closeMenu()
}
watch(menuOpen, open => {
  if (open) document.addEventListener('pointerdown', onOutside)
  else document.removeEventListener('pointerdown', onOutside)
})
onBeforeUnmount(() => document.removeEventListener('pointerdown', onOutside))

async function openMenu() {
  menuOpen.value = true
  await nextTick()
  layoutMenu.value?.querySelector<HTMLButtonElement>('[aria-pressed="true"]')?.focus()
}

function selectLayout(value: Layout) {
  emit('layout', value)
  closeMenu(true)
}

function menuKeys(event: KeyboardEvent) {
  if (!menuOpen.value) return
  if (event.key === 'Escape') {
    event.preventDefault()
    event.stopPropagation()
    closeMenu(true)
    return
  }
  const buttons = Array.from(layoutMenu.value?.querySelectorAll<HTMLButtonElement>('.layout-list button') ?? [])
  if (!buttons.length || !['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  event.stopPropagation()
  const i = buttons.indexOf(document.activeElement as HTMLButtonElement)
  const next = event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1
    : (i + (event.key === 'ArrowUp' ? -1 : 1) + buttons.length) % buttons.length
  buttons[next].focus()
}

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
        <button
          type="button"
          class="save-state"
          :class="editor.saveState"
          title="Simpan sekarang (Ctrl+S)"
          @click="editor.saveNow()"
        >
          {{ saveLabel }}
        </button>
      </div>
    </div>

    <div class="top-actions">
      <div ref="layoutMenu" class="layout-menu" @keydown="menuKeys"
        @focusout="!layoutMenu?.contains($event.relatedTarget as Node | null) && closeMenu()">
        <button ref="layoutButton" class="btn layout-trigger" title="Pilih tata letak editor"
          :aria-expanded="menuOpen" aria-controls="layout-list"
          @click="menuOpen ? closeMenu() : openMenu()" @keydown.down.prevent="openMenu">
          <LayoutPanelTop :size="15" /> Tata letak <ChevronDown :size="12" />
        </button>
        <div v-if="menuOpen" id="layout-list" class="layout-list" role="group" aria-label="Tata letak editor">
          <button v-for="option in layouts" :key="option.id" :aria-pressed="props.layout === option.id"
            @click="selectLayout(option.id)">
            <LayoutPanelTop v-if="option.id === 'default'" :size="17" />
            <PanelLeft v-else-if="option.id === 'media' || option.id === 'vertical-left'" :size="17" />
            <PanelRight v-else :size="17" />
            <span><strong>{{ option.label }}</strong><small>{{ option.detail }}</small></span>
            <Check v-if="props.layout === option.id" class="layout-check" :size="15" />
          </button>
          <button class="layout-reset" @click="emit('reset-layout'); closeMenu(true)">
            <RotateCcw :size="14" /> Reset ukuran tata letak ini
          </button>
        </div>
      </div>
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
  background: var(--bg);
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
  transition: color 0.15s ease, background 0.15s ease, border-color 0.15s ease;
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
  border-radius: 8px;
  background: var(--panel-3);
  color: var(--text);
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
  background: #0d0d0d;
  border-color: #6b6b6b;
}

.project-sub {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  color: var(--muted-2);
  padding-left: 0;
}

.dot-sep {
  opacity: 0.5;
}

.save-state {
  padding: 0;
  border: 0;
  background: transparent;
  font: inherit;
  color: var(--muted-2);
  cursor: pointer;
}

.save-state:hover {
  color: var(--text);
  text-decoration: underline;
}

.save-state.saving {
  color: var(--amber);
}

.save-state.error {
  color: var(--danger);
}

.layout-menu { position: relative; }
.layout-list {
  position: absolute;
  top: calc(100% + 6px);
  right: 0;
  z-index: 50;
  width: 224px;
  padding: 5px;
  background: var(--panel-2);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  box-shadow: 0 8px 24px rgb(0 0 0 / 35%);
}
.layout-list button {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 7px 9px;
  border: 0;
  border-radius: 4px;
  background: transparent;
  color: var(--muted);
  text-align: left;
}
.layout-list button:hover, .layout-list button:focus-visible { background: var(--panel-3); }
.layout-list button span { flex: 1; }
.layout-list strong, .layout-list small { display: block; }
.layout-list strong { color: var(--text); font-size: 12px; font-weight: 550; }
.layout-list small { margin-top: 2px; font-size: 11px; }
.layout-check { color: var(--accent); }
.layout-list .layout-reset { border-top: 1px solid var(--border); margin-top: 4px; padding-top: 10px; font-size: 11px; }

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
