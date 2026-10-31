# Plan: Timeline Dinamis ala CapCut

Dokumen ini merencanakan evolusi timeline AutoCut dari "4 track tetap" menjadi editor multi-track yang benar-benar dinamis: track tak terbatas, drag antar track, ripple/overwrite, multi-select, snapping, keyframes, transisi, dan speed. Belum ada perubahan kode — ini dokumen keputusan dan urutan kerja.

## 1. Audit Kondisi Sekarang

| Area | Kondisi | Lokasi |
|---|---|---|
| Model track | Selalu 4 track (video, overlay, audio, caption), dibuat saat project baru | `internal/models/project.go:197` |
| Tambah/hapus/reorder track | Tidak ada | — |
| Jumlah track yang dirender | Video: **hanya track video pertama**; overlay: semua track overlay; audio: semua track audio; caption: semua track caption digabung 1 ASS | `internal/render/render.go:410`, `:500`, `:578`, `:629` |
| Track hidden/height | Tidak ada (hanya `muted` dan `locked`) | `internal/models/project.go:63` |
| Drag clip | Horizontal saja (move/trim), tidak bisa pindah track | `web/src/components/TimelinePanel.vue:190` |
| Drop media | Hanya ke track dengan kind yang sama | `web/src/components/TimelinePanel.vue:266` |
| Seleksi | Satu clip (`selection: {trackId, clipId}`) | `web/src/stores/editor.ts:27`, `:310` |
| Ripple | Hanya saat apply auto-cut, per track | `internal/timeline/edit.go:64` |
| Overwrite/insert | Tidak ada; clip boleh bertumpuk di track yang sama dan renderer mengabaikannya | `internal/render/render.go:443` |
| Snapping | Ke playhead/tepi clip/0 tanpa guide line | `web/src/components/TimelinePanel.vue:154` |
| Undo/redo | Snapshot JSON timeline, maks 100 | `web/src/stores/editor.ts:111` |
| Fitur belum ada | Multi-select, marquee, blade tool, copy/paste, ripple delete, track reorder/height, markers, speed, freeze, transisi, keyframes | — |

Kesimpulan: fondasi JSON + snapshot undo + autosave sudah cocok untuk timeline dinamis. Pekerjaan terbesar ada di **model/validasi**, **renderer berlapis**, dan **interaksi timeline**.

## 2. Definisi "Dinamis" (Target Perilaku)

1. Tambah/hapus/reorder track; tinggi track bisa diatur; video track bisa di-hide, audio bisa di-mute, semua bisa di-lock.
2. Clip bisa dipindah antar track yang kompatibel (video↔video, audio↔audio, overlay↔overlay).
3. Multi-select (klik + Shift/Ctrl, marquee), lalu move/trim/delete/copy/paste/duplicate bersama.
4. Magnetic snapping ke playhead, tepi clip, marker, dan 0 — dengan guide line saat menempel.
5. Dua mode editing: **overwrite** (default CapCut untuk drop) dan **insert/ripple**; ripple delete menutup gap.
6. Blade tool + split di playhead, trim dengan batas source asset.
7. Video+audio hasil impor bisa **linked** (bergerak/trim/hapus bersama, lepas dengan Alt).
8. Compositing preview dan export mengikuti urutan track (bawah→atas), opacity, dan blend mode.
9. Zoom ke kursor (Ctrl+wheel), fit, auto-scroll mengikuti playhead, timecode per clip.
10. Fase lanjutan: speed, freeze frame, transisi, keyframes transform/opacity/volume.

## 3. Desain Teknis

### 3.1 Model Timeline v2

```ts
Timeline {
  version: 2
  width, height, fps
  caption_style
  tracks: Track[]        // index 0 = paling atas di UI
}
Track {
  id, kind: 'video'|'audio'|'overlay'|'caption'
  name
  muted, locked, hidden
  height: number         // px UI, default 52
  clips: Clip[]
}
Clip {
  id, asset_id?, start, duration, in, out
  x, y, scale, rotation, opacity, blend_mode
  text?, style?
  volume, fade_in, fade_out
  words?
  speed: number          // default 1
  linked_id?: string     // pasangan video/audio
  keyframes?: Keyframe[] // fase M4
  transition_in?, transition_out? // fase M4
}
```

Perubahan:
- `hidden` baru; `height` baru; `speed` default 1; `linked_id` opsional.
- Urutan track = urutan array (sudah didokumentasikan di `internal/models/project.go:62`, tinggal dipakai renderer).
- **Migrasi v1 → v2** di server saat load (`scanProject`) dan normalisasi di `PUT /timeline`: set `version=2`, `speed=1`, `height=52`, `hidden=false`; pastikan minimal ada 1 track video & 1 audio; ID track unik; durasi clip ≥ 0.
- Validasi server: kind valid, clip tidak keluar dari track (start ≥ 0), `in/out` konsisten, maksimum track/clip wajar (mis. 32 track, 2000 clip) agar render tidak meledak.
- Tipe frontend disinkronkan di `web/src/types.ts`.

### 3.2 Semantik Track & Overlap

Bedakan perilaku per jenis track, mengikuti CapCut:

| Track | Overlap | Default drop | Ripple delete |
|---|---|---|---|
| Video utama (magnetic) | Tidak boleh, tanpa gap | Insert: clip setelahnya bergeser | Ya, menutup gap otomatis |
| Video/overlay tambahan | Boleh antar track | Bebas (stack by track) | Tidak |
| Audio | Tidak boleh | Overwrite/insert mengikuti toggle | Ya bila mode ripple |
| Caption | Tidak boleh | Terkunci dari drag clip biasa | Ya |

**Keputusan**: track video utama bersifat **magnetic** seperti CapCut — setelah move/trim/delete, clip di track utama dirapatkan (tidak menyisakan gap), dan drop di tengah menggeser clip setelahnya. Gap hanya muncul jika dibuat sengaja lewat fitur "gap" di fase lanjutan. Track video/overlay tambahan tetap bebas bertumpuk.

Aturan ditulis sekali di backend (`internal/timeline`) dan dipakai frontend untuk preview, agar hasil edit dan render tidak berbeda.

### 3.3 Renderer Berlapis (pekerjaan inti)

Saat ini `buildVideo` menyusun satu track secara sekuensial dengan gap hitam (`internal/render/render.go:443`), sehingga tidak bisa compositing. Rencana:

1. **Base layer**: track video paling bawah → hasil seperti sekarang (trim/concat + gap hitam).
2. **Layer atas**: setiap track video di atasnya dirender per clip sebagai overlay penuh canvas dengan `enable='between(t,start,end)'`, scaling/pad sama seperti base. Clip yang tidak ada = transparan (tidak perlu gap).
3. **Overlay track**: generalisasi `buildOverlays` agar menghormati urutan track (bukan `sortClips` global) dan `hidden`; blend/alpha sudah ada (`render.go:578`).
4. **Audio**: `buildAudio` sudah mencampur semua track audio; tambahkan dukungan `linked_id`, per-track gain, dan `atempo` untuk speed (fase M4).
5. **Caption**: gabungkan hanya track caption yang tidak `hidden`; tetap satu ASS dengan style global (per-track style masuk fase lanjutan).
6. **Kinerja**: satu filter graph dengan banyak layer mahal. Guard: jika total layer > 8 atau clip > 300, pre-render track atas ke file intermediate (RGBA/MP4) lalu compositing tahap kedua. Ukur dulu; optimasi hanya jika perlu.
7. **Presisi**: tetap re-encode penuh (tanpa `-c copy`), frame-accurate.

### 3.4 Frontend: Timeline Engine

Modul baru/refactor:

- `web/src/stores/selection.ts` — refactor seleksi menjadi `Set<ClipRef>` + primary selection; operasi batch.
- `web/src/lib/snapping.ts` — kumpulkan kandidat snap (tepi clip semua track, playhead, marker, 0), threshold px→detik, hasilkan `{time, guide}`.
- `web/src/lib/timelineOps.ts` — operasi murni: move clips (delta + target track), trim, ripple delete, overwrite/insert, split, duplicate, link-aware. Mudah diuji unit.
- `TimelinePanel.vue` — pisahkan drag coordinator (move/trim/blade/marquee/cross-track), render guide line, track header drag-reorder, resize height, context menu.
- `web/src/stores/editor.ts` — semua mutasi struktural lewat `mutate()` (sudah ada) agar undo/autosave konsisten; tambah `addTrack`, `removeTrack`, `moveTrack`, `setTrackHidden`, batch selection ops.
- Preview (`PreviewStage.vue`) — stack `<video>` aktif per track video (z-index = urutan track), opacity/blend; hanya mount clip yang aktif di playhead; sinkronisasi tetap seperti sekarang.
- Toolbar timeline: tool select/blade, toggle ripple/overwrite, add track, zoom, snap toggle.

Detail interaksi:

- **Cross-track drag**: hit-test track berdasarkan Y; tolak jika kind tidak kompatibel; tampilkan ghost clip + highlight lane.
- **Multi-select**: Shift/Ctrl klik; marquee di area kosong; drag salah satu clip yang terpilih memindahkan semua (offset relatif dipertahankan).
- **Snapping**: on by default; tahan `Shift` untuk mematikan sementara; guide line vertikal saat snap.
- **Ripple/overwrite**: toggle global + modifier (`Alt` = bebas/abaikan snap, `Ctrl` = duplicate drag bila diinginkan).
- **Blade**: klik di clip memotong di posisi kursor; `Ctrl+B` tetap split di playhead.
- **Clipboard**: Ctrl+C/X/V pada playhead/track aktif; paste mempertahankan offset relatif untuk multi-select.
- **Nudge**: arrow = 1 frame, Shift+arrow = 1 detik (hanya saat timeline fokus, bukan saat playhead shortcut).
- **Linked clips**: jika ada `linked_id`, move/trim/delete berlaku ke pasangannya; Alt melepas sementara.

### 3.5 API & Backend Lain

- `PUT /projects/{id}/timeline`: normalisasi + validasi v2 (lihat 3.1).
- `POST /captions`: pilih track caption berdasarkan ID bila ada, buat baru bila belum; jangan asumsikan track pertama (`internal/api/projects.go:415`).
- `POST /autocut/apply`: terima daftar clip target lintas track; ripple menghormati aturan per track; pertimbangkan clip linked.
- `POST /export`: renderer menerima urutan track; `hidden`/`muted` dihormati.
- Project baru: tetap 4 track default (video, overlay, audio, caption) tapi bisa ditambah/dihapus user.
- Kompatibilitas: project v1 tetap bisa dibuka; simpan berikutnya menjadi v2.

### 3.6 Preview vs Export Parity

Setiap perilaku baru harus punya padanan preview dan render:
- Urutan layer & blend: CSS `mix-blend-mode` (preview) vs `blend`+`maskedmerge` (export).
- Track hidden: tidak dirender di keduanya.
- Speed/transisi/keyframes: preview memakai `playbackRate`/CSS, export memakai filter ffmpeg — perlu tabel padanan agar tidak drift.

## 4. Milestone

> Status per 2026-10-29: **M0 selesai**, **M1 selesai** (renderer berlapis + UI multi-track terverifikasi di preview, frame export, dan persistensi setelah reload), **M2 selesai** (seleksi ganda/marquee, batch drag/delete, ripple, overwrite/insert, blade, clipboard, nudge, context menu, snapping guide, linked clip video+audio; terverifikasi lewat store harness dan uji browser/CDP). Berikutnya: evaluasi sebelum M3.

### M0 — Fondasi model & validasi (0.5–1 hari)
- Model v2 + migrasi v1 + normalisasi server + types frontend.
- Tes: load v1, simpan v2, validasi menolak data rusak.
- **Acceptance**: project lama terbuka tanpa perubahan visual; save menghasilkan `version: 2`.

### M1 — Multi-track core (3–5 hari)
- Track add/remove/reorder/hide/height + UI header.
- Cross-track drag & drop; aturan kompatibilitas kind.
- Renderer: video berlapis, overlay mengikuti urutan track, caption/audio menghormati hidden/muted.
- Preview stacking per video track.
- **Acceptance**: 2 video track bertumpuk terlihat benar di preview dan frame export; drag clip antar track tersimpan setelah reopen.

### M2 — Ergonomi editing (3–4 hari)
- Multi-select + marquee, batch move/trim/delete.
- Ripple delete & mode overwrite/insert.
- Snapping guide line, blade tool, clipboard, nudge, context menu.
- Linked video+audio.
- **Acceptance**: 10 clip dipilih lalu digeser/dihapus sebagai grup; undo mengembalikan tepat; ripple delete menutup gap; linked clip bergerak bersama.

### M3 — Feel & performa (2–3 hari)
- Zoom ke kursor, fit, auto-scroll, timecode, marker.
- Virtualisasi/caching waveform & thumbnail; target 300 clip tetap > 50 fps saat scroll.
- Track height persisten per project; state zoom/scroll per project (UI-only).
- **Acceptance**: timeline 10 menit / 200 clip tetap responsif; zoom tidak menggeser playhead liar.

### M4 — Motion & timing (5–8 hari)
- Speed clip (video `setpts`, audio `atempo`), freeze frame.
- Transisi `xfade`/`acrossfade` antar clip bersebelahan.
- Keyframes transform/opacity/volume + easing; inspector keyframe + preview.
- **Acceptance**: speed 2× sinkron audio-video; fade in/out via keyframe sama hasilnya di preview dan export.

### M5 — Polish & dokumentasi (2–3 hari)
- Per-track caption style (opsional), audio gain per track, color label/marker.
- Update README/technical/PRD; test matrix; regresi visual frame export.

Total realistis: **±16–24 hari kerja** solo, dengan M1+M2 sebagai inti "dinamis" (≈ 7–10 hari).

## 5. Yang Sengaja Belum Masuk Scope

- Library efek/transisi bergaya CapCut, text animation preset, sticker.
- Multicam, auto-reframe, background removal, AI beat-sync.
- Kolaborasi real-time / cloud storage.
- Nested sequence / compound clip.

## 6. Risiko & Mitigasi

| Risiko | Mitigasi |
|---|---|
| Filter graph meledak untuk banyak layer | Guard jumlah layer + pre-render track atas; ukur waktu export |
| Preview dan export berbeda (blend/speed/keyframe) | Tabel padanan + tes frame visual per milestone |
| Data lama rusak saat migrasi | Migrasi idempoten + backup `timeline_json` sebelum save v2 + tes fixture v1 |
| Interaksi drag makin kompleks dan buggy | Pindahkan logika ke `timelineOps.ts` murni + unit test; e2e manual per milestone |
| Undo/autosave kacau pada operasi struktural | Semua mutasi lewat `mutate()`; snapshot per operasi; flush save tetap |
| Performa preview banyak video | Mount hanya clip aktif; batasi jumlah layer preview; placeholder saat scroll |

## 7. Keputusan Final (disetujui)

| Keputusan | Pilihan |
|---|---|
| Urutan kerja | **M1 + M2 dulu** (multi-track core + ergonomi editing), M3–M5 menyusul |
| Track video utama | **Magnetic/ripple** ala CapCut: tanpa gap, drop menggeser clip setelahnya |
| Style caption | **Global per project** (satu `CaptionStyle`), tidak per track |
| Target skala | **8 track / 30 menit / 500 clip** dalam satu project |

Implikasi langsung:
- M3 wajib menyertakan caching waveform/thumbnail dan virtualisasi clip untuk target 500 clip.
- Renderer M1 perlu guard layer (max 8–12 track aktif) dan pengukuran export 30 menit.
- Magnetic hanya untuk track video utama; track video tambahan/overlay tetap free placement.
- Caption generator tetap memakai satu track caption global style.

Urutan eksekusi: **M0 → M1 → M2**, lalu evaluasi hasil sebelum masuk M3.
