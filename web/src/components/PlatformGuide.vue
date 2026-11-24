<script setup lang="ts">
import {
  BatteryFull, Bookmark, EllipsisVertical, Forward, Heart, MessageCircleMore,
  MessageSquareText, Music2, Repeat2, Search, Signal, ThumbsDown, ThumbsUp, UserRound, Wifi, Youtube,
} from 'lucide-vue-next'

defineProps<{ platform: 'shorts' | 'tiktok' }>()

const actions = {
  shorts: [
    { icon: ThumbsUp, text: '2.8M' },
    { icon: ThumbsDown, text: 'Dislike' },
    { icon: MessageSquareText, text: '2.8M' },
    { icon: Forward, text: '2.8M' },
    { icon: Repeat2, text: '2.8M' },
  ],
  tiktok: [
    { icon: Heart, text: '2.8M' },
    { icon: MessageCircleMore, text: '2.8M' },
    { icon: Bookmark, text: '2.8M' },
    { icon: Forward, text: '2.8M' },
  ],
}
</script>

<template>
  <div class="platform-guide" :class="platform" aria-hidden="true" inert>
    <div class="feed">
      <div class="status"><span>9:41</span><div><Signal /><Wifi /><BatteryFull /></div></div>
      <div class="header">
        <div v-if="platform === 'tiktok'" class="tabs"><span>Explore</span><span>Following</span><b>For You</b></div>
        <Search /><EllipsisVertical v-if="platform === 'shorts'" />
      </div>
      <div class="actions">
        <div v-if="platform === 'tiktok'" class="action"><span class="avatar"><UserRound /></span></div>
        <div v-for="(action, index) in actions[platform]" :key="index" class="action">
          <component :is="action.icon" :class="{ comments: action.icon === MessageCircleMore }" /><span>{{ action.text }}</span>
        </div>
      </div>
      <div class="details">
        <div class="author">
          <span v-if="platform === 'shorts'" class="avatar"><UserRound /></span>
          {{ platform === 'shorts' ? '@namaanda' : 'Nama Anda' }}
        </div>
        <span>Contoh deskripsi video</span>
        <span v-if="platform === 'tiktok'">Lihat asli</span>
      </div>
      <div class="sound"><Youtube v-if="platform === 'shorts'" /><Music2 v-else /></div>
    </div>
  </div>
</template>

<style scoped>
.platform-guide {
  position: absolute;
  inset: 0;
  z-index: 4;
  container-type: size;
  overflow: hidden;
  pointer-events: none;
}
.feed {
  position: absolute;
  inset: 0;
  font: 500 min(3.5cqw, 2cqh)/1.15 var(--font);
  color: rgba(255, 255, 255, .8);
  text-shadow: 0 1px 3px #000b;
  background: linear-gradient(to bottom, #0003, transparent 18%, transparent 75%, #0005);
}
.feed svg { display: block; height: 1em; width: 1em; filter: drop-shadow(0 1px 1px #0008); }
.status, .header, .actions, .details, .sound { position: absolute; }
.status, .status > div, .header, .tabs, .author, .sound { display: flex; align-items: center; }
.status { inset: 2% 4.5% auto 5%; justify-content: space-between; font-size: 1.25em; font-weight: 650; }
.status > div { gap: .12em; }
.status svg:last-child { width: 1.45em; height: 1.45em; }
.header { inset: 10% 4.5% auto; justify-content: flex-end; gap: .7em; font-size: 1.9em; }
.tiktok .header { top: 8.5%; }
.tabs {
  position: absolute;
  left: 50%;
  transform: translateX(-50%);
  gap: 1.3em;
  font-size: .60em;
  white-space: nowrap;
}
.tabs span { opacity: .75; }
.tabs b { position: relative; }
.tabs b::after { content: ''; position: absolute; inset: auto 25% -.5em; height: .15em; background: currentColor; }
.actions { inset: 37% 2% 11% auto; width: 12%; display: flex; flex-direction: column; justify-content: space-between; }
.tiktok .actions { top: 42%; }
.action { display: grid; justify-items: center; gap: .12em; }
.action svg { font-size: 2.15em; }
.tiktok .action > svg { fill: currentColor; }
.comments :deep(path:not(:first-child)) { stroke: #373737; }
.avatar {
  width: 2.25em;
  aspect-ratio: 1;
  display: grid;
  place-items: center;
  border-radius: 50%;
  background: rgba(255, 255, 255, .8);
}
.actions .avatar { width: 3.1em; }
.avatar svg { width: 56%; height: 56%; fill: currentColor; color: #393939; filter: none; }
.details { inset: auto 17% 2.2% 4.5%; display: grid; gap: .7em; }
.author { gap: .45em; font-weight: 600; }
.tiktok .details { left: 1.5%; }
.tiktok .author { font-size: 1.45em; }
.sound {
  right: 3%;
  bottom: 2%;
  width: 2.8em;
  height: 2.8em;
  justify-content: center;
  border-radius: .4em;
  background: #282828cf;
}
.sound svg { font-size: 1.4em; }
.tiktok .sound { border-radius: 50%; }
</style>
