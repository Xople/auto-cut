<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Clock, Film, FolderOpen, Plus, Scissors, Trash2, X } from 'lucide-vue-next'
import { api } from '../api'
import { formatSecondsShort } from '../lib/format'
import type { Project } from '../types'

defineProps<{ busy: boolean }>()
const emit = defineEmits<{ (e: 'open', project: Project): void }>()

const projects = ref<Project[]>([])
const loading = ref(true)
const error = ref('')
const showCreate = ref(false)
const creating = ref(false)
const newName = ref('')
const preset = ref('1920x1080')
const fps = ref(30)

const presets = [
  { value: '1920x1080', label: 'YouTube 1080p · 16:9' },
  { value: '2560x1440', label: 'YouTube 1440p · 16:9' },
  { value: '1080x1920', label: 'Shorts / Reels · 9:16' },
  { value: '1080x1080', label: 'Square · 1:1' },
]

const size = computed(() => {
  const [w, h] = preset.value.split('x').map(Number)
  return { width: w, height: h }
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    projects.value = await api.listProjects()
  } catch (err) {
    error.value = (err as Error).message
  } finally {
    loading.value = false
  }
}

async function create() {
  if (creating.value) return
  creating.value = true
  try {
    const project = await api.createProject(
      newName.value.trim() || 'Untitled Project',
      size.value.width,
      size.value.height,
      fps.value,
    )
    showCreate.value = false
    emit('open', project)
  } catch (err) {
    error.value = (err as Error).message
  } finally {
    creating.value = false
  }
}

async function remove(project: Project, event: MouseEvent) {
  event.stopPropagation()
  if (!confirm(`Hapus project "${project.name}" beserta semua media di dalamnya?`)) return
  try {
    await api.deleteProject(project.id)
    projects.value = projects.value.filter((p) => p.id !== project.id)
  } catch (err) {
    error.value = (err as Error).message
  }
}

function duration(project: Project): number {
  let max = 0
  for (const track of project.timeline.tracks) {
    for (const clip of track.clips) max = Math.max(max, clip.start + clip.duration)
  }
  return max
}

function updated(project: Project): string {
  const date = new Date(project.updated_at)
  return date.toLocaleDateString('id-ID', { day: 'numeric', month: 'short', year: 'numeric' })
}

onMounted(load)
</script>

<template>
  <main class="launcher">
    <div class="launcher-head">
      <div class="brand-row">
        <div class="brand-mark"><Scissors :size="20" /></div>
        <div>
          <h1>AutoCut Workstation</h1>
          <p>Editor timeline lokal untuk produksi video essay TxShD.</p>
        </div>
      </div>
      <button class="btn btn-primary" @click="showCreate = true">
        <Plus :size="15" />
        Project Baru
      </button>
    </div>

    <p v-if="error" class="error-text launch-error">{{ error }}</p>

    <div v-if="loading" class="launch-empty">
      <div class="load-dots"><span /><span /><span /></div>
      <p>Memuat project...</p>
    </div>

    <div v-else-if="projects.length === 0" class="launch-empty">
      <div class="empty-icon"><FolderOpen :size="26" /></div>
      <h2>Belum ada project</h2>
      <p>Mulai dengan membuat project baru, lalu impor video, voice-over, musik, atau gambar.</p>
      <button class="btn btn-primary" @click="showCreate = true">
        <Plus :size="15" />
        Buat project pertama
      </button>
    </div>

    <div v-else class="project-grid">
      <article
        v-for="(project, index) in projects"
        :key="project.id"
        class="project-card"
        :style="{ animationDelay: `${Math.min(index, 8) * 35}ms` }"
        @click="emit('open', project)"
      >
        <div class="card-art" :class="{ vertical: project.height > project.width }">
          <Film :size="26" />
          <span class="card-res">{{ project.width }}×{{ project.height }}</span>
          <span v-if="project.last_export?.status === 'completed'" class="badge teal card-export">{{
            project.last_export.output_name
          }}</span>
        </div>
        <div class="card-body">
          <div class="card-title-row">
            <h3>{{ project.name }}</h3>
            <button class="icon-btn card-delete" title="Hapus project" @click="remove(project, $event)">
              <Trash2 :size="14" />
            </button>
          </div>
          <div class="card-meta">
            <span>{{ Math.round(project.fps) }} fps</span>
            <span class="dot">·</span>
            <span><Clock :size="11" /> {{ formatSecondsShort(duration(project)) }}</span>
            <span class="dot">·</span>
            <span>{{ updated(project) }}</span>
          </div>
        </div>
      </article>

      <button class="new-card" @click="showCreate = true">
        <span class="new-card-plus"><Plus :size="20" /></span>
        <span>Project baru</span>
        <span class="new-card-hint">Mulai dari timeline kosong</span>
      </button>
    </div>

    <footer class="launcher-foot">
      <span>Engine: Go + ffmpeg + whisper.cpp · UI: Tauri + Vue 3</span>
      <span v-if="busy">Membuka project...</span>
    </footer>

    <div v-if="showCreate" class="modal-backdrop" @click.self="showCreate = false">
      <div class="modal">
        <div class="modal-header">
          <div class="modal-title">Project baru</div>
          <button class="icon-btn" title="Tutup" @click="showCreate = false"><X :size="15" /></button>
        </div>
        <div class="modal-body">
          <div class="field">
            <label class="field-label">Nama project</label>
            <input
              v-model="newName"
              class="input"
              placeholder="Contoh: Analisis Elden Ring — Lore"
              autofocus
              @keydown.enter="create"
            />
          </div>
          <div class="field">
            <label class="field-label">Resolusi</label>
            <select v-model="preset" class="select">
              <option v-for="item in presets" :key="item.value" :value="item.value">{{ item.label }}</option>
            </select>
          </div>
          <div class="field">
            <label class="field-label">Frame rate</label>
            <div class="fps-row">
              <button
                v-for="value in [24, 30, 60]"
                :key="value"
                class="fps-chip"
                :class="{ active: fps === value }"
                @click="fps = value"
              >
                {{ value }} fps
              </button>
            </div>
          </div>
          <p class="hint">
            Timeline dibuat dengan track Video, Overlay, Audio, dan Caption. Semua media disalin ke folder project
            supaya aman walau file asli dipindah.
          </p>
        </div>
        <div class="modal-footer">
          <button class="btn btn-ghost" @click="showCreate = false">Batal</button>
          <button class="btn btn-primary" :disabled="creating" @click="create">
            {{ creating ? 'Membuat...' : 'Buat project' }}
          </button>
        </div>
      </div>
    </div>
  </main>
</template>

<style scoped>
.launcher {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 34px 40px 20px;
  background: var(--bg);
}

.launcher-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
  max-width: 1180px;
  margin: 0 auto 26px;
}

.brand-row {
  display: flex;
  gap: 14px;
  align-items: center;
}

.brand-mark {
  width: 44px;
  height: 44px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--panel-3);
  color: var(--text);
}

.launcher-head h1 {
  margin: 0;
  font-size: 21px;
  font-weight: 700;
  letter-spacing: -0.02em;
}

.launcher-head p {
  margin: 3px 0 0;
  color: var(--muted);
  font-size: 12.5px;
}

.launch-error {
  max-width: 1180px;
  margin: 0 auto 16px;
}

.launch-empty {
  max-width: 480px;
  margin: 80px auto 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  text-align: center;
  color: var(--muted);
}

.launch-empty h2 {
  margin: 6px 0 0;
  color: var(--text);
  font-size: 17px;
}

.launch-empty p {
  margin: 0 0 8px;
  line-height: 1.6;
  font-size: 12.5px;
}

.empty-icon {
  width: 58px;
  height: 58px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--muted);
  background: var(--panel-2);
  border: 1px solid var(--border);
}

.load-dots {
  display: flex;
  gap: 6px;
}

.load-dots span {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--muted);
  animation: pulse 1s ease-in-out infinite;
}

.load-dots span:nth-child(2) {
  animation-delay: 0.15s;
}

.load-dots span:nth-child(3) {
  animation-delay: 0.3s;
}

@keyframes pulse {
  0%,
  100% {
    opacity: 0.3;
    transform: translateY(0);
  }
  50% {
    opacity: 1;
    transform: translateY(-3px);
  }
}

.project-grid {
  max-width: 1180px;
  margin: 0 auto;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(248px, 1fr));
  gap: 16px;
}

.project-card {
  background: var(--panel);
  border: 1px solid var(--border-soft);
  border-radius: 10px;
  overflow: hidden;
  cursor: pointer;
  transition: transform 0.16s ease, border-color 0.16s ease;
  animation: card-in 0.3s ease both;
}

.project-card:hover {
  transform: translateY(-2px);
  border-color: #4a4a4a;
}

@keyframes card-in {
  from {
    opacity: 0;
    transform: translateY(8px);
  }
  to {
    opacity: 1;
    transform: none;
  }
}

.card-art {
  position: relative;
  height: 112px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--muted-2);
  background: var(--panel-2);
}

.card-art.vertical {
  background: var(--panel-2);
}

.card-res {
  position: absolute;
  left: 10px;
  bottom: 9px;
  font-size: 11px;
  padding: 2px 7px;
  border-radius: 5px;
  background: rgba(0, 0, 0, 0.5);
  color: #e6e6e6;
}

.card-export {
  position: absolute;
  right: 9px;
  bottom: 9px;
  max-width: 62%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.card-body {
  padding: 12px 13px 13px;
}

.card-title-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.card-title-row h3 {
  margin: 0;
  font-size: 13px;
  font-weight: 650;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.card-delete {
  opacity: 0;
  transition: opacity 0.15s ease;
}

.project-card:hover .card-delete {
  opacity: 1;
}

.card-delete:hover {
  color: var(--danger);
}

.card-meta {
  display: flex;
  align-items: center;
  gap: 5px;
  margin-top: 6px;
  font-size: 11px;
  color: var(--muted-2);
}

.card-meta span {
  display: inline-flex;
  align-items: center;
  gap: 3px;
}

.dot {
  opacity: 0.5;
}

.new-card {
  min-height: 186px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: space-evenly;
  gap: 10px;
  padding: 16px;
  border-radius: 10px;
  border: 1px dashed #3a3a3a;
  background: transparent;
  color: var(--muted);
  font-weight: 500;
  transition: border-color 0.16s ease;
}

.new-card:hover {
  border-color: #6b6b6b;
  color: var(--text);
  background: var(--panel);
}

.new-card-hint {
  font-size: 11px;
  font-weight: 400;
  color: var(--muted-2);
}

.new-card-plus {
  width: 42px;
  height: 42px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--panel-2);
  border: 1px solid var(--border);
}

.launcher-foot {
  max-width: 1180px;
  margin: 30px auto 0;
  display: flex;
  justify-content: space-between;
  font-size: 11px;
  color: var(--muted-2);
}

.fps-row {
  display: flex;
  gap: 8px;
}

.fps-chip {
  flex: 1;
  height: 34px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--panel-2);
  color: var(--muted);
  transition: border-color 0.15s ease;
}

.fps-chip.active {
  border-color: #6b6b6b;
  background: var(--panel-3);
  color: var(--text);
}
</style>
