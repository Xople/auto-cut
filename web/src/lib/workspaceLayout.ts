import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

export const layouts = [
  { id: 'default', label: 'Default', detail: 'Preview di tengah' },
  { id: 'media', label: 'Media', detail: 'Pustaka setinggi editor' },
  { id: 'attributes', label: 'Atribut', detail: 'Properti setinggi editor' },
  { id: 'vertical', label: 'Vertikal', detail: 'Preview penuh di kanan' },
  { id: 'vertical-left', label: 'Vertikal kiri', detail: 'Preview penuh di kiri' },
] as const

export type Layout = typeof layouts[number]['id']
type Dimension = 'properties' | 'timeline'
type Sizes = Partial<Record<Dimension, number>>
type Preferences = { layout: Layout; sizes: Partial<Record<Layout, Sizes>> }
const storageKey = 'autocut.workspace-layout.v1'

function readPreferences(): Preferences {
  const prefs: Preferences = { layout: 'default', sizes: {} }
  try {
    const saved = JSON.parse(localStorage.getItem(storageKey) ?? '{}')
    for (const { id } of layouts) {
      if (id === saved?.layout) prefs.layout = id
      const size = saved?.sizes?.[id]
      if (!size) continue
      const valid: Sizes = {}
      if (Number.isFinite(size.properties)) valid.properties = size.properties
      if (Number.isFinite(size.timeline)) valid.timeline = size.timeline
      prefs.sizes[id] = valid
    }
  } catch { /* Storage can be disabled in browser mode. */ }
  return prefs
}

function clamp(value: number, min: number, max: number) {
  return Math.round(Math.min(max, Math.max(min, value)))
}

export function useWorkspaceLayout() {
  const workspace = ref<HTMLElement | null>(null)
  const prefs = ref(readPreferences())
  const layout = computed(() => prefs.value.layout)
  const portrait = computed(() => layout.value === 'vertical' || layout.value === 'vertical-left')
  const width = ref(window.innerWidth)
  const height = ref(window.innerHeight - 56)
  const divider = 6
  const mediaWidth = computed(() => clamp(width.value * 0.25, 254, 370))
  const portraitWidth = computed(() => Math.round(width.value / 3))
  const bounds = computed(() => ({
    properties: {
      min: 240,
      max: Math.max(240, width.value - divider - (portrait.value ? portraitWidth.value + 240 : mediaWidth.value + 335)),
      initial: portrait.value ? (width.value - portraitWidth.value - divider) / 2 : clamp(width.value * 0.22, 260, 320),
    },
    timeline: {
      min: 150,
      max: Math.max(150, height.value - divider - 210),
      initial: portrait.value ? height.value * 0.45 : clamp((height.value + 56) * 0.29, 190, 310),
    },
  }))
  function size(axis: Dimension) {
    const { min, max, initial } = bounds.value[axis]
    return clamp(prefs.value.sizes[layout.value]?.[axis] ?? initial, min, max)
  }
  const gridStyle = computed(() => ({
    '--media-width': mediaWidth.value + 'px',
    '--portrait-width': portraitWidth.value + 'px',
    '--props-width': size('properties') + 'px',
    '--timeline-height': size('timeline') + 'px',
    '--divider': divider + 'px',
  }))

  function save() {
    try { localStorage.setItem(storageKey, JSON.stringify(prefs.value)) } catch { /* Keep the session layout. */ }
  }
  function chooseLayout(value: Layout) {
    prefs.value.layout = value
    save()
  }
  function resetLayout() {
    delete prefs.value.sizes[layout.value]
    save()
  }
  function resize(axis: Dimension, value: number) {
    const { min, max } = bounds.value[axis]
    const sizes = prefs.value.sizes[layout.value] ??= {}
    sizes[axis] = clamp(value, min, max)
  }

  const resizing = ref<Dimension | null>(null)
  let drag: { id: number; start: number; size: number; axis: Dimension } | null = null
  function beginResize(event: PointerEvent, axis: Dimension) {
    if (event.button !== 0 || drag) return
    const handle = event.currentTarget as HTMLElement
    handle.focus({ preventScroll: true })
    handle.setPointerCapture(event.pointerId)
    drag = { id: event.pointerId, start: axis === 'properties' ? event.clientX : event.clientY, size: size(axis), axis }
    resizing.value = axis
  }
  function moveResize(event: PointerEvent) {
    if (!drag || event.pointerId !== drag.id) return
    const position = drag.axis === 'properties' ? event.clientX : event.clientY
    resize(drag.axis, drag.size + drag.start - position)
  }
  function endResize(event: PointerEvent) {
    if (!drag || event.pointerId !== drag.id) return
    drag = null
    resizing.value = null
    save()
  }
  function keyResize(event: KeyboardEvent, axis: Dimension) {
    const increase = axis === 'properties' ? 'ArrowLeft' : 'ArrowUp'
    const decrease = axis === 'properties' ? 'ArrowRight' : 'ArrowDown'
    if (![increase, decrease, 'Home', 'End'].includes(event.key)) return
    event.preventDefault()
    event.stopPropagation()
    const { min, max } = bounds.value[axis]
    const step = event.shiftKey ? 40 : 10
    const value = event.key === 'Home' ? min : event.key === 'End' ? max : size(axis) + (event.key === increase ? step : -step)
    resize(axis, value)
    save()
  }

  let observer: ResizeObserver | undefined
  onMounted(() => {
    if (!workspace.value) return
    observer = new ResizeObserver(([entry]) => {
      width.value = entry.contentRect.width
      height.value = entry.contentRect.height
    })
    observer.observe(workspace.value)
  })
  onBeforeUnmount(() => observer?.disconnect())

  return { workspace, layout, bounds, gridStyle, size, resizing, chooseLayout, resetLayout, beginResize, moveResize, endResize, keyResize }
}
