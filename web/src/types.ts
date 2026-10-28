export type TrackKind = 'video' | 'audio' | 'overlay' | 'caption'
export type AssetKind = 'video' | 'audio' | 'image'
export type CutReason = 'silence' | 'filler' | 'retake' | 'off_topic' | 'keep'

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

export interface Clip {
  id: string
  asset_id?: string
  start: number
  duration: number
  in: number
  out: number
  x: number
  y: number
  scale: number
  rotation: number
  opacity: number
  blend_mode: string
  text?: string
  style?: TextStyle | null
  volume: number
  fade_in: number
  fade_out: number
  words?: WordTiming[]
}

export interface Track {
  id: string
  kind: TrackKind
  name: string
  muted: boolean
  locked: boolean
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
  family: string
  subfamily: string
  full_name: string
  path: string
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
    ...partial,
  }
}
