<script setup lang="ts">
import { ref } from 'vue'
import type { LibraryTool } from '../types'
import { useWorkspaceLayout } from '../lib/workspaceLayout'
import TopBar from './TopBar.vue'
import MediaPanel from './MediaPanel.vue'
import PreviewStage from './PreviewStage.vue'
import PropertiesPanel from './PropertiesPanel.vue'
import TimelinePanel from './TimelinePanel.vue'

const emit = defineEmits<{
  back: []
  'open-autocut': []
  'open-export': []
  'generate-captions': []
}>()
const libraryTool = ref<LibraryTool>('media')
const {
  workspace, layout, bounds, gridStyle, size, resizing, chooseLayout, resetLayout,
  beginResize, moveResize, endResize, keyResize,
} = useWorkspaceLayout()
const dividers = [
  { axis: 'properties', label: 'Lebar panel Properti', orientation: 'vertical', target: 'workspace-properties' },
  { axis: 'timeline', label: 'Tinggi timeline', orientation: 'horizontal', target: 'workspace-timeline' },
] as const
</script>

<template>
  <TopBar :layout="layout" @layout="chooseLayout" @reset-layout="resetLayout"
    @back="emit('back')" @open-autocut="emit('open-autocut')" @open-export="emit('open-export')"
    @generate-captions="emit('generate-captions')" />
  <main ref="workspace" class="workspace" :data-layout="layout" :data-resizing="resizing" :style="gridStyle">
    <MediaPanel v-model:tool="libraryTool" class="area-media" />
    <PreviewStage class="area-preview" />
    <PropertiesPanel id="workspace-properties" class="area-props" @open-library="libraryTool = $event"
      @open-autocut="emit('open-autocut')" @generate-captions="emit('generate-captions')" />
    <TimelinePanel id="workspace-timeline" class="area-timeline" />
    <div v-for="item in dividers" :key="item.axis" class="splitter" :class="item.axis"
      role="separator" tabindex="0" :aria-orientation="item.orientation" :aria-label="item.label"
      :aria-controls="item.target" :aria-valuemin="bounds[item.axis].min" :aria-valuemax="bounds[item.axis].max"
      :aria-valuenow="size(item.axis)" :aria-valuetext="size(item.axis) + ' piksel'" :title="item.label"
      @pointerdown.prevent="beginResize($event, item.axis)" @pointermove="moveResize"
      @pointerup="endResize" @pointercancel="endResize" @lostpointercapture="endResize"
      @keydown="keyResize($event, item.axis)" />
  </main>
</template>

<style scoped>
.workspace {
  flex: 1;
  min-height: 0;
  display: grid;
  overflow: hidden;
  background: var(--border-soft);
  grid-template-columns: var(--media-width) minmax(0, 1fr) var(--divider) var(--props-width);
  grid-template-rows: minmax(0, 1fr) var(--divider) var(--timeline-height);
  grid-template-areas: 'media preview grip props' 'bar bar bar bar' 'timeline timeline timeline timeline';
}
.workspace[data-layout="media"] {
  grid-template-areas: 'media preview grip props' 'media bar bar bar' 'media timeline timeline timeline';
}
.workspace[data-layout="attributes"] {
  grid-template-areas: 'media preview grip props' 'bar bar grip props' 'timeline timeline grip props';
}
.workspace[data-layout="vertical"] {
  grid-template-columns: minmax(0, 1fr) var(--divider) var(--props-width) var(--portrait-width);
  grid-template-areas: 'media grip props preview' 'bar bar bar preview' 'timeline timeline timeline preview';
}
.workspace[data-layout="vertical-left"] {
  grid-template-columns: var(--portrait-width) minmax(0, 1fr) var(--divider) var(--props-width);
  grid-template-areas: 'preview media grip props' 'preview bar bar bar' 'preview timeline timeline timeline';
}
.area-media { grid-area: media; min-width: 0; }
.area-preview { grid-area: preview; }
.area-props { grid-area: props; min-width: 0; }
.area-timeline { grid-area: timeline; min-width: 0; }
.splitter {
  position: relative;
  outline: none;
  touch-action: none;
  background: var(--border-soft);
}
.splitter.properties { grid-area: grip; cursor: col-resize; }
.splitter.timeline { grid-area: bar; cursor: row-resize; }
.splitter::after {
  content: '';
  position: absolute;
  inset: 50% auto auto 50%;
  transform: translate(-50%, -50%);
  background: var(--muted-2);
  border-radius: 2px;
}
.splitter.properties::after { width: 2px; height: 24px; }
.splitter.timeline::after { width: 24px; height: 2px; }
.splitter:hover, .splitter:focus-visible,
[data-resizing="properties"] .splitter.properties,
[data-resizing="timeline"] .splitter.timeline { background: var(--accent-soft); }
.splitter:hover::after, .splitter:focus-visible::after, .splitter:active::after { background: var(--accent); }
</style>
