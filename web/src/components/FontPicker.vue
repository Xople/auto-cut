<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { Check, ChevronDown, Search } from 'lucide-vue-next'
import type { Font } from '../types'

type Entry = { family: string; source: 'bundled' | 'system' }

const props = defineProps<{ modelValue: string; fonts: Font[] }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()

const root = ref<HTMLElement | null>(null)
const searchEl = ref<HTMLInputElement | null>(null)
const menuEl = ref<HTMLElement | null>(null)
const open = ref(false)
const query = ref('')
const active = ref(0)
const rect = ref({ top: 0, left: 0, width: 260, maxHeight: 320 })

const families = computed<Entry[]>(() => {
  const map = new Map<string, Entry>()
  for (const font of props.fonts) {
    const family = (font.family || '').trim()
    if (!family) continue
    const key = family.toLowerCase()
    const source: Entry['source'] = font.source === 'bundled' ? 'bundled' : 'system'
    const existing = map.get(key)
    if (!existing) map.set(key, { family, source })
    else if (source === 'bundled') existing.source = 'bundled'
  }
  return [...map.values()].sort((a, b) => a.family.localeCompare(b.family))
})

const results = computed<Entry[]>(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return families.value
  return families.value.filter((entry) => entry.family.toLowerCase().includes(q))
})

const bundled = computed(() => results.value.filter((entry) => entry.source === 'bundled'))
const system = computed(() => results.value.filter((entry) => entry.source === 'system'))
const current = computed(() => props.modelValue || 'Arial')

function measure() {
  if (!root.value) return
  const r = root.value.getBoundingClientRect()
  const below = window.innerHeight - r.bottom - 16
  const maxHeight = Math.min(320, Math.max(180, below))
  const top = below < 200 ? Math.max(8, r.top - maxHeight - 6) : r.bottom + 6
  rect.value = { top, left: Math.min(r.left, window.innerWidth - Math.max(r.width, 260) - 12), width: Math.max(r.width, 260), maxHeight }
}

// Focusing the search box and scrolling the active option can emit scroll
// events right after opening; ignore those instead of closing instantly.
let openedAt = 0

function openMenu() {
  measure()
  query.value = ''
  active.value = Math.max(0, families.value.findIndex((entry) => entry.family.toLowerCase() === current.value.toLowerCase()))
  open.value = true
  openedAt = performance.now()
  nextTick(() => {
    searchEl.value?.focus({ preventScroll: true })
    menuEl.value?.querySelector('.option.active')?.scrollIntoView({ block: 'nearest' })
  })
}

function closeMenu() {
  open.value = false
  query.value = ''
}

function toggle() {
  if (open.value) closeMenu()
  else openMenu()
}

function choose(entry: Entry) {
  emit('update:modelValue', entry.family)
  closeMenu()
}

function move(step: number) {
  if (!results.value.length) return
  active.value = (active.value + step + results.value.length) % results.value.length
  nextTick(() => menuEl.value?.querySelector('.option.active')?.scrollIntoView({ block: 'nearest' }))
}

function onKeydown(event: KeyboardEvent) {
  if (!open.value) {
    if (event.key === 'Enter' || event.key === ' ' || event.key === 'ArrowDown') {
      event.preventDefault()
      openMenu()
    }
    return
  }
  if (event.key === 'Escape') {
    event.preventDefault()
    closeMenu()
    return
  }
  if (event.key === 'ArrowDown') {
    event.preventDefault()
    move(1)
    return
  }
  if (event.key === 'ArrowUp') {
    event.preventDefault()
    move(-1)
    return
  }
  if (event.key === 'Enter') {
    event.preventDefault()
    const entry = results.value[active.value]
    if (entry) choose(entry)
  }
}

function onSearchInput() {
  active.value = 0
}

function onPointerDown(event: PointerEvent) {
  const target = event.target as Node
  if (root.value?.contains(target) || menuEl.value?.contains(target)) return
  closeMenu()
}

function onViewportChange(event: Event) {
  if (!open.value) return
  if (event.type === 'scroll') {
    if (performance.now() - openedAt < 250) return
    const target = event.target as Node | null
    if (target && menuEl.value?.contains(target)) return
  }
  closeMenu()
}

watch(open, (value) => {
  if (value) {
    window.addEventListener('pointerdown', onPointerDown, true)
    window.addEventListener('resize', onViewportChange)
    window.addEventListener('scroll', onViewportChange, true)
  } else {
    window.removeEventListener('pointerdown', onPointerDown, true)
    window.removeEventListener('resize', onViewportChange)
    window.removeEventListener('scroll', onViewportChange, true)
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('pointerdown', onPointerDown, true)
  window.removeEventListener('resize', onViewportChange)
  window.removeEventListener('scroll', onViewportChange, true)
})
</script>

<template>
  <div ref="root" class="font-picker">
    <button
      type="button"
      class="input font-trigger"
      :aria-expanded="open"
      aria-haspopup="listbox"
      @click="toggle"
      @keydown="onKeydown"
    >
      <span class="value" :style="{ fontFamily: `'${current}', sans-serif` }">{{ current }}</span>
      <ChevronDown :size="14" class="chev" :class="{ open }" />
    </button>

    <Teleport to="body">
      <div
        v-if="open"
        ref="menuEl"
        class="font-menu"
        role="listbox"
        :style="{ top: `${rect.top}px`, left: `${rect.left}px`, width: `${rect.width}px` }"
      >
        <div class="font-search">
          <Search :size="13" />
          <input
            ref="searchEl"
            v-model="query"
            placeholder="Cari font..."
            spellcheck="false"
            @input="onSearchInput"
            @keydown="onKeydown"
          />
        </div>
        <div class="font-list" :style="{ maxHeight: `${rect.maxHeight}px` }">
          <template v-if="bundled.length">
            <div class="group-label">Google Fonts · offline</div>
            <button
              v-for="entry in bundled"
              :key="entry.family"
              type="button"
              class="option"
              :class="{ active: results[active]?.family === entry.family, selected: entry.family.toLowerCase() === current.toLowerCase() }"
              @click="choose(entry)"
            >
              <span class="sample" :style="{ fontFamily: `'${entry.family}', sans-serif` }">{{ entry.family }}</span>
              <Check v-if="entry.family.toLowerCase() === current.toLowerCase()" :size="13" />
            </button>
          </template>
          <template v-if="system.length">
            <div class="group-label">Font sistem</div>
            <button
              v-for="entry in system"
              :key="entry.family"
              type="button"
              class="option"
              :class="{ active: results[active]?.family === entry.family, selected: entry.family.toLowerCase() === current.toLowerCase() }"
              @click="choose(entry)"
            >
              <span class="sample" :style="{ fontFamily: `'${entry.family}', sans-serif` }">{{ entry.family }}</span>
              <Check v-if="entry.family.toLowerCase() === current.toLowerCase()" :size="13" />
            </button>
          </template>
          <div v-if="!results.length" class="empty">Font tidak ditemukan.</div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.font-picker {
  width: 100%;
}

.font-trigger {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  cursor: pointer;
  text-align: left;
}

.font-trigger .value {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.chev {
  flex-shrink: 0;
  color: var(--muted-2);
  transition: transform 0.15s ease;
}

.chev.open {
  transform: rotate(180deg);
}

.font-menu {
  position: fixed;
  z-index: 1200;
  display: flex;
  flex-direction: column;
  background: var(--panel-2);
  border: 1px solid var(--border);
  border-radius: 10px;
  box-shadow: 0 18px 44px rgba(0, 0, 0, 0.6);
  overflow: hidden;
}

.font-search {
  display: flex;
  align-items: center;
  gap: 7px;
  margin: 8px;
  padding: 0 9px;
  height: 30px;
  border-radius: 7px;
  border: 1px solid var(--border);
  background: #0d0d0d;
  color: var(--muted-2);
}

.font-search input {
  flex: 1;
  min-width: 0;
  height: 100%;
  border: 0;
  background: transparent;
  color: var(--text);
  outline: none;
  font-size: 12.5px;
}

.font-list {
  overflow-y: auto;
  padding: 0 6px 6px;
}

.group-label {
  padding: 8px 9px 4px;
  font-size: 9.5px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--muted-2);
}

.option {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  width: 100%;
  padding: 7px 9px;
  border: 0;
  border-radius: 7px;
  background: transparent;
  color: var(--text);
  font-size: 12.5px;
  text-align: left;
  cursor: pointer;
}

.option:hover,
.option.active {
  background: var(--panel-3);
}

.option.selected {
  color: var(--accent);
}

.sample {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 14px;
}

.empty {
  padding: 14px 10px;
  font-size: 12px;
  color: var(--muted-2);
}
</style>
