import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { api } from '../api'
import {
  defaultClip,
  defaultTextStyle,
  type AnalysisResult,
  type Asset,
  type CaptionStyle,
  type Clip,
  type Font,
  type Project,
  type Track,
  type TrackKind,
} from '../types'

export interface Selection {
  trackId: string
  clipId: string
}

export const useEditor = defineStore('editor', () => {
  const project = ref<Project | null>(null)
  const assets = ref<Asset[]>([])
  const fonts = ref<Font[]>([])
  const fontsLoaded = ref(false)
  const selection = ref<Selection | null>(null)
  const playhead = ref(0)
  const playing = ref(false)
  const zoom = ref(1)
  const saveState = ref<'idle' | 'saving' | 'saved' | 'error'>('idle')
  const undoStack = ref<string[]>([])
  const redoStack = ref<string[]>([])
  const importing = ref(false)
  const analysis = ref<AnalysisResult | null>(null)
  const toast = ref<{ id: number; message: string; kind: 'info' | 'success' | 'error' } | null>(null)

  const duration = computed(() => {
    const tl = project.value?.timeline
    if (!tl) return 0
    let max = 0
    for (const track of tl.tracks) {
      for (const clip of track.clips) max = Math.max(max, clip.start + clip.duration)
    }
    return max
  })

  const selected = computed(() => {
    const sel = selection.value
    const tl = project.value?.timeline
    if (!sel || !tl) return null
    const track = tl.tracks.find((t) => t.id === sel.trackId)
    const clip = track?.clips.find((c) => c.id === sel.clipId)
    if (!track || !clip) return null
    return { track, clip }
  })

  const selectedClip = computed(() => selected.value?.clip ?? null)
  const selectedTrack = computed(() => selected.value?.track ?? null)
  const canUndo = computed(() => undoStack.value.length > 0)
  const canRedo = computed(() => redoStack.value.length > 0)

  const assetMap = computed(() => {
    const map = new Map<string, Asset>()
    for (const asset of assets.value) map.set(asset.id, asset)
    return map
  })

  const defaultCaptionStyle = computed<CaptionStyle>(
    () =>
      project.value?.timeline.caption_style ?? {
        font_family: 'Arial',
        font_file: '',
        font_size: 72,
        bold: true,
        italic: false,
        color: '#FFFFFF',
        highlight_color: '#FFD400',
        stroke_width: 5,
        stroke_color: '#000000',
        shadow: 2,
        shadow_color: '#000000',
        opacity: 1,
        blend_mode: 'normal',
        position_y: 0.82,
        uppercase: false,
        highlight: true,
        words_per_chunk: 3,
      },
  )

  let toastTimer = 0
  function notify(message: string, kind: 'info' | 'success' | 'error' = 'info') {
    toast.value = { id: Date.now(), message, kind }
    window.clearTimeout(toastTimer)
    toastTimer = window.setTimeout(() => (toast.value = null), 4200)
  }

  function assetFor(clip: Clip): Asset | undefined {
    return clip.asset_id ? assetMap.value.get(clip.asset_id) : undefined
  }

  function trackByKind(kind: TrackKind): Track | null {
    return project.value?.timeline.tracks.find((t) => t.kind === kind) ?? null
  }

  // ---- history -------------------------------------------------------
  let lastHistoryKey = ''
  let lastHistoryAt = 0

  function pushHistory(key = '') {
    if (!project.value) return
    const now = Date.now()
    if (key && key === lastHistoryKey && now - lastHistoryAt < 900) {
      lastHistoryAt = now
      return
    }
    lastHistoryKey = key
    lastHistoryAt = now
    undoStack.value.push(JSON.stringify(project.value.timeline))
    if (undoStack.value.length > 100) undoStack.value.shift()
    redoStack.value = []
  }

  function restore(snapshot: string) {
    if (!project.value) return
    project.value.timeline = JSON.parse(snapshot)
    clampPlayhead()
    if (selection.value) {
      const sel = selection.value
      const track = project.value.timeline.tracks.find((t) => t.id === sel.trackId)
      if (!track || !track.clips.some((c) => c.id === sel.clipId)) selection.value = null
    }
    scheduleSave()
  }

  function undo() {
    if (!project.value || undoStack.value.length === 0) return
    redoStack.value.push(JSON.stringify(project.value.timeline))
    restore(undoStack.value.pop()!)
  }

  function redo() {
    if (!project.value || redoStack.value.length === 0) return
    undoStack.value.push(JSON.stringify(project.value.timeline))
    restore(redoStack.value.pop()!)
  }

  function mutate(fn: () => void, historyKey = '') {
    if (!project.value) return
    pushHistory(historyKey)
    fn()
    scheduleSave()
  }

  // ---- persistence ---------------------------------------------------
  let saveTimer = 0
  function scheduleSave() {
    const p = project.value
    if (!p) return
    saveState.value = 'saving'
    window.clearTimeout(saveTimer)
    saveTimer = window.setTimeout(async () => {
      const id = p.id
      const snapshot = JSON.stringify(p.timeline)
      try {
        const updated = await api.saveTimeline(id, JSON.parse(snapshot))
        if (project.value?.id === id) {
          project.value.updated_at = updated.updated_at
          saveState.value = 'saved'
        }
      } catch (err) {
        saveState.value = 'error'
        console.error(err)
        notify('Gagal menyimpan timeline: ' + (err as Error).message, 'error')
      }
    }, 650)
  }

  async function loadProject(p: Project) {
    project.value = p
    assets.value = await api.listAssets(p.id)
    selection.value = null
    playhead.value = 0
    playing.value = false
    undoStack.value = []
    redoStack.value = []
    saveState.value = 'saved'
    analysis.value = null
    try {
      analysis.value = await api.latestAnalysis(p.id)
    } catch {
      analysis.value = null
    }
    ensureFonts()
  }

  function replaceProject(updated: Project) {
    if (!project.value) return
    pushHistory()
    project.value.timeline = updated.timeline
    project.value.updated_at = updated.updated_at
    clampPlayhead()
    scheduleSave()
  }

  async function ensureFonts() {
    if (fontsLoaded.value) return
    fontsLoaded.value = true
    try {
      fonts.value = await api.fonts()
    } catch {
      fonts.value = []
    }
  }

  async function renameProject(name: string) {
    if (!project.value || !name.trim()) return
    const updated = await api.renameProject(project.value.id, name.trim())
    project.value.name = updated.name
  }

  // ---- playback ------------------------------------------------------
  let rafHandle = 0
  let playBase = 0
  let playStartedAt = 0

  function loop() {
    if (!playing.value) return
    const t = playBase + (performance.now() - playStartedAt) / 1000
    if (t >= duration.value) {
      playhead.value = duration.value
      pause()
      return
    }
    playhead.value = t
    rafHandle = requestAnimationFrame(loop)
  }

  function play() {
    if (!project.value || duration.value <= 0) return
    if (playhead.value >= duration.value - 0.02) playhead.value = 0
    playing.value = true
    playBase = playhead.value
    playStartedAt = performance.now()
    cancelAnimationFrame(rafHandle)
    rafHandle = requestAnimationFrame(loop)
  }

  function pause() {
    playing.value = false
    cancelAnimationFrame(rafHandle)
    rafHandle = 0
  }

  function togglePlay() {
    playing.value ? pause() : play()
  }

  function seek(time: number) {
    const clamped = Math.max(0, Math.min(time, duration.value))
    playhead.value = clamped
    if (playing.value) {
      playBase = clamped
      playStartedAt = performance.now()
    }
  }

  function clampPlayhead() {
    if (playhead.value > duration.value) playhead.value = duration.value
  }

  // ---- selection & clip editing --------------------------------------
  function select(trackId: string, clipId: string) {
    selection.value = { trackId, clipId }
  }

  function clearSelection() {
    selection.value = null
  }

  function updateClip(trackId: string, clipId: string, patch: Partial<Clip>, historyKey = '') {
    const track = project.value?.timeline.tracks.find((t) => t.id === trackId)
    const clip = track?.clips.find((c) => c.id === clipId)
    if (!clip) return
    if (!historyKey) pushHistory()
    Object.assign(clip, patch)
    playlistChanged()
  }

  function playlistChanged() {
    scheduleSave()
  }

  function deleteSelected() {
    const found = selected.value
    if (!project.value || !found) return
    if (found.track.locked) {
      notify('Track terkunci.', 'error')
      return
    }
    mutate(() => {
      found.track.clips = found.track.clips.filter((c) => c.id !== found.clip.id)
    })
    selection.value = null
  }

  function splitAtPlayhead() {
    if (!project.value) return
    const t = playhead.value
    let targetTrack: Track | null = selected.value?.track ?? null
    let target: Clip | null = selected.value?.clip ?? null
    if (!target || !targetTrack || t <= target.start || t >= target.start + target.duration) {
      targetTrack = null
      target = null
      for (const track of project.value.timeline.tracks) {
        if (track.locked || track.kind === 'caption') continue
        for (const clip of track.clips) {
          if (t > clip.start + 0.02 && t < clip.start + clip.duration - 0.02) {
            targetTrack = track
            target = clip
            break
          }
        }
        if (target) break
      }
    }
    if (!target || !targetTrack) {
      notify('Playhead tidak berada di dalam clip.', 'info')
      return
    }
    if (targetTrack.locked) {
      notify('Track terkunci.', 'error')
      return
    }
    mutate(() => {
      const offset = t - target!.start
      const right: Clip = {
        ...JSON.parse(JSON.stringify(target)),
        id: `clip_${Date.now()}_split`,
        start: t,
        in: target!.in + offset,
        duration: target!.duration - offset,
        words: undefined,
      }
      target!.duration = offset
      target!.out = target!.in + offset
      targetTrack!.clips.push(right)
      targetTrack!.clips.sort((a, b) => a.start - b.start)
      selection.value = { trackId: targetTrack!.id, clipId: target!.id }
    })
  }

  function addTextOverlay(text = 'Teks baru') {
    const track = trackByKind('overlay')
    if (!project.value) return
    if (!track) {
      notify('Track overlay tidak tersedia.', 'error')
      return
    }
    const clip = defaultClip({
      start: playhead.value,
      duration: 3,
      in: 0,
      out: 3,
      text,
      style: defaultTextStyle(),
    })
    mutate(() => track.clips.push(clip))
    selection.value = { trackId: track.id, clipId: clip.id }
  }

  function addAsset(asset: Asset, atTime?: number, trackId?: string) {
    if (!project.value) return
    const kind: TrackKind = asset.kind === 'audio' ? 'audio' : asset.kind === 'image' ? 'overlay' : 'video'
    let track = trackId
      ? project.value.timeline.tracks.find((t) => t.id === trackId && t.kind === kind) ?? null
      : trackByKind(kind)
    if (!track) track = trackByKind(kind) ?? trackByKind('video')
    if (!track) {
      notify('Track tujuan tidak ditemukan.', 'error')
      return
    }
    let start = atTime ?? playhead.value
    if (start < 0.05) start = track.clips.reduce((end, c) => Math.max(end, c.start + c.duration), 0)
    const length = asset.kind === 'image' ? 5 : Math.max(0.2, asset.duration || 3)
    const clip = defaultClip({
      asset_id: asset.id,
      start,
      duration: length,
      in: 0,
      out: length,
      x: 0.5,
      y: 0.5,
      scale: 1,
    })
    mutate(() => {
      track.clips.push(clip)
      track.clips.sort((a, b) => a.start - b.start)
      if (asset.kind === 'video' && asset.has_audio) {
        const audioTrack = trackByKind('audio')
        if (audioTrack) {
          audioTrack.clips.push({ ...clip, id: `clip_${Date.now()}_a`, duration: length, out: length })
          audioTrack.clips.sort((a, b) => a.start - b.start)
        }
      }
    })
    selection.value = { trackId: track.id, clipId: clip.id }
    notify(`${asset.filename} ditambahkan ke timeline.`, 'success')
  }

  function setTrackMuted(trackId: string, muted: boolean) {
    const track = project.value?.timeline.tracks.find((t) => t.id === trackId)
    if (!track) return
    mutate(() => (track.muted = muted))
  }

  function setTrackLocked(trackId: string, locked: boolean) {
    const track = project.value?.timeline.tracks.find((t) => t.id === trackId)
    if (!track) return
    mutate(() => (track.locked = locked))
  }

  function updateCaptionStyle(patch: Partial<CaptionStyle>, historyKey = 'caption-style') {
    if (!project.value) return
    pushHistory(historyKey)
    Object.assign(project.value.timeline.caption_style, patch)
    scheduleSave()
  }

  // ---- assets --------------------------------------------------------
  async function importFiles(files: File[]): Promise<Asset[]> {
    if (!project.value || files.length === 0) return []
    importing.value = true
    const imported: Asset[] = []
    try {
      for (const file of files) {
        try {
          const asset = await api.importAsset(project.value.id, file)
          assets.value.push(asset)
          imported.push(asset)
        } catch (err) {
          notify(`Gagal mengimpor ${file.name}: ${(err as Error).message}`, 'error')
        }
      }
      if (imported.length) notify(`${imported.length} media diimpor.`, 'success')
    } finally {
      importing.value = false
    }
    return imported
  }

  async function removeAsset(asset: Asset) {
    if (!project.value) return
    const used = project.value.timeline.tracks.some((t) => t.clips.some((c) => c.asset_id === asset.id))
    if (used) {
      notify('Asset masih dipakai di timeline. Hapus clip-nya dulu.', 'error')
      return
    }
    await api.deleteAsset(project.value.id, asset.id)
    assets.value = assets.value.filter((a) => a.id !== asset.id)
  }

  function closeProject() {
    pause()
    project.value = null
    assets.value = []
    selection.value = null
    playhead.value = 0
    analysis.value = null
    undoStack.value = []
    redoStack.value = []
  }

  return {
    project,
    assets,
    fonts,
    selection,
    playhead,
    playing,
    zoom,
    saveState,
    importing,
    analysis,
    toast,
    duration,
    selected,
    selectedClip,
    selectedTrack,
    canUndo,
    canRedo,
    assetMap,
    defaultCaptionStyle,
    assetFor,
    notify,
    loadProject,
    replaceProject,
    ensureFonts,
    renameProject,
    play,
    pause,
    togglePlay,
    seek,
    select,
    clearSelection,
    updateClip,
    deleteSelected,
    splitAtPlayhead,
    addTextOverlay,
    addAsset,
    setTrackMuted,
    setTrackLocked,
    updateCaptionStyle,
    importFiles,
    removeAsset,
    closeProject,
    undo,
    redo,
    pushHistory,
    scheduleSave,
    mutate,
  }
})
