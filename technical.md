# Technical Documentation — AutoCut Workstation

Dokumen ini menjelaskan arsitektur, data model, API, pipeline render, dan cara build. Untuk kebutuhan produk lihat [PRD.md](PRD.md); untuk mulai cepat lihat [README.md](README.md).

## 1. Gambaran Arsitektur

```
┌────────────────────────────────────────────────────────────┐
│ Tauri 2 shell (Rust)                                       │
│  • membuat window WebView2                                 │
│  • memilih port acak + token acak                          │
│  • spawn Go sidecar (stdin pipe = watchdog)                │
│  • command api_info, reveal_export                         │
└───────────────┬───────────────────────────────┬────────────┘
                │ invoke/api_info               │ HTTP + SSE
                ▼                               ▼
┌───────────────────────────────┐   ┌────────────────────────┐
│ Vue 3 + TS (Vite)             │   │ Go engine (sidecar)    │
│  • Pinia editor store         │──▶│  net/http API          │
│  • timeline, preview, dialog  │   │  SQLite + filesystem   │
│  • EventSource progress       │◀──│  ffmpeg / whisper / AI │
└───────────────────────────────┘   └────────────────────────┘
```

Prinsip:

- **Satu mesin, dua proses.** UI tidak pernah memanggil ffmpeg; semua operasi media lewat engine.
- **Loopback only.** Engine bind ke `127.0.0.1`; di mode desktop semua request (kecuali `/api/health`) membutuhkan token sesi.
- **State editor = JSON.** Timeline disimpan sebagai satu dokumen JSON di SQLite; UI memegang salinan dan melakukan autosave.
- **AI opsional.** Kegagalan DeepSeek/TypeSafe/Gemini tidak menghentikan auto-cut lokal.

## 2. Struktur Repository

```
cmd/server/               entrypoint engine (env config, watchdog, HTTP)
internal/
  ai/deepseek/            saran cut berbasis transkrip
  ai/typesafe/            validasi cutlist
  ai/gemini/              QA cutlist + metadata (legacy)
  api/                    routing, handler project, SSE task hub, auth
  captions/               generator caption + writer ASS
  config/                 loader env/config.json
  db/                     SQLite init + repository project/asset
  fonts/                  indeks font terpasang
  logger/                 file + console logger
  media/                  wrapper ffmpeg/ffprobe
  models/                 Project, Timeline, Clip, Asset, Transcript, ...
  pipeline/               orkestrasi auto-cut (AnalyzeVO)
  project/                folder project, import, thumbnail, waveform
  render/                 graph builder, overlay, ASS burn, encoder fallback
  storage/                cleaner/paths legacy
  stt/                    whisper.cpp runner + fallback mock
  timeline/               apply cut, split (pure functions)
web/                      UI Vue 3 + TypeScript
src-tauri/                Cargo project, konfigurasi Tauri, ikon
scripts/build-sidecar.mjs build engine Go dengan nama target-triple
legacy/web-svelte/        UI Svelte lama (arsip lokal, tidak dipakai build)
```

## 3. Proses Desktop

### 3.1 Startup

1. `src-tauri/src/lib.rs` membuat `ApiInfo { base_url, token }` dengan port dari `TcpListener::bind("127.0.0.1:0")`.
2. Token 24 byte acak (hex) dibuat per launch.
3. Sidecar dijalankan via `tauri-plugin-shell` dengan env:
   - `PORT`, `STORAGE_DIR` (`app_data_dir`, yaitu `%APPDATA%\com.txshd.autocut`), `AUTOCUT_TOKEN`, `AUTOCUT_WATCH_STDIN=1`.
   - `WHISPER_CLI_PATH` / `WHISPER_MODEL_PATH` default ke `<app_data>/bin/whisper/...` jika env OS tidak diset.
4. Frontend memanggil `invoke('api_info')` saat boot, lalu `waitForServer()` mem-poll `/api/health` sampai 25 detik.

### 3.2 Shutdown

- Normal: event `ExitRequested`/`Exit` memanggil `stop_sidecar()` (kill child).
- Paksa (Task Manager / kill parent): stdin pipe sidecar tertutup → goroutine `watchParent()` di engine mendapat EOF dan `os.Exit(0)`. Tidak ada engine yatim yang tetap listen.

### 3.3 Command Tauri

| Command | Argumen | Fungsi |
|---|---|---|
| `api_info` | — | Kembalikan `{ base_url, token }`. |
| `reveal_export` | `projectId`, `outputName` | Validasi path, lalu buka Explorer dengan file terpilih (`explorer /select,`). Fallback macOS/Linux disediakan. |

## 4. Data Model

### 4.1 Tabel editor

```sql
CREATE TABLE projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  width INTEGER NOT NULL DEFAULT 1920,
  height INTEGER NOT NULL DEFAULT 1080,
  fps REAL NOT NULL DEFAULT 30,
  timeline_json TEXT NOT NULL DEFAULT '{}',
  transcript_json TEXT,
  created_at DATETIME,
  updated_at DATETIME
);

CREATE TABLE assets (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  kind TEXT NOT NULL,              -- video | audio | image
  filename TEXT NOT NULL,
  path TEXT NOT NULL,
  duration REAL, width INTEGER, height INTEGER, fps REAL,
  has_audio INTEGER DEFAULT 0,
  thumb_path TEXT DEFAULT '',
  waveform_path TEXT DEFAULT '',
  size INTEGER,
  created_at DATETIME
);
```

Tabel legacy `jobs`, `transcripts`, `cutlists`, `metadata` masih ada untuk API `/api/jobs`, tetapi editor baru memakai `projects`/`assets`.

### 4.2 Timeline JSON

```jsonc
{
  "version": 1,
  "width": 1920,
  "height": 1080,
  "fps": 30,
  "caption_style": {
    "font_family": "Arial", "font_size": 72,
    "color": "#FFFFFF", "highlight_color": "#FFD400",
    "stroke_width": 5, "stroke_color": "#000000",
    "shadow": 2, "opacity": 1, "blend_mode": "normal",
    "position_y": 0.82, "uppercase": false,
    "highlight": true, "words_per_chunk": 3
  },
  "tracks": [
    { "id": "video-main", "kind": "video", "clips": [] },
    { "id": "overlay", "kind": "overlay", "clips": [] },
    { "id": "audio-main", "kind": "audio", "clips": [] },
    { "id": "caption", "kind": "caption", "clips": [] }
  ]
}
```

Track punya `muted` dan `locked`. Clip:

| Field | Arti |
|---|---|
| `start`, `duration` | Posisi/panjang di timeline (detik). |
| `in`, `out` | Rentang sumber pada asset (detik). |
| `x`, `y` | Pusat overlay ternormalisasi (0..1). |
| `scale`, `rotation`, `opacity` | Transform overlay. |
| `blend_mode` | normal, multiply, screen, overlay, softlight, hardlight, addition, difference, darken, lighten, dodge, burn. |
| `text`, `style` | Overlay teks + TextStyle. |
| `volume`, `fade_in`, `fade_out` | Audio clip. |
| `words[]` | Waktu kata absolut timeline untuk caption karaoke. |
| `asset_id` | Asset sumber (kosong untuk teks). |

### 4.3 Folder project

```
<STORAGE_DIR>/
  autocut.db
  projects/<projectID>/
    assets/        file media yang diimpor (disalin)
    thumbs/        thumbnail JPEG
    waveforms/     peak JSON
    cache/         overlay PNG, text file, captions.ass, filter_graph.txt
    exports/       hasil MP4
```

Cache overlay lama (`overlay_*.png`, `text_*.txt`) dibersihkan di awal setiap render; `captions.ass` dan `filter_graph.txt` ditimpa.

## 5. HTTP API

Semua request/response JSON kecuali upload multipart dan file streaming. Saat `AUTOCUT_TOKEN` diset, kirim `X-Autocut-Token` atau `?token=`.

| Method & Path | Fungsi |
|---|---|
| `GET /api/health` | Status engine, ffmpeg, whisper, `token_required`. Bebas token. |
| `GET /api/fonts` | Daftar font sistem `{ family, subfamily, full_name, path }`. |
| `GET /api/projects` | List project (100 terbaru). |
| `POST /api/projects` | Buat project `{ name, width, height, fps }`. |
| `GET /api/projects/{id}` | Detail project + timeline. |
| `PATCH /api/projects/{id}` | Rename `{ name }`. |
| `DELETE /api/projects/{id}` | Hapus project + folder. |
| `PUT /api/projects/{id}/timeline` | Simpan timeline (body = Timeline). |
| `GET /api/projects/{id}/assets` | List asset. |
| `POST /api/projects/{id}/import` | Upload multipart field `file`. |
| `POST /api/projects/{id}/import-path` | Impor dari path lokal `{ path }`. |
| `DELETE /api/projects/{id}/assets/{assetID}` | Hapus asset. |
| `GET /api/projects/{id}/assets/{assetID}/file` | Stream media (Range). |
| `GET /api/projects/{id}/assets/{assetID}/thumb` | Thumbnail. |
| `GET /api/projects/{id}/assets/{assetID}/waveform` | Peak waveform JSON. |
| `GET /api/projects/{id}/transcript` | Transkrip terakhir. |
| `POST /api/projects/{id}/analyze` | Mulai analisis `{ asset_id, use_ai?, use_qa? }` → 202. |
| `GET /api/projects/{id}/analyze` | Hasil analisis terakhir (memori). |
| `GET /api/projects/{id}/analyze/events` | SSE progress/result analisis. |
| `POST /api/projects/{id}/autocut/apply` | Terapkan cut `{ asset_id, cuts[], ripple? }`. |
| `POST /api/projects/{id}/captions` | Generate caption `{ replace? }`. |
| `POST /api/projects/{id}/export` | Mulai export `{ settings }` → job. |
| `GET /api/projects/{id}/export/events` | SSE progress/result export. |
| `GET /api/projects/{id}/export/latest` | Job export terakhir. |
| `GET /api/projects/{id}/export/{exportID}/download` | Unduh MP4 (Range). |

Legacy `/api/jobs*` dipertahankan untuk pipeline lama.

### 5.1 Task hub & SSE

`internal/api/tasks.go` menyimpan satu task per `(kind, projectID)`.

- `begin` membuat state `running` dan channel subscriber.
- `Update` menyiarkan progress; `Complete`/`Fail` menyiarkan state akhir, menutup channel, tetapi **menyimpan event terakhir**.
- Subscriber yang datang terlambat langsung menerima state terakhir (mencegah race antara POST dan pembukaan EventSource).
- SSE ditulis sebagai `data: <json>\n\n` sampai status bukan `running`.
- `TaskEvent.result` berisi `AnalysisResult` atau snapshot `ExportJob`.
- Hanya satu analisis dan satu export per project pada satu waktu (409 jika sudah berjalan).

## 6. Auto-Cut Pipeline

`pipeline.Orchestrator.AnalyzeVO(videoPath, AnalyzeOptions)`:

1. **Extract audio** 16 kHz mono PCM ke `<cache>/analyze`.
2. **Silence detection** dengan ffmpeg `silencedetect` (noise threshold & durasi minimum dari config).
3. **Whisper** (jika binary ada): `whisper-cli -m <model> -f <wav> --language id --dtw -oj -of <base>`; parse token `offsets` menjadi `WordTiming` (detik, confidence).
4. **Fallback mock** jika whisper tidak ditemukan: 5 kalimat contoh, ditandai `mock: true`, kata-kata tersebar sepanjang durasi.
5. **DeepSeek** (jika `UseAI` dan key tersedia): kirim segmen transkrip, terima proposal cut; **TypeSafe** memvalidasi batas & confidence.
6. **Rule-based filler/retake** selalu berjalan (kata filler Bahasa Indonesia/Inggris, pengulangan frasa).
7. **Gemini QA** (jika `UseQA` dan key tersedia) menandai cut berisiko.
8. Hasil: `AnalysisResult{ transcript, cuts, total_cut_duration }`. Transkrip disimpan ke `projects.transcript_json` lengkap dengan `asset_id`.

Setiap `CutSegment` punya `reason` (`silence`, `filler`, `retake`, `off_topic`, `keep`), `source`, `confidence`, `needs_review`, `is_cut`.

### 6.1 Apply cut

`timeline.ApplyCuts(tl, clipIDs, cuts, ripple)` murni tanpa I/O:

1. Cut dinyatakan dalam **waktu asset**.
2. Untuk setiap clip target, hitung interval keep = `[in,out]` dikurangi cut yang di-merge.
3. Potongan keep dibuat berurutan mulai dari `clip.start` (durasi total menyusut).
4. Jika `ripple`, clip non-target pada track yang sama dengan `start >= akhir cut` digeser sebesar durasi yang hilang.
5. Track caption selalu dilewati.
6. `SplitClipAt` memotong clip di playhead: kiri tetap `id`, kanan `id_baru`, `words` dikosongkan agar caption di-generate ulang.

Handler `autocut/apply` mengumpulkan clip ID dari asset jika tidak diberikan, menyimpan timeline, dan mengembalikan `removed_seconds`.

## 7. Caption

### 7.1 Generator

`captions.FromTranscript(sources, transcript, style)`:

- Sumber = clip pada track Audio/Video yang `asset_id == transcript.asset_id`.
- Kata yang overlap `[clip.in, clip.out]` dipetakan ke `clip.start + (word.start - clip.in)`.
- Kata dikelompokkan (`words_per_chunk`, default 3) dengan pemutus: jeda antar-kata > 0.6 s, chunk > 5 s, atau tanda baca akhir kalimat.
- Chunk < 0.2 s digabung/dibuang untuk menghindari caption berkedip.
- Setiap clip caption menyimpan `words[]` bertimestamp absolut; ID `cap_<index>_<jumlahKata>`.

Handler `captions` menghapus isi track caption (default `replace=true`) lalu menambahkan hasil generate.

### 7.2 ASS

`render.writeASS` menulis:

- Satu event per chunk; highlight per kata memakai event tambahan dengan warna `highlight_color`.
- Font size diskalakan `height/1080`; posisi `\pos(width/2, position_y*height)`.
- Stroke memakai `Outline`, shadow memakai `Shadow`, warna ASS BGR.
- Burn dengan `ass=filename=...:fontsdir=C:\Windows\Fonts`.

**Keterbatasan:** ASS tidak mendukung blend mode. `caption_style.blend_mode` hanya dipakai preview CSS. UI menampilkan peringatan.

## 8. Render Pipeline

`render.Renderer.Render` membangun **satu filter graph** dan menjalankannya sekali per encoder.

### 8.1 Tahap

1. Validasi timeline & resolusi (genap, minimal 16 px).
2. Pre-render setiap clip overlay ke PNG seukuran canvas project:
   - Teks: `drawtext` dengan font yang di-resolve, stroke, shadow, box, rotasi, opacity.
   - Gambar: `scale` sesuai fit, `rotate`, `colorchannelmixer=aa`, komposisi di atas **canvas transparan**.
   - Penting: sumber `color=c=black@0` harus diikuti `format=rgba` agar alpha benar-benar transparan; tanpa ini PNG menjadi opaque hitam dan menutupi video.
3. Build graph:
   - **Video base:** trim per clip → `setpts`, `fps`, `scale` + `pad`, segmen hitam untuk gap, `concat`.
   - **Audio:** trim, volume, fade in/out, `adelay` per clip, `amix`, `apad`/`atrim` ke durasi total.
   - **Overlay:** tiap PNG di-loop, `overlay` untuk normal; untuk blend: `split` → `blend=all_mode=...` → `alphaextract` → `maskedmerge` dengan `enable='between(t,start,end)'`.
   - **Caption:** `ass` filter, opsional `scale` ke resolusi output.
4. Input dideduplikasi per asset; bila asset dipakai beberapa kali, `split`/`asplit` dipakai.
5. Tulis graph ke `cache/filter_graph.txt`, jalankan ffmpeg dengan `-filter_complex_script`, `-progress pipe:1`.
6. Parse `out_time` untuk progress (14%–99%), lalu `report(1)`.

### 8.2 Encoder

| Encoder | Args |
|---|---|
| `h264_amf` | `-quality quality -rc cqp -qp_i/qp_p/qp_b <q>` |
| `h264_nvenc` | `-preset p5 -rc vbr -cq <q> -b:v 0` |
| `libx264` | `-preset veryfast -crf <q>` |

`auto` mencoba `h264_amf` lalu `libx264`; encoder lain yang gagal juga fallback ke `libx264`. Output AAC 192k/48k, `-movflags +faststart`.

### 8.3 Perilaku audio

Export hanya membaca clip pada track **Audio** yang tidak muted. Audio tertanam pada file video tidak ikut diproses. UI mengompensasi dengan menambahkan clip audio otomatis saat video ber-audio dimasukkan; jika clip itu dihapus, audio video tidak muncul di hasil export.

## 9. Frontend

### 9.1 Struktur

- `web/src/api.ts` — base URL/token (Tauri `api_info` atau `VITE_API_URL`), wrapper fetch, EventSource.
- `web/src/stores/editor.ts` — Pinia setup store:
  - project, assets, fonts, selection, playhead, playing, zoom, analysis, toast.
  - history: snapshot JSON (maks 100), coalescing edit dengan key yang sama < 900 ms.
  - autosave: debounce 650 ms → `PUT /timeline`.
  - playback: `requestAnimationFrame`, berhenti di durasi timeline.
  - editing: update clip, delete, split, add text/asset, track mute/lock, caption style.
- Komponen: `TopBar`, `ProjectLauncher`, `MediaPanel`, `PreviewStage`, `OverlayPreview`, `CaptionPreview`, `TimelinePanel`, `PropertiesPanel`, `AutoCutDialog`, `ExportDialog`, `ToastHost`.
- `TimelinePanel` menghitung posisi pixel dari `start * pxPerSecond`, snapping ke playhead/tepi clip/0, drag move/trim memakai pointer events; wheel horizontal scroll; zoom 0.2–4×.

### 9.2 Preview

- Satu elemen `<video>` aktif (muted) + elemen `<audio>` per clip audio aktif.
- Sinkronisasi: jika selisih `currentTime` > 0.28 s → seek; play/pause mengikuti store.
- Overlay DOM memakai posisi persen, transform, opacity, dan `mix-blend-mode` (pemetaan blend ffmpeg → CSS).
- Caption DOM: ukuran font diskalakan dari tinggi stage, stroke via `-webkit-text-stroke`, highlight kata aktif, blend hanya visual.

### 9.3 Alur dialog

- **AutoCutDialog:** pilih asset → POST analyze → subscribe SSE → tampilkan cut per alasan → pilih → POST apply → replace timeline di store.
- **ExportDialog:** set nama/resolusi/encoder/quality → POST export → SSE → tombol unduh/buka folder. Jika ada export selesai sebelumnya, dialog bisa render ulang.

## 10. Build & Packaging

### 10.1 Script

```bash
npm run dev            # build sidecar + tauri dev
npm run build          # build sidecar + tauri build (installer)
npm run build:sidecar  # go build ./cmd/server ke src-tauri/binaries
npm run build:web      # vue-tsc --noEmit && vite build
```

`npm run build` menjalankan `beforeBuildCommand` (`npm --prefix web run build`) dan `beforeDevCommand` (`npm --prefix web run dev`).

### 10.2 External binary

`src-tauri/tauri.conf.json` mendeklarasikan `externalBin: ["binaries/autocut-server"]`. Tauri mencari nama sesuai target triple:

```
src-tauri/binaries/autocut-server-x86_64-pc-windows-msvc.exe
```

`scripts/build-sidecar.mjs` menentukan triple dari `TAURI_ENV_TARGET_TRIPLE`, `CARGO_BUILD_TARGET`, atau `rustc -vV`, lalu menjalankan `go build -trimpath -ldflags "-s -w"`.

### 10.3 Ikon

Ikon dihasilkan dari `src-tauri/icons/source.png` dengan `npx tauri icon`. File yang dipakai installer: `icon.ico`, `icon.icns`, `32x32.png`, `128x128.png`, `128x128@2x.png`.

## 11. Testing

| Level | Perintah | Cakupan |
|---|---|---|
| Go unit | `go test ./...` | orchestrator, ASS, ffmpeg filter, API handler |
| Go build | `go build ./...` | seluruh engine |
| Type check | `npm --prefix web exec vue-tsc --noEmit` | Vue + TS |
| Web build | `npm --prefix web run build` | bundling Vite |
| Rust | `cd src-tauri && cargo check` | shell Tauri |
| Desktop | `npx tauri build --debug --no-bundle` | shell + frontend + sidecar |
| Manual | buat project → impor → timeline → analyze → captions → export | end-to-end |

Smoke test yang sudah divalidasi: project 1280×720/30fps, video 8 s + voice 12 s + gambar overlay (screen blend, rotasi) + teks + caption; analisis mock menghasilkan 3 cut; apply cut menghapus 4,5 s; export 15,5 s berhasil dengan video, audio, overlay, dan caption terbakar.

## 12. Troubleshooting

| Gejala | Penyebab umum | Solusi |
|---|---|---|
| UI "Engine belum siap" | sidecar gagal start / ffmpeg tidak ada | Jalankan `go run ./cmd/server` manual, cek log; pastikan ffmpeg di PATH. |
| Export gagal encoder | GPU tidak mendukung AMF | Pilih encoder `libx264` atau biarkan `auto`. |
| Caption kosong | belum ada transkrip atau clip audio tidak memakai asset yang dianalisis | Jalankan Auto Cut untuk asset tersebut; pastikan clip ada di track Audio/Video. |
| Transkrip mock | whisper-cli/model tidak ditemukan | Letakkan binary & model di `<STORAGE_DIR>/bin/whisper` atau set env. |
| Hasil export beda dari preview | blend caption hanya preview | Gunakan warna solid untuk caption; blend berlaku untuk overlay teks/gambar. |
| Port bentrok di mode browser | engine lain memakai 8080 | Set `PORT` (engine) dan `VITE_API_URL` (UI) ke nilai sama. |
| Sidecar tetap hidup setelah app ditutup paksa | versi lama tanpa watchdog | Build ulang sidecar (`npm run build:sidecar`). |
