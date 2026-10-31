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
- Gaya caption: font (17 keluarga Google Fonts OFL bawaan + font sistem), ukuran, bold/italic, warna, stroke, shadow, uppercase, posisi vertikal, highlight karaoke, jumlah kata per potongan.
- Preview caption real-time di stage editor.
- Caption di-export sebagai hardsub ASS.

**Overlay teks & gambar**

- Overlay teks dengan font (Google Fonts OFL bawaan + sistem), stroke, shadow, background, rotasi, opacity; bisa digeser dan di-scale langsung di preview.
- Overlay gambar dengan posisi, skala, rotasi, opacity, dan blend mode (normal, multiply, screen, overlay, soft/hard light, add, difference, darken, lighten, dodge, burn).
- Overlay teks/gambar selalu tampil **di atas** semua track video (di preview maupun hasil export), berapa pun urutan track-nya; dalam satu jenis track, track paling atas adalah lapisan teratas.
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

**Untuk memakai aplikasi yang sudah terpasang** (tanpa install toolchain):

- Windows 10/11 + WebView2 Runtime (Windows 11 biasanya sudah punya).
- **ffmpeg & ffprobe** tersedia di `PATH`. Cek dengan `ffmpeg -version`.
- Opsional: whisper.cpp + model untuk transkrip nyata; API key DeepSeek/TypeSafe/Gemini untuk fitur AI.

**Untuk development atau build dari source:**

- **Go 1.26+**, **Node.js 20+**, **Rust toolchain** (`rustup` + MSVC build tools).
- ffmpeg/ffprobe di `PATH` (atau set `FFMPEG_PATH`).

## Cara Menjalankan

### A. Pakai aplikasi terpasang (cara normal)

1. Tutup aplikasi jika sedang berjalan, lalu jalankan installer:
   - `src-tauri/target-build/release/bundle/nsis/AutoCut Workstation_0.1.3_x64-setup.exe`, atau
   - `src-tauri/target-build/release/bundle/msi/AutoCut Workstation_0.1.3_x64_en-US.msi`.
2. Buka **AutoCut Workstation** dari Start Menu. Bisa juga langsung:
   `%LOCALAPPDATA%\AutoCut Workstation\autocut-workstation.exe`.
3. Engine Go menyala otomatis sebagai sidecar (port acak + token acak). **Tidak perlu** membuka terminal atau menjalankan server manual.
4. Alur pertama kali:
   - Klik **Project Baru** → pilih resolusi & fps.
   - Impor video/audio/gambar dari panel Media.
   - Seret media ke track timeline, atur trim/posisi.
   - `Ctrl+S` untuk memaksa simpan (autosave juga berjalan otomatis).
   - **Export** untuk render MP4, lalu **Buka folder** untuk mengambil hasilnya.
5. Lokasi data: `%APPDATA%\com.txshd.autocut` (database + folder project). Uninstall **tidak** menghapus data ini.

Uninstall: Settings → Apps → AutoCut Workstation, atau jalankan `uninstall.exe` di folder instalasi.

### B. Mode development (Tauri + hot reload)

```bash
npm install
npm --prefix web install
npm run dev
```

`npm run dev` akan:
1. Build engine Go ke `src-tauri/binaries/autocut-server-<target-triple>.exe`.
2. Menjalankan Vite dev server (hot reload UI).
3. Membuka window Tauri; shell menyalakan sidecar engine secara otomatis.

### C. Build installer sendiri

```bash
CARGO_TARGET_DIR=target-build npm run build
```

Butuh Rust toolchain. Hasil build:

| File | Fungsi |
|---|---|
| `src-tauri/target-build/release/autocut-workstation.exe` | Aplikasi utama (portable) |
| `src-tauri/target-build/release/autocut-server.exe` | Engine Go (sidecar) — **wajib ada di folder yang sama** |
| `src-tauri/target-build/release/bundle/nsis/AutoCut Workstation_0.1.3_x64-setup.exe` | Installer NSIS |
| `src-tauri/target-build/release/bundle/msi/AutoCut Workstation_0.1.3_x64_en-US.msi` | Installer MSI |

### D. Jalankan .exe hasil build tanpa installer (portable)

1. Buka folder `src-tauri/target-build/release/`.
2. Jalankan `autocut-workstation.exe` (double-click, atau dari terminal):
   ```bash
   ./src-tauri/target-build/release/autocut-workstation.exe
   ```
3. Jangan pindahkan `autocut-workstation.exe` sendirian — `autocut-server.exe` harus tetap satu folder karena Tauri menyalakannya sebagai sidecar. Kalau ingin dipindah, salin **kedua file** bersama-sama.
4. Tidak perlu install, tidak muncul di Start Menu, dan tidak ada uninstaller. Untuk menghapus, tutup aplikasi lalu hapus foldernya. Data project tetap tersimpan di `%APPDATA%\com.txshd.autocut`.
5. Untuk update portable: tutup aplikasi, timpa kedua file `.exe` dengan hasil build baru.

Syaratnya tetap sama: ffmpeg/ffprobe harus ada di `PATH`. Jika `autocut-server.exe` tidak ditemukan di sebelah aplikasi, window akan menampilkan "Engine belum siap".

### E. Mode browser (tanpa Tauri, untuk debug UI)

```bash
# terminal 1 — engine
go run ./cmd/server

# terminal 2 — UI
npm --prefix web run dev
```

Buka `http://localhost:5173`. UI memakai `VITE_API_URL` (default `http://127.0.0.1:8080`); set jika engine berjalan di port lain:

```bash
VITE_API_URL=http://127.0.0.1:8099 npm --prefix web run dev
```

### Troubleshooting singkat

| Gejala | Penyebab / solusi |
|---|---|
| Layar "Engine belum siap" di app | Sidecar gagal start atau ffmpeg tidak ditemukan. Cek `ffmpeg -version` di `PATH`. |
| Port 8080 sudah dipakai (mode browser) | Jalankan engine dengan `PORT=8099`, lalu set `VITE_API_URL` sesuai. |
| Transkrip ditandai **mock** | whisper.cpp/model tidak ditemukan; silence cut tetap jalan, caption tidak realistis. |
| Export gagal encoder | Pilih encoder `libx264` di dialog export (AMF/NVENC butuh GPU/driver sesuai). |

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
| `Ctrl+S` | Simpan timeline sekarang |
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
