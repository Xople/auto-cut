import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { api, subscribeProjectSync } from '../api'
import { applyBundledFonts } from '../lib/fonts'
import {
  clampTransform,
  hasTransformAnimation,
  keyframeAt,
  MAX_CLIP_SCALE,
  MIN_CLIP_SCALE,
  removeTransformKeyframe,
  transformAt,
  upsertTransformKeyframe,
  type TransformValue,
} from '../lib/transform'
import {
  DEFAULT_TRANSITION_DURATION,
  defaultClip,
  defaultTextStyle,
  type AnalysisResult,
  type Asset,
  type CaptionStyle,
  type Clip,
  type Font,
  type Project,
  type Timeline,
  type Track,
  type TrackKind,
  type TransitionType,
} from '../types'

export interface Selection {
  trackId: string
  clipId: string
}

export type EditMode = 'overwrite' | 'insert'

export const useEditor = defineStore('editor', () => {
  const project = ref<Project | null>(null)
  const assets = ref<Asset[]>([])
  const fonts = ref<Font[]>([])
  const fontsLoaded = ref(false)
  const selection = ref<Selection | null>(null)
  const selectionIds = ref<string[]>([])
  const rippleDelete = ref(false)
  const editMode = ref<EditMode>('overwrite')
  const clipboard = ref<Array<{ trackId: string; clip: Clip }>>([])
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
  const selectedEntries = computed(() => {
    const tl = project.value?.timeline
    if (!tl || selectionIds.value.length === 0) return []
    const wanted = new Set(selectionIds.value)
    const list: Array<{ track: Track; clip: Clip }> = []
    for (const track of tl.tracks) {
      for (const clip of track.clips) if (wanted.has(clip.id)) list.push({ track, clip })
    }
    return list
  })
  const selectionCount = computed(() => selectedEntries.value.length)
  const canPaste = computed(() => clipboard.value.length > 0)
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

  function locateClip(clipId: string): { track: Track; clip: Clip } | null {
    const tl = project.value?.timeline
    if (!tl) return null
    for (const track of tl.tracks) {
      const clip = track.clips.find((c) => c.id === clipId)
      if (clip) return { track, clip }
    }
    return null
  }

  function linkedPartner(clip: Clip): { track: Track; clip: Clip } | null {
    return clip.linked_id ? locateClip(clip.linked_id) : null
  }

  // Clip overlaps are illegal on one track, but two tracks may stack freely.
  function findCollision(track: Track, start: number, duration: number, excludeIds = new Set<string>()): Clip | null {
    const end = start + duration
    for (const other of track.clips) {
      if (excludeIds.has(other.id)) continue
      if (other.start < end - 1e-4 && other.start + other.duration > start + 1e-4) return other
    }
    return null
  }

  // Older projects stored a video's audio as a separate clip on the audio track.
  // Some were linked, some were not, so detect both forms: move the audio
  // settings onto the video clip and drop the duplicate to avoid doubled sound.
  const AUDIO_TWIN_TOLERANCE = 0.06

  function sameTiming(a: Clip, b: Clip): boolean {
    return (
      Math.abs(a.start - b.start) <= AUDIO_TWIN_TOLERANCE &&
      Math.abs(a.duration - b.duration) <= AUDIO_TWIN_TOLERANCE &&
      Math.abs(a.in - b.in) <= AUDIO_TWIN_TOLERANCE
    )
  }

  function absorbAudioSettings(video: Clip, audio: Clip) {
    if (video.volume === 1) video.volume = audio.volume
    if (video.fade_in === 0) video.fade_in = audio.fade_in
    if (video.fade_out === 0) video.fade_out = audio.fade_out
    if (video.linked_id === audio.id) video.linked_id = undefined
  }

  function migrateLinkedAudio(timeline: Timeline): boolean {
    const located = new Map<string, { track: Track; clip: Clip }>()
    const videosByAsset = new Map<string, Clip[]>()
    for (const track of timeline.tracks) {
      for (const clip of track.clips) {
        located.set(clip.id, { track, clip })
        if (track.kind === 'video' && clip.asset_id) {
          const list = videosByAsset.get(clip.asset_id) ?? []
          list.push(clip)
          videosByAsset.set(clip.asset_id, list)
        }
      }
    }
    let changed = false
    for (const track of timeline.tracks) {
      if (track.kind !== 'audio' || track.clips.length === 0) continue
      const kept: Clip[] = []
      for (const clip of track.clips) {
        const linked = clip.linked_id ? located.get(clip.linked_id) : undefined
        if (linked && linked.track.kind === 'video') {
          absorbAudioSettings(linked.clip, clip)
          changed = true
          continue
        }
        const twin = clip.asset_id
          ? videosByAsset.get(clip.asset_id)?.find((video) => sameTiming(video, clip))
          : undefined
        if (twin) {
          absorbAudioSettings(twin, clip)
          changed = true
          continue
        }
        kept.push(clip)
      }
      if (kept.length !== track.clips.length) track.clips = kept
    }
    return changed
  }

  function pruneSelection() {
    const ids = selectionIds.value.filter((id) => locateClip(id))
    selectionIds.value = ids
    if (!selection.value || !ids.includes(selection.value.clipId)) {
      const first = ids[0] ? locateClip(ids[0]) : null
      selection.value = first ? { trackId: first.track.id, clipId: first.clip.id } : null
    }
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
    pruneSelection()
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
  let saveChain: Promise<void> = Promise.resolve()
  let dirty = false
  let saveRetries = 0

  function scheduleSave() {
    if (!project.value) return
    dirty = true
    saveState.value = 'saving'
    window.clearTimeout(saveTimer)
    saveTimer = window.setTimeout(() => {
      void flushSave()
    }, 650)
  }

  // Serializes writes so a debounced save can never land after a newer one.
  function flushSave(): Promise<void> {
    window.clearTimeout(saveTimer)
    saveTimer = 0
    const p = project.value
    if (!p || !dirty) return saveChain
    const snapshot = JSON.stringify(p.timeline)
    dirty = false
    saveState.value = 'saving'
    saveChain = saveChain
      .catch(() => {})
      .then(async () => {
        try {
          const updated = await api.saveTimeline(p.id, JSON.parse(snapshot))
          saveRetries = 0
          if (project.value?.id !== p.id) return
          project.value.updated_at = updated.updated_at
          if (!dirty) saveState.value = 'saved'
        } catch (err) {
          dirty = true
          saveState.value = 'error'
          console.error(err)
          if (saveRetries === 0) notify('Gagal menyimpan timeline: ' + (err as Error).message, 'error')
          // Backend can be busy (import/export); retry a few times before giving up.
          if (saveRetries < 3) {
            saveRetries += 1
            window.setTimeout(() => {
              if (project.value?.id === p.id) void flushSave()
            }, 1500 * saveRetries)
          }
        }
      })
    return saveChain
  }

  async function saveNow() {
    await flushSave()
    if (saveState.value === 'saved') notify('Timeline disimpan.', 'success')
  }

  // Best-effort write when the window is closing; the sidecar may go away first.
  function flushKeepalive() {
    const p = project.value
    if (!p || !dirty) return
    dirty = false
    try {
      api.saveTimelineKeepalive(p.id, JSON.parse(JSON.stringify(p.timeline)))
    } catch {
      dirty = true
    }
  }

  let stopProjectSync: (() => void) | null = null

  async function reloadFromRemote() {
    const current = project.value
    if (!current || dirty) return
    try {
      const [freshProject, freshAssets] = await Promise.all([
        api.getProject(current.id),
        api.listAssets(current.id),
      ])
      if (project.value?.id !== current.id || dirty) return
      project.value = freshProject
      assets.value = freshAssets
      pruneSelection()
      clampPlayhead()
      notify('Project diperbarui oleh AI Agent (MCP).', 'info')
    } catch {
      /* ignore transient reload error */
    }
  }

  async function loadProject(p: Project) {
    await flushSave()
    if (stopProjectSync) {
      stopProjectSync()
      stopProjectSync = null
    }
    project.value = p
    stopProjectSync = subscribeProjectSync(p.id, () => {
      void reloadFromRemote()
    })
    if (migrateLinkedAudio(p.timeline)) {
      scheduleSave()
      notify('Audio bawaan video sekarang menyatu dengan clip videonya.', 'info')
    }
    assets.value = await api.listAssets(p.id)
    selection.value = null
    selectionIds.value = []
    clipboard.value = []
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
    pruneSelection()
    clampPlayhead()
    scheduleSave()
  }

  async function ensureFonts() {
    if (fontsLoaded.value) return
    try {
      fonts.value = await api.fonts()
      applyBundledFonts(fonts.value)
      fontsLoaded.value = true
    } catch {
      fontsLoaded.value = false
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
  function select(trackId: string, clipId: string, additive = false) {
    if (!additive) {
      selection.value = { trackId, clipId }
      selectionIds.value = [clipId]
      return
    }
    const index = selectionIds.value.indexOf(clipId)
    if (index >= 0) {
      selectionIds.value.splice(index, 1)
      if (selection.value?.clipId === clipId) {
        const first = selectionIds.value[0] ? locateClip(selectionIds.value[0]) : null
        selection.value = first ? { trackId: first.track.id, clipId: first.clip.id } : null
      }
      return
    }
    selectionIds.value.push(clipId)
    selection.value = { trackId, clipId }
  }

  function setSelection(entries: Selection[]) {
    const seen = new Set<string>()
    const valid: Selection[] = []
    for (const entry of entries) {
      if (seen.has(entry.clipId)) continue
      const found = locateClip(entry.clipId)
      if (!found) continue
      seen.add(entry.clipId)
      valid.push({ trackId: found.track.id, clipId: found.clip.id })
    }
    selectionIds.value = valid.map((v) => v.clipId)
    selection.value = valid[0] ?? null
  }

  function isSelected(clipId: string): boolean {
    return selectionIds.value.includes(clipId)
  }

  function selectAll() {
    const tl = project.value?.timeline
    if (!tl) return
    const entries: Selection[] = []
    for (const track of tl.tracks) {
      for (const clip of track.clips) entries.push({ trackId: track.id, clipId: clip.id })
    }
    setSelection(entries)
  }

  function clearSelection() {
    selection.value = null
    selectionIds.value = []
  }

  function updateClip(trackId: string, clipId: string, patch: Partial<Clip>, historyKey = '') {
    const track = project.value?.timeline.tracks.find((t) => t.id === trackId)
    const clip = track?.clips.find((c) => c.id === clipId)
    if (!clip) return
    if (!historyKey) pushHistory()
    Object.assign(clip, patch)
    playlistChanged()
  }

  function clampTransformPatch(value: TransformValue): TransformValue {
    return {
      x: clampTransform(value.x, 0, 1),
      y: clampTransform(value.y, 0, 1),
      scale: clampTransform(value.scale, MIN_CLIP_SCALE, MAX_CLIP_SCALE),
      rotation: clampTransform(value.rotation, -360, 360),
    }
  }

  // Once a clip has keyframes, transform edits become keyframes at the
  // playhead so every adjustment is animatable. Without keyframes they edit
  // the clip's static transform.
  function setClipTransform(trackId: string, clipId: string, patch: Partial<TransformValue>) {
    const track = project.value?.timeline.tracks.find((t) => t.id === trackId)
    const clip = track?.clips.find((c) => c.id === clipId)
    if (!clip) return
    const current = transformAt(clip, playhead.value)
    const next = clampTransformPatch({ ...current, ...patch })
    pushHistory(`transform-${clipId}`)
    if (hasTransformAnimation(clip)) {
      clip.keyframes = upsertTransformKeyframe(clip, playhead.value, next)
    } else {
      clip.x = next.x
      clip.y = next.y
      clip.scale = next.scale
      clip.rotation = next.rotation
    }
    playlistChanged()
  }

  function toggleClipKeyframe(trackId: string, clipId: string) {
    const track = project.value?.timeline.tracks.find((t) => t.id === trackId)
    const clip = track?.clips.find((c) => c.id === clipId)
    if (!clip) return
    pushHistory(`keyframe-${clipId}`)
    if (keyframeAt(clip, playhead.value)) {
      const value = transformAt(clip, playhead.value)
      const frames = removeTransformKeyframe(clip, playhead.value)
      if (frames.length === 0) {
        clip.x = value.x
        clip.y = value.y
        clip.scale = value.scale
        clip.rotation = value.rotation
        clip.keyframes = undefined
      } else {
        clip.keyframes = frames
      }
      notify('Keyframe dihapus.', 'info')
    } else {
      clip.keyframes = upsertTransformKeyframe(clip, playhead.value, transformAt(clip, playhead.value))
      notify('Keyframe ditambahkan.', 'success')
    }
    playlistChanged()
  }

  function clearClipTransform(trackId: string, clipId: string) {
    const track = project.value?.timeline.tracks.find((t) => t.id === trackId)
    const clip = track?.clips.find((c) => c.id === clipId)
    if (!clip) return
    pushHistory(`transform-reset-${clipId}`)
    clip.x = 0.5
    clip.y = 0.5
    clip.scale = 1
    clip.rotation = 0
    clip.keyframes = undefined
    notify('Transform clip direset.', 'info')
    playlistChanged()
  }

  function playlistChanged() {
    scheduleSave()
  }

  function setClipTransition(type: TransitionType | 'none', duration?: number, clipId?: string) {
    const entries = clipId
      ? [locateClip(clipId)].filter((entry): entry is { track: Track; clip: Clip } => !!entry)
      : selectedEntries.value
    const targets = entries.filter(({ track, clip }) => track.kind === 'video' && !!clip.asset_id)
    if (targets.length === 0) {
      notify('Pilih clip video dulu untuk menambah transisi.', 'info')
      return
    }
    mutate(() => {
      for (const { clip } of targets) {
        if (type === 'none') {
          clip.transition = null
          continue
        }
        const limit = Math.max(0.1, Math.min(3, clip.duration))
        const wanted = duration ?? clip.transition?.duration ?? DEFAULT_TRANSITION_DURATION
        clip.transition = { type, duration: Math.min(limit, Math.max(0.1, wanted)) }
      }
    }, 'transition')
    notify(type === 'none' ? 'Transisi dihapus.' : 'Transisi diterapkan.', 'success')
  }

  function uniqueClipId(suffix = ''): string {
    return `clip_${Date.now()}_${Math.random().toString(36).slice(2, 7)}${suffix}`
  }

  function deleteSelected(ripple = rippleDelete.value) {
    if (!project.value) return
    const entries = selectedEntries.value
    if (entries.length === 0) return
    const removals = new Map<string, Set<string>>()
    const addRemoval = (trackId: string, clipId: string) => {
      let ids = removals.get(trackId)
      if (!ids) {
        ids = new Set()
        removals.set(trackId, ids)
      }
      ids.add(clipId)
    }
    let blocked = 0
    for (const { track, clip } of entries) {
      if (track.locked) {
        blocked++
        continue
      }
      addRemoval(track.id, clip.id)
      const partner = linkedPartner(clip)
      if (partner && !partner.track.locked) addRemoval(partner.track.id, partner.clip.id)
    }
    if (removals.size === 0) {
      notify('Track terkunci.', 'error')
      return
    }
    if (blocked > 0) notify(`${blocked} clip di track terkunci dilewati.`, 'info')
    const timeline = project.value.timeline
    mutate(() => {
      for (const [trackId, ids] of removals) {
        const track = timeline.tracks.find((t) => t.id === trackId)
        if (!track) continue
        const removed = track.clips.filter((c) => ids.has(c.id))
        track.clips = track.clips.filter((c) => !ids.has(c.id))
        if (!ripple || removed.length === 0) continue
        const intervals = removed
          .map((c) => ({ start: c.start, end: c.start + c.duration }))
          .sort((a, b) => a.start - b.start)
        for (const clip of track.clips) {
          let shift = 0
          for (const interval of intervals) {
            const before = Math.min(interval.end, clip.start) - Math.min(interval.start, clip.start)
            if (before > 0) shift += before
          }
          clip.start = Math.max(0, clip.start - shift)
        }
      }
    })
    clearSelection()
  }

  function overwriteRange(track: Track, start: number, end: number) {
    const kept: Clip[] = []
    for (const clip of track.clips) {
      const clipEnd = clip.start + clip.duration
      if (clipEnd <= start + 1e-6 || clip.start >= end - 1e-6) {
        kept.push(clip)
        continue
      }
      if (clip.start >= start - 1e-6 && clipEnd <= end + 1e-6) continue
      if (clip.start < start - 1e-6 && clipEnd > end + 1e-6) {
        const right: Clip = {
          ...(JSON.parse(JSON.stringify(clip)) as Clip),
          id: uniqueClipId('_r'),
          start: end,
          in: clip.in + (end - clip.start),
          duration: clipEnd - end,
          linked_id: undefined,
        }
        clip.duration = start - clip.start
        clip.out = clip.in + clip.duration
        kept.push(clip, right)
        continue
      }
      if (clip.start < start - 1e-6) {
        clip.duration = start - clip.start
        clip.out = clip.in + clip.duration
        kept.push(clip)
        continue
      }
      const cut = end - clip.start
      clip.start = end
      clip.in += cut
      clip.duration -= cut
      clip.out = clip.in + clip.duration
      kept.push(clip)
    }
    track.clips = kept.sort((a, b) => a.start - b.start)
  }

  function placeClip(track: Track, clip: Clip, mode: EditMode = editMode.value) {
    if (mode === 'insert') {
      for (const existing of track.clips) {
        if (existing.start >= clip.start - 1e-6) existing.start += clip.duration
      }
    } else {
      overwriteRange(track, clip.start, clip.start + clip.duration)
    }
    track.clips.push(clip)
    track.clips.sort((a, b) => a.start - b.start)
  }

  function copySelected() {
    const entries = selectedEntries.value
    if (entries.length === 0) return
    const seen = new Set(entries.map((entry) => entry.clip.id))
    const group = [...entries]
    for (const { clip } of entries) {
      const partner = linkedPartner(clip)
      if (partner && !seen.has(partner.clip.id)) {
        seen.add(partner.clip.id)
        group.push(partner)
      }
    }
    clipboard.value = group.map(({ track, clip }) => ({
      trackId: track.id,
      clip: JSON.parse(JSON.stringify(clip)) as Clip,
    }))
    notify(`${group.length} clip disalin.`, 'success')
  }

  function cutSelected() {
    if (selectedEntries.value.length === 0) return
    copySelected()
    deleteSelected(false)
  }

  function pasteAtPlayhead() {
    const timeline = project.value?.timeline
    const items = clipboard.value
    if (!timeline || items.length === 0) return
    const minStart = Math.min(...items.map((item) => item.clip.start))
    const base = playhead.value
    const idMap = new Map<string, string>()
    const prepared: Array<{ track: Track; clip: Clip; linkedFrom?: string }> = []
    for (const item of items) {
      const source = timeline.tracks.find((t) => t.id === item.trackId)
      const track =
        (source && !source.locked ? source : null) ??
        timeline.tracks.find((t) => t.kind === source?.kind && !t.locked) ??
        null
      if (!track) continue
      const copy = JSON.parse(JSON.stringify(item.clip)) as Clip
      copy.id = uniqueClipId()
      copy.start = Math.max(0, base + (item.clip.start - minStart))
      const linkedFrom = copy.linked_id
      copy.linked_id = undefined
      idMap.set(item.clip.id, copy.id)
      prepared.push({ track, clip: copy, linkedFrom })
    }
    if (prepared.length === 0) {
      notify('Tidak ada track tujuan yang tersedia.', 'error')
      return
    }
    for (const item of prepared) {
      if (item.linkedFrom && idMap.has(item.linkedFrom)) item.clip.linked_id = idMap.get(item.linkedFrom)
    }
    const span = Math.max(...prepared.map((p) => p.clip.start + p.clip.duration)) - base
    mutate(() => {
      if (editMode.value === 'insert') {
        const touched = new Set(prepared.map((p) => p.track.id))
        for (const track of timeline.tracks) {
          if (!touched.has(track.id)) continue
          for (const clip of track.clips) if (clip.start >= base - 1e-6) clip.start += span
        }
        for (const item of prepared) {
          item.track.clips.push(item.clip)
          item.track.clips.sort((a, b) => a.start - b.start)
        }
      } else {
        for (const item of prepared) placeClip(item.track, item.clip)
      }
    })
    setSelection(prepared.map((p) => ({ trackId: p.track.id, clipId: p.clip.id })))
    notify(`${prepared.length} clip ditempel di playhead.`, 'success')
  }

  function nudgeSelected(seconds: number) {
    if (!project.value || seconds === 0) return
    const entries = selectedEntries.value
    if (entries.length === 0) return
    const seen = new Set(entries.map((entry) => entry.clip.id))
    const group = [...entries]
    for (const { clip } of entries) {
      const partner = linkedPartner(clip)
      if (partner && !seen.has(partner.clip.id)) {
        seen.add(partner.clip.id)
        group.push(partner)
      }
    }
    const minStart = Math.min(...group.map((item) => item.clip.start))
    const delta = Math.max(seconds, -minStart)
    if (Math.abs(delta) < 1e-9) return
    mutate(() => {
      for (const item of group) item.clip.start = Math.max(0, item.clip.start + delta)
    }, 'nudge')
  }

  function splitClipAt(trackId: string, clipId: string, time: number): boolean {
    const timeline = project.value?.timeline
    const track = timeline?.tracks.find((t) => t.id === trackId)
    const clip = track?.clips.find((c) => c.id === clipId)
    if (!timeline || !track || !clip) return false
    if (track.locked) {
      notify('Track terkunci.', 'error')
      return false
    }
    const offset = time - clip.start
    if (offset <= 0.02 || offset >= clip.duration - 0.02) {
      notify('Klik di dalam clip untuk memotong.', 'info')
      return false
    }
    mutate(() => {
      splitClipPair(track, clip, time)
      selection.value = { trackId, clipId }
      selectionIds.value = [clipId]
    }, `split-${clipId}`)
    return true
  }

  function splitClipPair(track: Track, clip: Clip, time: number) {
    const right: Clip = {
      ...(JSON.parse(JSON.stringify(clip)) as Clip),
      id: uniqueClipId('_split'),
      start: time,
      in: clip.in + (time - clip.start),
      duration: clip.start + clip.duration - time,
    }
    right.linked_id = undefined
    clip.duration = time - clip.start
    clip.out = clip.in + clip.duration
    const partner = linkedPartner(clip)
    let partnerRight: { track: Track; clip: Clip } | null = null
    if (partner && !partner.track.locked) {
      const partnerOffset = time - partner.clip.start
      if (partnerOffset > 0.02 && partnerOffset < partner.clip.duration - 0.02) {
        const pRight: Clip = {
          ...(JSON.parse(JSON.stringify(partner.clip)) as Clip),
          id: uniqueClipId('_split'),
          start: time,
          in: partner.clip.in + partnerOffset,
          duration: partner.clip.duration - partnerOffset,
        }
        pRight.linked_id = right.id
        right.linked_id = pRight.id
        partner.clip.duration = partnerOffset
        partner.clip.out = partner.clip.in + partnerOffset
        partnerRight = { track: partner.track, clip: pRight }
      }
    }
    track.clips.push(right)
    track.clips.sort((a, b) => a.start - b.start)
    if (partnerRight) {
      partnerRight.track.clips.push(partnerRight.clip)
      partnerRight.track.clips.sort((a, b) => a.start - b.start)
    }
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
    splitClipAt(targetTrack.id, target.id, t)
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
    selectionIds.value = [clip.id]
  }

  function nextTrackName(kind: TrackKind): string {
    const labels: Record<TrackKind, string> = {
      video: 'Video',
      audio: 'Audio',
      overlay: 'Overlay',
      caption: 'Caption',
    }
    const count = project.value?.timeline.tracks.filter((t) => t.kind === kind).length ?? 0
    return count === 0 ? labels[kind] : `${labels[kind]} ${count + 1}`
  }

  // Dropping a video on a spot that is already taken adds a new layer directly
  // above the target track instead of rejecting the edit.
  function insertTrackAbove(target: Track): Track | null {
    const timeline = project.value?.timeline
    if (!timeline) return null
    if (timeline.tracks.length >= 64) {
      notify('Batas 64 track tercapai.', 'error')
      return null
    }
    const track: Track = {
      id: `${target.kind}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 5)}`,
      kind: target.kind,
      name: nextTrackName(target.kind),
      muted: false,
      locked: false,
      hidden: false,
      height: target.height || 52,
      clips: [],
    }
    mutate(() => timeline.tracks.splice(timeline.tracks.indexOf(target), 0, track), 'auto-layer')
    notify(`Layer ${track.name} dibuat otomatis.`, 'info')
    return track
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
    if (kind === 'video' && findCollision(track, start, length)) {
      const layered = insertTrackAbove(track)
      if (!layered) return
      track = layered
    }
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
    // A video's own audio stays inside the clip; it is mixed at playback and
    // render time, so no separate clip is placed on the audio track.
    mutate(() => placeClip(track!, clip))
    selection.value = { trackId: track.id, clipId: clip.id }
    selectionIds.value = [clip.id]
    notify(`${asset.filename} ditambahkan ke timeline.`, 'success')
  }

  function addAssetOnNewTrack(asset: Asset, atTime?: number) {
    const timeline = project.value?.timeline
    if (!timeline) return
    const kind: TrackKind = asset.kind === 'audio' ? 'audio' : asset.kind === 'image' ? 'overlay' : 'video'
    const anchor = timeline.tracks.find((t) => t.kind === kind) ?? null
    const track = anchor ? insertTrackAbove(anchor) : addTrack(kind)
    if (!track) return
    addAsset(asset, atTime, track.id)
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

  function setTrackHidden(trackId: string, hidden: boolean) {
    const track = project.value?.timeline.tracks.find((t) => t.id === trackId)
    if (!track) return
    mutate(() => (track.hidden = hidden))
  }

  function setTrackHeight(trackId: string, height: number, commit = false) {
    const track = project.value?.timeline.tracks.find((t) => t.id === trackId)
    if (!track) return
    const value = Math.min(160, Math.max(36, Math.round(height)))
    if (value === track.height) return
    if (commit) {
      mutate(() => (track.height = value), 'track-height')
      return
    }
    track.height = value
    pushHistory('track-height')
    scheduleSave()
  }

  function addTrack(kind: TrackKind): Track | null {
    const timeline = project.value?.timeline
    if (!timeline) return null
    if (timeline.tracks.length >= 64) {
      notify('Batas 64 track tercapai.', 'error')
      return null
    }
    const sameKind = timeline.tracks.filter((t) => t.kind === kind)
    const labels: Record<TrackKind, string> = {
      video: 'Video',
      audio: 'Audio',
      overlay: 'Overlay',
      caption: 'Caption',
    }
    const name = sameKind.length === 0 ? labels[kind] : `${labels[kind]} ${sameKind.length + 1}`
    const track: Track = {
      id: `${kind}-${Date.now().toString(36)}`,
      kind,
      name,
      muted: false,
      locked: false,
      hidden: false,
      height: 52,
      clips: [],
    }
    const index = timeline.tracks.findIndex((t) => t.kind === kind)
    mutate(() => timeline.tracks.splice(index < 0 ? timeline.tracks.length : index, 0, track))
    return track
  }

  function removeTrack(trackId: string) {
    const timeline = project.value?.timeline
    if (!timeline) return
    const index = timeline.tracks.findIndex((t) => t.id === trackId)
    if (index < 0) return
    if (timeline.tracks.length <= 1) {
      notify('Minimal satu track harus tersisa.', 'error')
      return
    }
    mutate(() => timeline.tracks.splice(index, 1))
    pruneSelection()
  }

  function moveTrack(trackId: string, delta: number) {
    const tracks = project.value?.timeline.tracks
    if (!tracks) return
    const index = tracks.findIndex((t) => t.id === trackId)
    const target = index + delta
    if (index < 0 || target < 0 || target >= tracks.length) return
    mutate(() => {
      const [track] = tracks.splice(index, 1)
      tracks.splice(target, 0, track)
    })
  }

  function moveClipToTrack(clipId: string, fromTrackId: string, toTrackId: string): boolean {
    const tracks = project.value?.timeline.tracks
    if (!tracks) return false
    const from = tracks.find((t) => t.id === fromTrackId)
    const to = tracks.find((t) => t.id === toTrackId)
    if (!from || !to || from.id === to.id) return from?.id === to?.id
    if (to.locked) {
      notify('Track tujuan terkunci.', 'error')
      return false
    }
    if (to.kind !== from.kind) {
      notify(`Clip ${from.kind} tidak cocok untuk track ${to.kind}.`, 'error')
      return false
    }
    mutate(() => {
      const index = from.clips.findIndex((c) => c.id === clipId)
      if (index < 0) return
      const [clip] = from.clips.splice(index, 1)
      clip.start = Math.max(0, clip.start)
      to.clips.push(clip)
      to.clips.sort((a, b) => a.start - b.start)
    }, `drag-${clipId}`)
    selection.value = { trackId: to.id, clipId }
    selectionIds.value = [clipId]
    return true
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

  async function closeProject() {
    pause()
    await flushSave()
    project.value = null
    assets.value = []
    selection.value = null
    selectionIds.value = []
    clipboard.value = []
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
    selectionIds,
    rippleDelete,
    editMode,
    clipboard,
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
    selectedEntries,
    selectionCount,
    canPaste,
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
    setSelection,
    isSelected,
    selectAll,
    clearSelection,
    updateClip,
    setClipTransform,
    toggleClipKeyframe,
    clearClipTransform,
    setClipTransition,
    findCollision,
    deleteSelected,
    copySelected,
    cutSelected,
    pasteAtPlayhead,
    nudgeSelected,
    splitAtPlayhead,
    splitClipAt,
    addTextOverlay,
    addAsset,
    addAssetOnNewTrack,
    setTrackMuted,
    setTrackLocked,
    setTrackHidden,
    setTrackHeight,
    addTrack,
    removeTrack,
    moveTrack,
    moveClipToTrack,
    updateCaptionStyle,
    importFiles,
    removeAsset,
    closeProject,
    undo,
    redo,
    pushHistory,
    scheduleSave,
    flushSave,
    saveNow,
    flushKeepalive,
    mutate,
  }
})
