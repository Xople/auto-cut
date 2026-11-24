import { onBeforeUnmount, shallowRef } from 'vue'
import { useEditor } from '../stores/editor'
import { captionPosition, type Clip } from '../types'
import { MAX_CLIP_SCALE, MIN_CLIP_SCALE, transformAt } from './transform'

export type PreviewPose = { x: number; y: number; scale: number }
type DragMode = 'move' | 'scale'
const CENTER_SNAP_PX = 6

function dragPosition(start: number, delta: number, length: number, snap: boolean) {
  const value = start + delta / length
  if (snap && Math.abs(value - 0.5) * length <= CENTER_SNAP_PX) return 0.5
  return Math.min(1, Math.max(0, value))
}

export function usePreviewDrag() {
  const editor = useEditor()
  const draft = shallowRef<(PreviewPose & { clipId: string; mode: DragMode }) | null>(null)
  let cancel: (() => void) | undefined

  function begin(event: PointerEvent, trackId: string, clip: Clip, mode: DragMode = 'move', target = event.currentTarget as HTMLElement) {
    if (cancel || !event.isPrimary || event.button !== 0) return
    const stage = target.closest('.stage')
    if (!stage || !editor.project) return
    event.stopPropagation()
    event.preventDefault()
    editor.pause()
    editor.select(trackId, clip.id)
    const track = editor.selectedTrack
    if (!track || track.locked) return

    const rect = stage.getBoundingClientRect()
    if (!rect.width || !rect.height) return
    const caption = track.kind === 'caption'
    const video = track.kind === 'video'
    const time = editor.playhead
    const transform = video ? transformAt(clip, time) : clip
    const { x, y } = caption ? captionPosition(clip, editor.project.timeline.caption_style) : transform
    const scale = transform.scale || 1
    const cx = rect.left + x * rect.width, cy = rect.top + y * rect.height
    const radius = Math.max(1, Math.hypot(event.clientX - cx, event.clientY - cy))

    function move(e: PointerEvent) {
      if (e.pointerId !== event.pointerId) return
      const dx = e.clientX - event.clientX, dy = e.clientY - event.clientY
      if (!draft.value && Math.hypot(dx, dy) < 3) return
      if (mode === 'move') {
        draft.value = {
          clipId: clip.id, mode, scale,
          x: dragPosition(x, dx, rect.width, !e.altKey),
          y: dragPosition(y, dy, rect.height, !e.altKey),
        }
      } else {
        const distance = Math.hypot(e.clientX - cx, e.clientY - cy)
        const minScale = video ? MIN_CLIP_SCALE : 0.1
        draft.value = { clipId: clip.id, mode, x, y, scale: Math.min(MAX_CLIP_SCALE, Math.max(minScale, scale * distance / radius)) }
      }
    }

    function finish(e: PointerEvent) {
      if (e.pointerId !== event.pointerId) return
      if (e.type === 'pointerup' && editor.playhead === time && !editor.selectedTrack?.locked && editor.selectedClip === clip) {
        move(e)
        const pose = draft.value
        if (pose && (pose.x !== x || pose.y !== y || pose.scale !== scale)) {
          const point = { x: pose.x, y: pose.y }
          const patch = mode === 'move' ? point : { scale: pose.scale }
          if (video) editor.setClipTransform(trackId, clip.id, patch, '')
          else editor.updateClip(trackId, clip.id, caption ? { caption_position: point } : patch)
        }
      }
      cleanup()
    }

    function cleanup() {
      draft.value = null
      cancel = undefined
      target.removeEventListener('pointermove', move)
      target.removeEventListener('pointerup', finish)
      target.removeEventListener('pointercancel', finish)
      target.removeEventListener('lostpointercapture', finish)
      if (target.hasPointerCapture(event.pointerId)) target.releasePointerCapture(event.pointerId)
    }

    target.setPointerCapture(event.pointerId)
    target.addEventListener('pointermove', move)
    target.addEventListener('pointerup', finish)
    target.addEventListener('pointercancel', finish)
    target.addEventListener('lostpointercapture', finish)
    cancel = cleanup
  }

  onBeforeUnmount(() => cancel?.())
  return { draft, begin }
}
