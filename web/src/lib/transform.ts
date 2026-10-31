import type { Clip, TransformKeyframe } from '../types'

export interface TransformValue {
  x: number
  y: number
  scale: number
  rotation: number
}

export const KEYFRAME_EPSILON = 0.03
export const MIN_CLIP_SCALE = 0.05
export const MAX_CLIP_SCALE = 3

export function clampTransform(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value))
}

export function baseTransform(clip: Clip): TransformValue {
  return { x: clip.x, y: clip.y, scale: clip.scale, rotation: clip.rotation }
}

export function sortedKeyframes(clip: Clip): TransformKeyframe[] {
  return [...(clip.keyframes ?? [])].sort((a, b) => a.time - b.time)
}

function frameValue(frame: TransformKeyframe): TransformValue {
  return { x: frame.x, y: frame.y, scale: frame.scale, rotation: frame.rotation }
}

function lerp(a: number, b: number, t: number): number {
  return a + (b - a) * t
}

// Keyframe time is relative to the clip start. Before the first and after the
// last keyframe the transform holds its edge value so a clip never snaps back.
export function transformAt(clip: Clip, timelineTime: number): TransformValue {
  const frames = sortedKeyframes(clip)
  if (frames.length === 0) return baseTransform(clip)
  const local = Math.min(Math.max(timelineTime - clip.start, 0), Math.max(0, clip.duration))
  if (local <= frames[0].time) return frameValue(frames[0])
  const last = frames[frames.length - 1]
  if (local >= last.time) return frameValue(last)
  for (let i = 0; i < frames.length - 1; i++) {
    const from = frames[i]
    const to = frames[i + 1]
    if (local > to.time) continue
    const span = to.time - from.time
    if (span <= 0.0001) return frameValue(to)
    const ratio = (local - from.time) / span
    return {
      x: lerp(from.x, to.x, ratio),
      y: lerp(from.y, to.y, ratio),
      scale: lerp(from.scale, to.scale, ratio),
      rotation: lerp(from.rotation, to.rotation, ratio),
    }
  }
  return frameValue(last)
}

export function keyframeAt(
  clip: Clip,
  timelineTime: number,
  epsilon = KEYFRAME_EPSILON,
): TransformKeyframe | null {
  const local = timelineTime - clip.start
  for (const frame of clip.keyframes ?? []) {
    if (Math.abs(frame.time - local) <= epsilon) return frame
  }
  return null
}

export function hasTransformAnimation(clip: Clip): boolean {
  return (clip.keyframes?.length ?? 0) > 0
}

// Inserting at an existing keyframe time replaces it. The list stays sorted and
// keyframe times are clamped inside the clip.
export function upsertTransformKeyframe(
  clip: Clip,
  timelineTime: number,
  value: TransformValue,
  epsilon = KEYFRAME_EPSILON,
): TransformKeyframe[] {
  const local = Math.min(Math.max(timelineTime - clip.start, 0), Math.max(0, clip.duration))
  const frames = (clip.keyframes ?? []).filter((frame) => Math.abs(frame.time - local) > epsilon)
  frames.push({
    time: Number(local.toFixed(3)),
    x: value.x,
    y: value.y,
    scale: value.scale,
    rotation: value.rotation,
  })
  return frames.sort((a, b) => a.time - b.time)
}

export function removeTransformKeyframe(
  clip: Clip,
  timelineTime: number,
  epsilon = KEYFRAME_EPSILON,
): TransformKeyframe[] {
  const local = timelineTime - clip.start
  return (clip.keyframes ?? []).filter((frame) => Math.abs(frame.time - local) > epsilon)
}

export function isDefaultTransform(value: TransformValue): boolean {
  return (
    Math.abs(value.x - 0.5) < 0.0005 &&
    Math.abs(value.y - 0.5) < 0.0005 &&
    Math.abs(value.scale - 1) < 0.0005 &&
    Math.abs(value.rotation) < 0.0005
  )
}
