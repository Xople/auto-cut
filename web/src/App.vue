<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { api, initApi, waitForServer } from './api'
import { useEditor } from './stores/editor'
import ProjectLauncher from './components/ProjectLauncher.vue'
import TopBar from './components/TopBar.vue'
import MediaPanel from './components/MediaPanel.vue'
import PreviewStage from './components/PreviewStage.vue'
import PropertiesPanel from './components/PropertiesPanel.vue'
import TimelinePanel from './components/TimelinePanel.vue'
import AutoCutDialog from './components/AutoCutDialog.vue'
import ExportDialog from './components/ExportDialog.vue'
import ToastHost from './components/ToastHost.vue'
import type { Project } from './types'

const editor = useEditor()
const booting = ref(true)
const bootError = ref('')
const autoCutOpen = ref(false)
const exportOpen = ref(false)
const opening = ref(false)

async function boot() {
  booting.value = true
  bootError.value = ''
  await initApi()
  try {
    await waitForServer()
  } catch (err) {
    bootError.value =
      'Backend AutoCut tidak merespons. Pastikan sidecar berjalan (atau jalankan `go run ./cmd/server` untuk mode browser). ' +
      (err as Error).message
  } finally {
    booting.value = false
  }
}

async function openProject(project: Project) {
  opening.value = true
  try {
    // The launcher list can predate a pending autosave, so always load fresh.
    const fresh = await api.getProject(project.id)
    await editor.loadProject(fresh)
  } catch (err) {
    editor.notify('Gagal membuka project: ' + (err as Error).message, 'error')
  } finally {
    opening.value = false
  }
}

function onPageHide() {
  editor.flushKeepalive()
}

async function generateCaptions() {
  if (!editor.project) return
  try {
    const res = await api.generateCaptions(editor.project.id)
    editor.replaceProject(res.project)
    editor.notify(`${res.caption_count} caption digenerate dari transkrip.`, 'success')
  } catch (err) {
    const message = (err as Error).message
    if (message.toLowerCase().includes('transkrip')) {
      editor.notify('Belum ada transkrip. Jalankan Auto Cut / analisis voice-over dulu.', 'info')
      autoCutOpen.value = true
    } else {
      editor.notify(message, 'error')
    }
  }
}

function onGlobalKeydown(event: KeyboardEvent) {
  if (!editor.project) return
  const target = event.target as HTMLElement | null
  const tag = target?.tagName?.toLowerCase()
  if (tag === 'input' || tag === 'textarea' || tag === 'select' || target?.isContentEditable) return

  const mod = event.ctrlKey || event.metaKey
  if (mod && event.key.toLowerCase() === 'z') {
    event.preventDefault()
    event.shiftKey ? editor.redo() : editor.undo()
    return
  }
  if (mod && event.key.toLowerCase() === 'y') {
    event.preventDefault()
    editor.redo()
    return
  }
  if (mod && event.key.toLowerCase() === 'b') {
    event.preventDefault()
    editor.splitAtPlayhead()
    return
  }
  if (mod && event.key.toLowerCase() === 's') {
    event.preventDefault()
    void editor.saveNow()
    return
  }
  if (mod && event.key.toLowerCase() === 'c') {
    event.preventDefault()
    editor.copySelected()
    return
  }
  if (mod && event.key.toLowerCase() === 'x') {
    event.preventDefault()
    editor.cutSelected()
    return
  }
  if (mod && event.key.toLowerCase() === 'v') {
    event.preventDefault()
    editor.pasteAtPlayhead()
    return
  }
  if (mod && event.key.toLowerCase() === 'a') {
    event.preventDefault()
    editor.selectAll()
    return
  }
  if (event.key === 'Escape') {
    editor.clearSelection()
    return
  }
  if (event.code === 'Space') {
    event.preventDefault()
    editor.togglePlay()
    return
  }
  if (event.key === 'Delete' || event.key === 'Backspace') {
    event.preventDefault()
    if (event.shiftKey) editor.deleteSelected(true)
    else editor.deleteSelected()
    return
  }
  const timelineFocused = Boolean(
    (target instanceof Element && target.closest('.timeline-panel')) ||
      document.activeElement?.closest?.('.timeline-panel'),
  )
  if (event.key === 'ArrowRight' || event.key === 'ArrowLeft') {
    event.preventDefault()
    const direction = event.key === 'ArrowRight' ? 1 : -1
    if (timelineFocused) {
      editor.nudgeSelected(direction * (event.shiftKey ? 1 : 1 / (editor.project.fps || 30)))
    } else {
      editor.seek(editor.playhead + (direction * 1) / (editor.project.fps || 30))
    }
  }
}

onMounted(() => {
  boot()
  window.addEventListener('keydown', onGlobalKeydown)
  window.addEventListener('pagehide', onPageHide)
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onGlobalKeydown)
  window.removeEventListener('pagehide', onPageHide)
})
</script>

<template>
  <div class="app-shell">
    <div v-if="booting" class="boot-screen">
      <div class="boot-mark">
        <span class="boot-dot" />
        <span class="boot-dot" />
        <span class="boot-dot" />
      </div>
      <h1>AutoCut Workstation</h1>
      <p>Menyalakan engine lokal (ffmpeg + whisper)...</p>
    </div>

    <div v-else-if="bootError" class="boot-screen">
      <div class="boot-error-mark">!</div>
      <h1>Engine belum siap</h1>
      <p class="boot-error-text">{{ bootError }}</p>
      <button class="btn btn-primary" @click="boot">Coba lagi</button>
    </div>

    <template v-else>
      <TopBar
        v-if="editor.project"
        @back="editor.closeProject()"
        @open-autocut="autoCutOpen = true"
        @open-export="exportOpen = true"
        @generate-captions="generateCaptions"
      />

      <div v-if="editor.project" class="workspace">
        <MediaPanel class="area-media" />
        <PreviewStage class="area-preview" />
        <PropertiesPanel
          class="area-props"
          @open-autocut="autoCutOpen = true"
          @generate-captions="generateCaptions"
        />
        <TimelinePanel class="area-timeline" />
      </div>

      <ProjectLauncher v-else :busy="opening" @open="openProject" />

      <AutoCutDialog v-model="autoCutOpen" />
      <ExportDialog v-model="exportOpen" />
      <ToastHost />

      <div v-if="opening" class="opening-overlay">
        <div class="spinner" />
        <span>Membuka project...</span>
      </div>
    </template>
  </div>
</template>

<style scoped>
.app-shell {
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--bg);
}

.workspace {
  flex: 1;
  min-height: 0;
  display: grid;
  grid-template-columns: 268px minmax(0, 1fr) 306px;
  grid-template-rows: minmax(0, 1fr) 286px;
  gap: 1px;
  background: var(--border-soft);
}

.area-media {
  grid-column: 1;
  grid-row: 1;
}

.area-preview {
  grid-column: 2;
  grid-row: 1;
}

.area-props {
  grid-column: 3;
  grid-row: 1;
}

.area-timeline {
  grid-column: 1 / -1;
  grid-row: 2;
}

.boot-screen {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  text-align: center;
  padding: 40px;
  background: var(--bg);
}

.boot-screen h1 {
  margin: 0;
  font-size: 22px;
  font-weight: 700;
  letter-spacing: -0.01em;
}

.boot-screen p {
  margin: 0;
  max-width: 520px;
  color: var(--muted);
  line-height: 1.6;
}

.boot-mark {
  display: flex;
  gap: 7px;
  margin-bottom: 8px;
}

.boot-dot {
  width: 11px;
  height: 11px;
  border-radius: 50%;
  background: var(--accent);
  animation: boot-pulse 1.1s ease-in-out infinite;
}

.boot-dot:nth-child(2) {
  animation-delay: 0.15s;
}

.boot-dot:nth-child(3) {
  animation-delay: 0.3s;
}

.boot-error-mark {
  width: 46px;
  height: 46px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 24px;
  font-weight: 700;
  color: var(--amber);
  background: rgba(229, 169, 59, 0.1);
  border: 1px solid rgba(229, 169, 59, 0.3);
}

.boot-error-text {
  color: var(--muted) !important;
  font-size: 12.5px;
  max-width: 620px !important;
}

.opening-overlay {
  position: fixed;
  inset: 0;
  z-index: 70;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  background: rgba(0, 0, 0, 0.72);
  color: var(--muted);
}

.spinner {
  width: 17px;
  height: 17px;
  border-radius: 50%;
  border: 2px solid var(--panel-3);
  border-top-color: var(--text);
  animation: spin 0.7s linear infinite;
}

@keyframes boot-pulse {
  0%,
  100% {
    transform: scale(0.8);
    opacity: 0.5;
  }
  50% {
    transform: scale(1.1);
    opacity: 1;
  }
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
