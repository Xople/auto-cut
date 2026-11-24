export type LibraryTool = 'media' | 'text' | 'transitions' | 'effects'
export type TrackKind = 'video' | 'audio' | 'overlay' | 'caption'
export const MUSIC_TRACK_ID = 'audio-bgm'
export type AssetKind = 'video' | 'audio' | 'image'
export type CutReason = 'silence' | 'filler' | 'retake' | 'off_topic' | 'keep'
export type TransitionType =
  | 'pull_in'
  | 'pull_out'
  | 'blink'
  | 'flash'
  | 'fade'
  | 'slide_up'
  | 'slide_down'
  | 'slide_right'
  | 'slide_left'
  | 'spin_in'

export interface Transition {
  type: TransitionType
  duration: number
}

export const TRANSITIONS: Array<{ value: TransitionType; label: string; hint: string }> = [
  { value: 'pull_in', label: 'Pull In', hint: 'Zoom masuk cepat dari 1.22× ke normal' },
  { value: 'pull_out', label: 'Pull Out', hint: 'Zoom membesar dari 0.78× ke layar penuh' },
  { value: 'blink', label: 'Blink', hint: 'Efek kedipan mata / shutter gelap di awal scene' },
  { value: 'flash', label: 'Flash', hint: 'Kilatan cahaya kamera putih terang' },
  { value: 'fade', label: 'Fade', hint: 'Cross-dissolve halus dari transparan' },
  { value: 'slide_up', label: 'Swipe Up', hint: 'Geser vertikal dari bawah ke atas (gaya Shorts)' },
  { value: 'slide_down', label: 'Swipe Down', hint: 'Geser vertikal dari atas ke bawah' },
  { value: 'slide_right', label: 'Slide Right', hint: 'Masuk bergeser dari kanan' },
  { value: 'slide_left', label: 'Slide Left', hint: 'Masuk bergeser dari kiri' },
  { value: 'spin_in', label: 'Spin In', hint: 'Whip-zoom masuk dengan rotasi dinamis' },
]

export const DEFAULT_TRANSITION_DURATION = 0.38

export interface WordTiming {
  word: string
  start: number
  end: number
  confidence?: number
}

export interface TextStyle {
  font_family: string
  font_file: string
  font_size: number
  bold: boolean
  italic: boolean
  color: string
  stroke_width: number
  stroke_color: string
  shadow: number
  shadow_color: string
  align: string
  uppercase: boolean
  background_color: string
  background_alpha: number
}

export interface TransformKeyframe {
  time: number
  x: number
  y: number
  scale: number
  rotation: number
}

export type VideoEffect = 'mono' | 'sepia' | 'vivid'

export const VIDEO_EFFECTS = [
  { value: 'mono', label: 'Monokrom' },
  { value: 'sepia', label: 'Sepia' },
  { value: 'vivid', label: 'Vivid' },
] as const

export function videoEffectFilter(effect?: VideoEffect, intensity = 1): string {
  const amount = Math.max(0, Math.min(1, intensity))
  switch (effect) {
    case 'mono': return `grayscale(${amount})`
    case 'sepia': return `sepia(${amount})`
    case 'vivid': return `saturate(${1 + 0.7 * amount})`
    default: return ''
  }
}

export interface Clip {
  id: string
  asset_id?: string
  start: number
  duration: number
  in: number
  out: number
  x: number
  y: number
  position_set?: boolean
  scale: number
  rotation: number
  opacity: number
  blend_mode: string
  text?: string
  style?: TextStyle | null
  volume: number
  fade_in: number
  fade_out: number
  speed: number
  linked_id?: string
  words?: WordTiming[]
  caption_position?: { x: number; y: number }
  transition?: Transition | null
  keyframes?: TransformKeyframe[]
  video_effect?: VideoEffect
  effect_intensity?: number
  background_fill?: '' | 'black' | 'blur'
  background_blur?: number
  background_dim?: number
}

export function captionPosition(clip: Clip, style: CaptionStyle) {
  return clip.caption_position ?? { x: 0.5, y: style.position_y > 0 ? Math.min(0.98, style.position_y) : 0.82 }
}

export interface Track {
  id: string
  kind: TrackKind
  name: string
  muted: boolean
  locked: boolean
  hidden: boolean
  height: number
  clips: Clip[]
}

export interface CaptionStyle {
  font_family: string
  font_file: string
  font_size: number
  bold: boolean
  italic: boolean
  color: string
  highlight_color: string
  stroke_width: number
  stroke_color: string
  shadow: number
  shadow_color: string
  opacity: number
  blend_mode: string
  position_y: number
  uppercase: boolean
  highlight: boolean
  words_per_chunk: number
  background_color?: string
  background_alpha?: number
}

export interface Timeline {
  version: number
  width: number
  height: number
  fps: number
  caption_style: CaptionStyle
  tracks: Track[]
}

export interface ExportSettings {
  width: number
  height: number
  fps: number
  encoder: string
  quality: number
  filename: string
}

export interface ExportJob {
  id: string
  project_id: string
  settings: ExportSettings
  status: 'running' | 'completed' | 'failed'
  progress: number
  message: string
  output_name: string
  error?: string
  started_at: string
  finished_at?: string
}

export interface Project {
  id: string
  name: string
  width: number
  height: number
  fps: number
  timeline: Timeline
  has_transcript: boolean
  created_at: string
  updated_at: string
  last_export?: ExportJob
}

export interface Asset {
  id: string
  project_id: string
  kind: AssetKind
  filename: string
  duration: number
  width: number
  height: number
  fps: number
  has_audio: boolean
  size: number
  created_at: string
}

export interface Font {
  id: string
  family: string
  subfamily: string
  full_name: string
  path: string
  source: 'bundled' | 'system'
}

export interface Transcript {
  job_id: string
  asset_id?: string
  full_text: string
  words: WordTiming[]
  duration: number
  language: string
  mock?: boolean
  created_at: string
}

export interface CutSegment {
  id: string
  start: number
  end: number
  duration: number
  reason: CutReason
  source: string
  text: string
  confidence: number
  needs_review: boolean
  user_overridden: boolean
  is_cut: boolean
}

export interface AnalysisResult {
  transcript: Transcript
  cuts: CutSegment[]
  total_cut_duration: number
}

export interface TaskEvent<T = unknown> {
  kind: 'analyze' | 'export'
  task_id: string
  project_id: string
  status: 'running' | 'completed' | 'failed'
  progress: number
  message?: string
  error?: string
  result?: T
}

export interface Health {
  ok: boolean
  storage_dir: string
  ffmpeg: string
  ffmpeg_ready: boolean
  whisper_ready: boolean
  token_required: boolean
}

export const BLEND_MODES = [
  { value: 'normal', label: 'Normal', css: 'normal' },
  { value: 'multiply', label: 'Multiply', css: 'multiply' },
  { value: 'screen', label: 'Screen', css: 'screen' },
  { value: 'overlay', label: 'Overlay', css: 'overlay' },
  { value: 'softlight', label: 'Soft Light', css: 'soft-light' },
  { value: 'hardlight', label: 'Hard Light', css: 'hard-light' },
  { value: 'addition', label: 'Add', css: 'plus-lighter' },
  { value: 'difference', label: 'Difference', css: 'difference' },
  { value: 'darken', label: 'Darken', css: 'darken' },
  { value: 'lighten', label: 'Lighten', css: 'lighten' },
  { value: 'dodge', label: 'Color Dodge', css: 'color-dodge' },
  { value: 'burn', label: 'Color Burn', css: 'color-burn' },
] as const

export function blendCSS(mode: string): string {
  return BLEND_MODES.find((m) => m.value === mode)?.css ?? 'normal'
}

export function defaultTextStyle(): TextStyle {
  return {
    font_family: 'Arial',
    font_file: '',
    font_size: 84,
    bold: true,
    italic: false,
    color: '#FFFFFF',
    stroke_width: 4,
    stroke_color: '#000000',
    shadow: 3,
    shadow_color: '#000000',
    align: 'center',
    uppercase: false,
    background_color: '#000000',
    background_alpha: 0,
  }
}

export function defaultClip(partial: Partial<Clip> = {}): Clip {
  return {
    id: `clip_${Date.now()}_${Math.random().toString(36).slice(2, 7)}`,
    start: 0,
    duration: 3,
    in: 0,
    out: 3,
    x: 0.5,
    y: 0.5,
    scale: 1,
    rotation: 0,
    opacity: 1,
    blend_mode: 'normal',
    volume: 1,
    fade_in: 0,
    fade_out: 0,
    speed: 1,
    ...partial,
  }
}
