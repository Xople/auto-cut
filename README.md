# AutoCut Workstation

Editor video desktop lokal untuk produksi **TxShD** — menggabungkan timeline bergaya CapCut, auto-cut voice-over, caption generator, dan export ffmpeg dalam satu aplikasi. Dibangun dengan **Tauri 2 + Vue 3 (TypeScript)**, dengan engine **Go + ffmpeg + whisper.cpp** berjalan sebagai sidecar di mesin yang sama.

Tidak ada server cloud, tidak ada upload media. Semua project, aset, dan hasil render tersimpan di komputer sendiri.

Dokumen lain:

- [PRD.md](PRD.md) — kebutuhan produk, user flow, kriteria penerimaan.
- [technical.md](technical.md) — arsitektur, API, skema data, pipeline render, build.

## Fitur

**Media & timeline**

- Impor video, audio, dan gambar lewat file picker atau drag-and-drop.
- Timeline multi-track (Video, Overlay, Audio, Caption) dengan trim, geser, split, snap, zoom, mute, dan lock per track.
- Thumbnail dan waveform dibuat otomatis oleh ffmpeg.
- Video yang punya audio otomatis menambah clip audio terpisah agar suara ikut ter-export.
- Undo/redo, autosave timeline, dan rename project.

**Auto Cut**

- Analisis voice-over: silence detection (ffmpeg), transkrip lokal (whisper.cpp), lalu saran cut dari DeepSeek + validasi TypeSafe, dengan QA opsional dari Gemini.
- Daftar saran cut per alasan (senyap, filler, retake, off-topic) yang bisa dicentang sebelum diterapkan.
- Apply cut berbasis waktu aset: clip dipadatkan dan track setelahnya ikut bergeser (ripple) secara opsional.
- Semua fitur AI opsional; tanpa API key, auto-cut tetap berjalan dari silence detection + aturan teks.

**Caption**

- Generator caption dari transkrip, mengikuti trim/split clip (waktu kata dipetakan ulang ke timeline).
- Gaya caption: font sistem, ukuran, bold/italic, warna, stroke, shadow, uppercase, posisi vertikal, highlight karaoke, jumlah kata per potongan.
- Preview caption real-time di stage editor.
- Caption di-export sebagai hardsub ASS.

**Overlay teks & gambar**

- Overlay teks dengan font, stroke, shadow, background, rotasi, opacity.
- Overlay gambar dengan posisi, skala, rotasi, opacity, dan blend mode (normal, multiply, screen, overlay, soft/hard light, add, difference, darken, lighten, dodge, burn).
- Preview blend mode memakai CSS `mix-blend-mode`; hasil export memakai `blend` + `maskedmerge` ffmpeg.

**Preview & export**

- Preview stage sinkron dengan playhead: video, audio, overlay, caption.
- Export MP4 H.264 + AAC 192k, presisi frame (re-encode).
- Encoder `auto` mencoba **AMD AMF** lebih dulu lalu fallback **libx264**; NVENC juga tersedia.
- Progress export/analisis real-time via SSE.
- Tombol buka folder hasil export di desktop.

## Stack

| Bagian | Teknologi |
|---|---|
| Desktop shell | Tauri 2 (Rust) + WebView2 |
| UI | Vue 3, TypeScript, Vite, Pinia, lucide-vue-next |
| Engine lokal | Go 1.26 (HTTP API + SQLite) |
| Media | ffmpeg/ffprobe 8.x |
| Speech-to-text | whisper.cpp (opsional, lokal) |
| AI opsional | DeepSeek (cut), TypeSafe (validasi), Gemini (QA) |
| Database | SQLite (`modernc.org/sqlite`, pure Go) |

## Prasyarat

- Windows 10/11 (target utama), WebView2 Runtime (biasanya sudah ada).
- **Go 1.26+**, **Node.js 20+**, **Rust toolchain** (`rustup`, MSVC build tools).
- **ffmpeg & ffprobe** tersedia di `PATH` (atau set `FFMPEG_PATH`).
- Opsional: whisper.cpp (`whisper-cli.exe`) + model `ggml-small.bin` untuk transkrip nyata.

## Menjalankan

### Mode desktop (Tauri)

```bash
npm install
npm --prefix web install
npm run dev
```

`npm run dev` akan:
1. Build engine Go ke `src-tauri/binaries/autocut-server-<target-triple>.exe`.
2. Menjalankan Vite dev server.
3. Membuka aplikasi Tauri; shell menyalakan sidecar di port acak dengan token acak.

### Build installer

```bash
npm run build
```

Menghasilkan installer di `src-tauri/target/release/bundle/` (MSI/NSIS).

### Mode browser (tanpa Tauri)

Berguna untuk debug UI cepat:

```bash
# terminal 1 — engine
go run ./cmd/server

# terminal 2 — UI
npm --prefix web run dev
```

UI memakai `VITE_API_URL` (default `http://127.0.0.1:8080`).

## Konfigurasi

Engine membaca `config.json` di working directory (opsional), lalu **environment variable menimpa** nilai file.

| Env | Default | Keterangan |
|---|---|---|
| `PORT` | `8080` | Port HTTP engine (Tauri memakai port acak). |
| `STORAGE_DIR` | `data` | Root database + project. Tauri memakai `%APPDATA%\com.txshd.autocut`. |
| `AUTOCUT_TOKEN` | kosong | Jika diisi, semua endpoint kecuali `/api/health` butuh header `X-Autocut-Token` atau `?token=`. |
| `FFMPEG_PATH` | `ffmpeg` | Path binary ffmpeg (ffprobe diasumsikan satu folder). |
| `WHISPER_CLI_PATH` | `bin/whisper/whisper-cli.exe` | Binary whisper.cpp. |
| `WHISPER_MODEL_PATH` | `bin/whisper/models/ggml-small.bin` | Model ggml. |
| `DEEPSEEK_API_KEY` | kosong | Saran cut AI. |
| `TYPESAFE_API_KEY` | kosong | Validasi cutlist. |
| `GEMINI_API_KEY` | kosong | QA cutlist. |
| `USE_NVENC` | `true` | Preferensi encoder (opsional). |

Di mode desktop, letakkan whisper di:

```
%APPDATA%\com.txshd.autocut\bin\whisper\whisper-cli.exe
%APPDATA%\com.txshd.autocut\bin\whisper\models\ggml-small.bin
```

Jika whisper tidak ditemukan, transkrip memakai **mock transcript** yang ditandai jelas di UI — auto-cut silence tetap berfungsi, tetapi saran berbasis teks dan caption tidak realistis.

## Shortcut

| Tombol | Aksi |
|---|---|
| `Space` | Play/pause |
| `Ctrl+Z` / `Ctrl+Shift+Z` / `Ctrl+Y` | Undo / redo |
| `Ctrl+B` | Split clip di playhead |
| `Delete` / `Backspace` | Hapus clip terpilih |
| `←` / `→` | Geser playhead satu frame |

## Struktur Singkat

```
cmd/server/            entrypoint engine Go
internal/
  api/                 HTTP API, SSE task, auth token
  project/             folder project, import, thumbnail, waveform
  render/              filter graph ffmpeg, overlay, caption ASS
  timeline/            apply cut & split murni
  captions/            generator caption dari transkrip
  pipeline/            orkestrasi auto-cut (ffmpeg + AI)
  stt/ whisper.go      whisper.cpp runner + fallback mock
web/                   UI Vue 3 + TypeScript
src-tauri/             shell Tauri, sidecar, capability, ikon
scripts/               build sidecar Go
legacy/web-svelte/     UI Svelte lama (arsip lokal, tidak dipakai)
```

## Batasan yang Diketahui

- **Blend mode caption hanya berlaku di preview.** File ASS tidak mendukung blend; caption hasil export selalu solid.
- **Audio video tidak otomatis masuk export.** Export hanya membaca track Audio. UI menambahkan clip audio otomatis saat video ber-audio dimasukkan, tetapi jika clip audio dihapus, suara video tidak ikut.
- Satu track video dan satu track audio utama; overlay gambar/teks didukung, overlay video belum.
- Analisis dan export berjalan per project; hasil analisis terakhir disimpan di memori engine (transkrip tetap tersimpan di SQLite).
- Edit timeline disimpan sebagai satu blob JSON; cocok untuk project personal, belum untuk kolaborasi real-time.

## Lisensi & Penggunaan

Proyek internal untuk channel TxShD. Media pengguna tidak dikirim ke pihak ketiga kecuali fitur AI opsional diaktifkan (teks transkrip dikirim ke DeepSeek/TypeSafe/Gemini).
