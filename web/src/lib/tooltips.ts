import { onBeforeUnmount, onMounted } from 'vue'

export function useTooltips() {
  const tip = document.createElement('div')
  tip.className = 'app-tooltip'
  tip.id = 'autocut-tooltip'
  tip.setAttribute('role', 'tooltip')
  tip.popover = 'manual'
  const label = document.createElement('span')
  const keys = document.createElement('kbd')
  tip.append(label, keys)

  let anchor: HTMLElement | null = null
  let timer = 0
  const observer = new ResizeObserver(() => { if (tip.isConnected) position() })
  const changes = new MutationObserver(() => { if (tip.isConnected) show() })

  function hide() {
    window.clearTimeout(timer)
    if (anchor) {
      const ids = (anchor.getAttribute('aria-describedby') || '').split(/\s+/).filter(id => id && id !== tip.id)
      if (ids.length) anchor.setAttribute('aria-describedby', ids.join(' '))
      else anchor.removeAttribute('aria-describedby')
    }
    observer.disconnect()
    changes.disconnect()
    tip.remove()
    anchor = null
  }

  function position() {
    if (!anchor?.isConnected || !anchor.getClientRects().length) return hide()
    const r = anchor.getBoundingClientRect()
    const gap = 8
    const left = r.left + (r.width - tip.offsetWidth) / 2
    const above = r.top - tip.offsetHeight - gap
    tip.style.left = Math.max(gap, Math.min(left, innerWidth - tip.offsetWidth - gap)) + 'px'
    tip.style.top = Math.max(gap, above >= gap ? above : Math.min(r.bottom + gap, innerHeight - tip.offsetHeight - gap)) + 'px'
  }

  function show() {
    if (!anchor?.isConnected) return hide()
    const text = anchor.dataset.tooltip || anchor.getAttribute('aria-label')
    if (!text) return hide()
    const shortcut = /\s*\(((?:Ctrl|Shift|Alt)[^()]*|Del|Space|Esc)\)$/i.exec(text)
    label.textContent = shortcut ? text.slice(0, shortcut.index) : text
    keys.textContent = shortcut?.[1] || ''
    keys.hidden = !shortcut
    if (!tip.isConnected) {
      const host = anchor.closest('dialog[open]') || document.fullscreenElement || document.body
      host.append(tip)
      tip.showPopover()
      const ids = [anchor.getAttribute('aria-describedby'), tip.id].filter(Boolean)
      anchor.setAttribute('aria-describedby', ids.join(' '))
      observer.observe(anchor)
      changes.observe(anchor, { attributes: true, attributeFilter: ['data-tooltip', 'aria-label'] })
    }
    position()
  }

  function find(target: EventTarget | null) {
    return target instanceof Element ? target.closest<HTMLElement>('[data-tooltip]') : null
  }

  function enter(e: PointerEvent | FocusEvent) {
    if (e instanceof PointerEvent && (e.pointerType === 'touch' || e.buttons)) return
    const next = find(e.target)
    if (!next || next === anchor) return
    hide()
    anchor = next
    timer = window.setTimeout(show, e.type === 'focusin' ? 80 : 350)
  }

  function leave(e: PointerEvent | FocusEvent) {
    if (anchor && find(e.target) === anchor && find(e.relatedTarget) !== anchor) hide()
  }

  function escape(e: KeyboardEvent) {
    if (e.key !== 'Escape' || !tip.isConnected) return
    e.preventDefault()
    e.stopImmediatePropagation()
    hide()
  }

  const events = new AbortController()
  onMounted(() => {
    const options = { capture: true, signal: events.signal }
    document.addEventListener('pointerover', enter, options)
    document.addEventListener('pointerout', leave, options)
    document.addEventListener('focusin', enter, options)
    document.addEventListener('focusout', leave, options)
    document.addEventListener('pointerdown', hide, options)
    document.addEventListener('scroll', hide, options)
    window.addEventListener('resize', hide, options)
    window.addEventListener('blur', hide, options)
    window.addEventListener('keydown', escape, options)
  })
  onBeforeUnmount(() => { events.abort(); hide() })
}
