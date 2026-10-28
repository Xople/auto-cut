<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Captions, Film, Layers, Music, Sparkles, Trash2, Wand2 } from 'lucide-vue-next'
import { useEditor } from '../stores/editor'
import { BLEND_MODES, defaultTextStyle } from '../types'
import { formatSecondsShort } from '../lib/format'

const emit = defineEmits<{
  (e: 'open-autocut'): void
  (e: 'generate-captions'): void
}>()

const editor = useEditor()
const tab = ref<'clip' | 'caption'>('clip')

const selected = computed(() => editor.selected)
const clip = computed(() => selected.value?.clip ?? null)
const track = computed(() => selected.value?.track ?? null)

watch(
  () => selected.value?.track.id,
  () => {
    if (track.value?.kind === 'caption') tab.value = 'caption'
  },
)

const asset = computed(() => (clip.value?.asset_id ? editor.assetMap.get(clip.value.asset_id) : undefined))
const isText = computed(() => !!clip.value && clip.value.text !== undefined)
const isCaption = computed(() => track.value?.kind === 'caption')

function num(event: Event): number {
  return Number((event.target as HTMLInputElement).value)
}

function update(patch: Record<string, unknown>, key = '') {
  if (!selected.value) return
  editor.updateClip(selected.value.track.id, selected.value.clip.id, patch as never, key ? `${key}-${clip.value?.id}` : '')
}

function updateStyle(patch: Record<string, unknown>, key = 'text') {
  if (!selected.value || !clip.value) return
  const style = { ...defaultTextStyle(), ...(clip.value.style ?? {}), ...patch }
  editor.updateClip(
    selected.value.track.id,
    selected.value.clip.id,
    { style } as never,
    `${key}-${clip.value.id}`,
  )
}

function updateCaption(patch: Record<string, unknown>, key = 'cap') {
  editor.updateCaptionStyle(patch as never, `${key}-caption`)
}

function trackIcon() {
  switch (track.value?.kind) {
    case 'audio':
      return Music
    case 'overlay':
      return Layers
    case 'caption':
      return Captions
    default:
      return Film
  }
}
</script>

<template>
  <aside class="props-panel">
    <div class="props-tabs">
      <button class="props-tab" :class="{ active: tab === 'clip' }" @click="tab = 'clip'">Properti</button>
      <button class="props-tab" :class="{ active: tab === 'caption' }" @click="tab = 'caption'">Caption</button>
    </div>

    <div class="props-body">
      <template v-if="tab === 'clip'">
        <div v-if="!selected" class="props-empty">
          <div class="empty-icon"><Layers :size="20" /></div>
          <h3>Belum ada clip terpilih</h3>
          <p>Klik clip di timeline untuk mengatur posisi, skala, blend mode, audio, dan teks.</p>

          <div class="project-card">
            <div class="section-title" style="margin-bottom: 8px">Project</div>
            <div class="kv"><span>Resolusi</span><strong>{{ editor.project?.width }}×{{ editor.project?.height }}</strong></div>
            <div class="kv"><span>Frame rate</span><strong>{{ Math.round(editor.project?.fps ?? 30) }} fps</strong></div>
            <div class="kv"><span>Durasi</span><strong>{{ formatSecondsShort(editor.duration) }}</strong></div>
            <div class="kv"><span>Media</span><strong>{{ editor.assets.length }} asset</strong></div>
          </div>

          <div class="shortcut-list">
            <div class="section-title" style="margin-bottom: 8px">Shortcut</div>
            <div class="kv"><span>Play / pause</span><span class="kbd">Space</span></div>
            <div class="kv"><span>Split clip</span><span class="kbd">Ctrl B</span></div>
            <div class="kv"><span>Hapus clip</span><span class="kbd">Del</span></div>
            <div class="kv"><span>Undo / redo</span><span class="kbd">Ctrl Z / Y</span></div>
          </div>

          <button class="btn full" style="margin-top: 14px" @click="emit('open-autocut')">
            <Wand2 :size="14" />
            Analisis voice-over
          </button>
        </div>

        <template v-else-if="clip && track">
          <div class="clip-head">
            <div class="clip-kind" :class="track.kind">
              <component :is="trackIcon()" :size="16" />
            </div>
            <div class="clip-title">
              <div class="name">{{ clip.text?.slice(0, 40) || asset?.filename || 'Clip' }}</div>
              <div class="meta">
                {{ track.name }} · {{ clip.duration.toFixed(2) }}s
                <span v-if="asset"> · {{ asset.width }}×{{ asset.height }}</span>
              </div>
            </div>
          </div>

          <div class="clip-actions">
            <button class="btn small" @click="editor.splitAtPlayhead()">Split</button>
            <button class="btn small btn-danger" @click="editor.deleteSelected()">
              <Trash2 :size="13" />
              Hapus
            </button>
          </div>

          <div v-if="isCaption" class="notice">
            Caption memakai style global project. Atur font, stroke, highlight, dan blend di tab
            <button class="link" @click="tab = 'caption'">Caption</button>.
          </div>

          <template v-if="track.kind === 'overlay'">
            <div class="section-title">Transform</div>
            <div class="field">
              <div class="field-label"><span>Posisi X</span><span class="value">{{ (clip.x * 100).toFixed(0) }}%</span></div>
              <input type="range" min="0" max="1" step="0.005" :value="clip.x" @input="update({ x: num($event) }, 'x')" />
            </div>
            <div class="field">
              <div class="field-label"><span>Posisi Y</span><span class="value">{{ (clip.y * 100).toFixed(0) }}%</span></div>
              <input type="range" min="0" max="1" step="0.005" :value="clip.y" @input="update({ y: num($event) }, 'y')" />
            </div>
            <div class="grid-2">
              <div class="field">
                <div class="field-label"><span>Scale</span><span class="value">{{ clip.scale.toFixed(2) }}×</span></div>
                <input type="range" min="0.05" max="3" step="0.01" :value="clip.scale" @input="update({ scale: num($event) }, 'scale')" />
              </div>
              <div class="field">
                <div class="field-label"><span>Rotasi</span><span class="value">{{ clip.rotation.toFixed(0) }}°</span></div>
                <input type="range" min="-180" max="180" step="1" :value="clip.rotation" @input="update({ rotation: num($event) }, 'rot')" />
              </div>
            </div>
            <div class="field">
              <div class="field-label"><span>Opacity</span><span class="value">{{ Math.round(clip.opacity * 100) }}%</span></div>
              <input type="range" min="0" max="1" step="0.01" :value="clip.opacity" @input="update({ opacity: num($event) }, 'op')" />
            </div>
            <div class="field">
              <label class="field-label">Blend mode</label>
              <select
                class="select"
                :value="clip.blend_mode"
                @change="update({ blend_mode: ($event.target as HTMLSelectElement).value }, 'blend')"
              >
                <option v-for="mode in BLEND_MODES" :key="mode.value" :value="mode.value">{{ mode.label }}</option>
              </select>
            </div>

            <template v-if="isText">
              <div class="divider" />
              <div class="section-title">Teks</div>
              <div class="field">
                <textarea class="input" :value="clip.text" @input="update({ text: ($event.target as HTMLTextAreaElement).value }, 'text')" />
              </div>
              <div class="field">
                <label class="field-label">Font</label>
                <input
                  class="input"
                  list="font-families"
                  :value="clip.style?.font_family ?? 'Arial'"
                  @change="updateStyle({ font_family: ($event.target as HTMLInputElement).value }, 'font')"
                />
              </div>
              <div class="grid-2">
                <div class="field">
                  <div class="field-label"><span>Ukuran</span><span class="value">{{ clip.style?.font_size ?? 84 }}</span></div>
                  <input
                    type="range"
                    min="16"
                    max="220"
                    step="1"
                    :value="clip.style?.font_size ?? 84"
                    @input="updateStyle({ font_size: num($event) }, 'size')"
                  />
                </div>
                <div class="field">
                  <div class="field-label">Warna</div>
                  <div class="color-row">
                    <input type="color" :value="clip.style?.color ?? '#FFFFFF'" @input="updateStyle({ color: ($event.target as HTMLInputElement).value }, 'color')" />
                    <button class="chip" :class="{ active: clip.style?.bold }" @click="updateStyle({ bold: !clip.style?.bold })">B</button>
                    <button class="chip italic" :class="{ active: clip.style?.italic }" @click="updateStyle({ italic: !clip.style?.italic })">I</button>
                  </div>
                </div>
              </div>
              <div class="grid-2">
                <div class="field">
                  <div class="field-label"><span>Stroke</span><span class="value">{{ (clip.style?.stroke_width ?? 4).toFixed(0) }}</span></div>
                  <input
                    type="range"
                    min="0"
                    max="16"
                    step="0.5"
                    :value="clip.style?.stroke_width ?? 4"
                    @input="updateStyle({ stroke_width: num($event) }, 'stroke')"
                  />
                </div>
                <div class="field">
                  <div class="field-label">Warna stroke</div>
                  <input type="color" :value="clip.style?.stroke_color ?? '#000000'" @input="updateStyle({ stroke_color: ($event.target as HTMLInputElement).value }, 'strokecolor')" />
                </div>
              </div>
              <div class="grid-2">
                <div class="field">
                  <div class="field-label"><span>Shadow</span><span class="value">{{ (clip.style?.shadow ?? 3).toFixed(0) }}</span></div>
                  <input
                    type="range"
                    min="0"
                    max="14"
                    step="0.5"
                    :value="clip.style?.shadow ?? 3"
                    @input="updateStyle({ shadow: num($event) }, 'shadow')"
                  />
                </div>
                <div class="field">
                  <div class="field-label">Warna shadow</div>
                  <input type="color" :value="clip.style?.shadow_color ?? '#000000'" @input="updateStyle({ shadow_color: ($event.target as HTMLInputElement).value }, 'shadowcolor')" />
                </div>
              </div>
              <div class="grid-2">
                <div class="field">
                  <div class="field-label">Background</div>
                  <input type="color" :value="clip.style?.background_color ?? '#000000'" @input="updateStyle({ background_color: ($event.target as HTMLInputElement).value }, 'bg')" />
                </div>
                <div class="field">
                  <div class="field-label"><span>Alpha BG</span><span class="value">{{ Math.round((clip.style?.background_alpha ?? 0) * 100) }}%</span></div>
                  <input
                    type="range"
                    min="0"
                    max="1"
                    step="0.01"
                    :value="clip.style?.background_alpha ?? 0"
                    @input="updateStyle({ background_alpha: num($event) }, 'bgalpha')"
                  />
                </div>
              </div>
              <label class="toggle">
                <input type="checkbox" :checked="clip.style?.uppercase" @change="updateStyle({ uppercase: ($event.target as HTMLInputElement).checked }, 'upper')" />
                Uppercase
              </label>
            </template>
          </template>

          <template v-else-if="track.kind !== 'caption'">
            <div class="section-title">Audio</div>
            <div class="field">
              <div class="field-label"><span>Volume</span><span class="value">{{ Math.round(clip.volume * 100) }}%</span></div>
              <input type="range" min="0" max="2" step="0.01" :value="clip.volume" @input="update({ volume: num($event) }, 'vol')" />
            </div>
            <div class="grid-2">
              <div class="field">
                <div class="field-label"><span>Fade in</span><span class="value">{{ clip.fade_in.toFixed(2) }}s</span></div>
                <input type="range" min="0" max="5" step="0.05" :value="clip.fade_in" @input="update({ fade_in: num($event) }, 'fi')" />
              </div>
              <div class="field">
                <div class="field-label"><span>Fade out</span><span class="value">{{ clip.fade_out.toFixed(2) }}s</span></div>
                <input type="range" min="0" max="5" step="0.05" :value="clip.fade_out" @input="update({ fade_out: num($event) }, 'fo')" />
              </div>
            </div>

            <div class="divider" />
            <div class="section-title">Timing</div>
            <div class="kv"><span>Mulai</span><strong>{{ clip.start.toFixed(2) }}s</strong></div>
            <div class="kv"><span>Durasi</span><strong>{{ clip.duration.toFixed(2) }}s</strong></div>
            <div class="kv"><span>Source in</span><strong>{{ clip.in.toFixed(2) }}s</strong></div>
            <div class="kv"><span>Source out</span><strong>{{ clip.out.toFixed(2) }}s</strong></div>

            <div v-if="asset" class="asset-note">
              {{ asset.filename }} · {{ formatSecondsShort(asset.duration) }}
              <span v-if="asset.has_audio" class="badge teal">audio</span>
            </div>
          </template>

          <div class="divider" />
          <div class="section-title">Track</div>
          <label class="toggle" style="margin-bottom: 6px">
            <input
              type="checkbox"
              :checked="track.muted"
              @change="editor.setTrackMuted(track.id, ($event.target as HTMLInputElement).checked)"
            />
            Mute track
          </label>
          <label class="toggle">
            <input
              type="checkbox"
              :checked="track.locked"
              @change="editor.setTrackLocked(track.id, ($event.target as HTMLInputElement).checked)"
            />
            Lock track
          </label>
        </template>
      </template>

      <template v-else>
        <div class="section-title">
          <span>Caption Style</span>
          <span class="badge accent">global</span>
        </div>

        <div class="field">
          <label class="field-label">Font</label>
          <input
            class="input"
            list="font-families"
            :value="editor.project?.timeline.caption_style.font_family"
            @change="updateCaption({ font_family: ($event.target as HTMLInputElement).value }, 'font')"
          />
        </div>

        <div class="grid-2">
          <div class="field">
            <div class="field-label"><span>Ukuran</span><span class="value">{{ editor.project?.timeline.caption_style.font_size }}</span></div>
            <input
              type="range"
              min="24"
              max="180"
              step="1"
              :value="editor.project?.timeline.caption_style.font_size"
              @input="updateCaption({ font_size: num($event) }, 'size')"
            />
          </div>
          <div class="field">
            <div class="field-label">Warna</div>
            <div class="color-row">
              <input
                type="color"
                :value="editor.project?.timeline.caption_style.color"
                @input="updateCaption({ color: ($event.target as HTMLInputElement).value }, 'color')"
              />
              <button
                class="chip"
                :class="{ active: editor.project?.timeline.caption_style.bold }"
                @click="updateCaption({ bold: !editor.project?.timeline.caption_style.bold }, 'bold')"
              >
                B
              </button>
              <button
                class="chip italic"
                :class="{ active: editor.project?.timeline.caption_style.italic }"
                @click="updateCaption({ italic: !editor.project?.timeline.caption_style.italic }, 'italic')"
              >
                I
              </button>
            </div>
          </div>
        </div>

        <div class="grid-2">
          <div class="field">
            <div class="field-label"><span>Stroke</span><span class="value">{{ (editor.project?.timeline.caption_style.stroke_width ?? 0).toFixed(1) }}</span></div>
            <input
              type="range"
              min="0"
              max="16"
              step="0.5"
              :value="editor.project?.timeline.caption_style.stroke_width"
              @input="updateCaption({ stroke_width: num($event) }, 'stroke')"
            />
          </div>
          <div class="field">
            <div class="field-label">Warna stroke</div>
            <input
              type="color"
              :value="editor.project?.timeline.caption_style.stroke_color"
              @input="updateCaption({ stroke_color: ($event.target as HTMLInputElement).value }, 'strokecolor')"
            />
          </div>
        </div>

        <div class="grid-2">
          <div class="field">
            <div class="field-label"><span>Shadow</span><span class="value">{{ (editor.project?.timeline.caption_style.shadow ?? 0).toFixed(1) }}</span></div>
            <input
              type="range"
              min="0"
              max="14"
              step="0.5"
              :value="editor.project?.timeline.caption_style.shadow"
              @input="updateCaption({ shadow: num($event) }, 'shadow')"
            />
          </div>
          <div class="field">
            <div class="field-label">Warna shadow</div>
            <input
              type="color"
              :value="editor.project?.timeline.caption_style.shadow_color"
              @input="updateCaption({ shadow_color: ($event.target as HTMLInputElement).value }, 'shadowcolor')"
            />
          </div>
        </div>

        <div class="field">
          <div class="field-label"><span>Posisi vertikal</span><span class="value">{{ Math.round((editor.project?.timeline.caption_style.position_y ?? 0.82) * 100) }}%</span></div>
          <input
            type="range"
            min="0.2"
            max="0.97"
            step="0.005"
            :value="editor.project?.timeline.caption_style.position_y"
            @input="updateCaption({ position_y: num($event) }, 'posy')"
          />
        </div>

        <div class="field">
          <div class="field-label"><span>Opacity</span><span class="value">{{ Math.round((editor.project?.timeline.caption_style.opacity ?? 1) * 100) }}%</span></div>
          <input
            type="range"
            min="0"
            max="1"
            step="0.01"
            :value="editor.project?.timeline.caption_style.opacity"
            @input="updateCaption({ opacity: num($event) }, 'opacity')"
          />
        </div>

        <div class="divider" />
        <div class="section-title">Highlight per kata</div>
        <label class="toggle" style="margin-bottom: 10px">
          <input
            type="checkbox"
            :checked="editor.project?.timeline.caption_style.highlight"
            @change="updateCaption({ highlight: ($event.target as HTMLInputElement).checked }, 'highlight')"
          />
          Aktifkan karaoke highlight
        </label>
        <div class="grid-2">
          <div class="field">
            <div class="field-label">Warna highlight</div>
            <input
              type="color"
              :value="editor.project?.timeline.caption_style.highlight_color"
              @input="updateCaption({ highlight_color: ($event.target as HTMLInputElement).value }, 'hcolor')"
            />
          </div>
          <div class="field">
            <div class="field-label"><span>Kata / baris</span><span class="value">{{ editor.project?.timeline.caption_style.words_per_chunk }}</span></div>
            <input
              type="range"
              min="1"
              max="8"
              step="1"
              :value="editor.project?.timeline.caption_style.words_per_chunk"
              @input="updateCaption({ words_per_chunk: num($event) }, 'wpc')"
            />
          </div>
        </div>

        <div class="field">
          <label class="field-label">Blend mode caption</label>
          <select
            class="select"
            :value="editor.project?.timeline.caption_style.blend_mode"
            @change="updateCaption({ blend_mode: ($event.target as HTMLSelectElement).value }, 'blend')"
          >
            <option v-for="mode in BLEND_MODES" :key="mode.value" :value="mode.value">{{ mode.label }}</option>
          </select>
        </div>

        <label class="toggle" style="margin-bottom: 12px">
          <input
            type="checkbox"
            :checked="editor.project?.timeline.caption_style.uppercase"
            @change="updateCaption({ uppercase: ($event.target as HTMLInputElement).checked }, 'upper')"
          />
          Uppercase semua caption
        </label>

        <div class="warning-box" style="margin-bottom: 12px">
          <span>Blend mode caption hanya berlaku di preview. Export caption memakai ASS (tanpa blend).</span>
        </div>

        <button class="btn btn-primary full" @click="emit('generate-captions')">
          <Sparkles :size="14" />
          Generate caption dari transkrip
        </button>
        <button class="btn full" style="margin-top: 8px" @click="emit('open-autocut')">
          <Wand2 :size="14" />
          Analisis voice-over
        </button>
      </template>
    </div>

    <datalist id="font-families">
      <option v-for="font in editor.fonts" :key="font.path + font.subfamily" :value="font.family">
        {{ font.full_name }}
      </option>
    </datalist>
  </aside>
</template>

<style scoped>
.props-panel {
  display: flex;
  flex-direction: column;
  min-height: 0;
  background: var(--panel);
  border-left: 1px solid var(--border-soft);
}

.props-tabs {
  height: 46px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 2px;
  padding: 0 10px;
  border-bottom: 1px solid var(--border-soft);
}

.props-tab {
  height: 28px;
  padding: 0 14px;
  border: none;
  border-radius: 7px;
  background: transparent;
  color: var(--muted);
  font-size: 11.5px;
  font-weight: 600;
}

.props-tab.active {
  background: var(--panel-3);
  color: var(--text);
}

.props-body {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 14px;
}

.props-empty {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.props-empty h3 {
  margin: 0;
  font-size: 13px;
}

.props-empty p {
  margin: 0;
  color: var(--muted-2);
  font-size: 11.5px;
  line-height: 1.55;
}

.empty-icon {
  width: 40px;
  height: 40px;
  border-radius: 11px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--accent-strong);
  background: var(--accent-soft);
}

.project-card,
.shortcut-list {
  padding: 11px;
  border-radius: 10px;
  border: 1px solid var(--border-soft);
  background: #0c0c11;
}

.kv {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 3px 0;
  font-size: 11.5px;
  color: var(--muted);
}

.kv strong {
  color: var(--text);
  font-weight: 600;
}

.clip-head {
  display: flex;
  gap: 10px;
  align-items: center;
  margin-bottom: 11px;
}

.clip-kind {
  width: 34px;
  height: 34px;
  border-radius: 9px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--panel-3);
  color: var(--muted);
}

.clip-kind.video {
  color: #8c9cff;
}

.clip-kind.audio {
  color: var(--teal);
}

.clip-kind.overlay {
  color: var(--amber);
}

.clip-kind.caption {
  color: #d99cff;
}

.clip-title {
  min-width: 0;
}

.clip-title .name {
  font-size: 12.5px;
  font-weight: 650;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.clip-title .meta {
  margin-top: 2px;
  font-size: 10px;
  color: var(--muted-2);
}

.clip-actions {
  display: flex;
  gap: 7px;
  margin-bottom: 14px;
}

.btn.small {
  height: 27px;
  padding: 0 10px;
  font-size: 11px;
}

.notice {
  padding: 10px 11px;
  border-radius: 9px;
  border: 1px solid rgba(124, 108, 255, 0.25);
  background: rgba(124, 108, 255, 0.08);
  font-size: 11.5px;
  line-height: 1.55;
  color: #c5c0ff;
  margin-bottom: 12px;
}

.link {
  border: none;
  background: transparent;
  color: var(--accent-strong);
  padding: 0;
  text-decoration: underline;
  cursor: pointer;
}

.color-row {
  display: flex;
  align-items: center;
  gap: 6px;
}

.chip {
  width: 27px;
  height: 27px;
  border-radius: 7px;
  border: 1px solid var(--border);
  background: #0c0c11;
  color: var(--muted);
  font-weight: 700;
  font-size: 12px;
}

.chip.italic {
  font-style: italic;
}

.chip.active {
  border-color: var(--accent);
  background: var(--accent-soft);
  color: var(--text);
}

.asset-note {
  margin-top: 10px;
  padding: 8px 10px;
  border-radius: 8px;
  background: #0c0c11;
  border: 1px solid var(--border-soft);
  font-size: 10.5px;
  color: var(--muted);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  overflow: hidden;
}

.full {
  width: 100%;
}
</style>
