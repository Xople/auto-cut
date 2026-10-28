# PRD — AutoCut Workstation

| Field | Isi |
|---|---|
| Produk | AutoCut Workstation |
| Versi | 0.1.0 (MVP) |
| Pemilik | TxShD |
| Platform | Windows 10/11 desktop (Tauri) |
| Status | MVP implemented |

## 1. Latar Belakang

Produksi video essay TxShD saat ini memakai pipeline auto-cut terpisah dari proses editing. Voice-over harus diproses, cutlist diterapkan, lalu caption dan visual pendukung dikerjakan di editor lain. Perpindahan antar-tool memakan waktu dan menyulitkan iterasi karena perubahan kecil pada potongan harus mengulang pipeline.

AutoCut Workstation menyatukan langkah tersebut dalam satu aplikasi desktop lokal:

- Impor cepat video, audio, dan gambar.
- Timeline bergaya CapCut untuk merapikan potongan dan overlay.
- Auto-cut voice-over dengan kontrol manual atas setiap saran potongan.
- Caption generator dengan font, stroke, dan highlight yang bisa diatur.
- Preview langsung dan export MP4 sekali klik.

## 2. Tujuan

1. Memangkas waktu produksi voice-over dari rekaman mentah sampai video siap upload.
2. Menjaga kontrol kreatif: setiap cut dan caption dapat diubah manual.
3. Berjalan 100% lokal; AI hanya dipakai jika pengguna mengaktifkannya.
4. Memberi pengalaman editing yang familiar bagi pengguna CapCut.

### Non-tujuan (MVP)

- Kolaborasi multi-user atau cloud sync.
- Editing multi-cam, motion graphics kompleks, atau color grading profesional.
- Overlay video bergerak sebagai layer terpisah.
- Publikasi langsung ke YouTube.

## 3. Persona & Kebutuhan

**Editor utama (TxShD).** Mengedit video essay 8–20 menit, voice-over dominan, visual berupa gambar/klip pendukung. Butuh: impor cepat, auto-cut yang bisa dikoreksi, caption rapi, export cepat di GPU AMD.

**Operator/editor tamu.** Mengikuti alur yang sama tanpa perlu paham ffmpeg. Butuh UI jelas, progress yang terlihat, dan pesan error yang bisa ditindaklanjuti.

## 4. Alur Utama

### 4.1 Membuat project

1. Buka aplikasi → launcher menampilkan daftar project + tombol **Project Baru**.
2. Isi nama, resolusi (1080p/1440p/vertical/square), dan FPS (24/30/60).
3. Masuk ke workspace editor.

### 4.2 Impor & menyusun timeline

1. Panel Media → impor file (picker atau drag-drop).
2. Klik **+** pada asset untuk menambah ke timeline, atau drag ke lane yang sesuai.
3. Video ber-audio otomatis menambah clip audio terpisah pada track Audio.
4. Geser/trim/split clip; snapping membantu perataan.
5. Atur overlay teks/gambar di panel Properti (posisi, skala, rotasi, opacity, blend).
6. Timeline autosave; undo/redo tersedia.

### 4.3 Auto-cut voice-over

1. Klik **Auto Cut**.
2. Pilih asset audio/video ber-audio.
3. Aktifkan/matikan AI cut dan QA (default aktif).
4. Jalankan analisis; progress tampil real-time.
5. Tinjau saran cut per alasan; centang yang ingin diterapkan.
6. **Terapkan** → clip di timeline dipadatkan, clip sesudahnya bergeser (ripple opsional).

Jika whisper tidak terpasang, UI menampilkan peringatan bahwa transkrip adalah mock.

### 4.4 Caption

1. Setelah transkrip tersedia, klik **Captions** (dari toolbar atau panel Caption).
2. Sistem membuat caption per potongan kata (default 3 kata), memetakan waktu kata ke timeline setelah trim/split/cut.
3. Gaya caption diatur di tab **Caption** (font, ukuran, warna, stroke, shadow, highlight, posisi, uppercase).
4. Preview stage menampilkan caption dan highlight kata aktif secara real-time.

### 4.5 Preview & export

1. Playhead menggerakkan video, audio, overlay, dan caption secara sinkron.
2. Klik **Export**, pilih nama file, resolusi, encoder, dan quality.
3. Progress render tampil; setelah selesai tersedia **Buka folder** dan **Unduh MP4**.

## 5. Kebutuhan Fungsional

| ID | Kebutuhan | Prioritas | Status |
|---|---|---|---|
| F1 | CRUD project dengan nama, resolusi, FPS | Must | Selesai |
| F2 | Impor video/audio/gambar via multipart atau path | Must | Selesai |
| F3 | Thumbnail video/gambar dan waveform audio | Must | Selesai |
| F4 | Timeline multi-track: move, trim, split, delete, snap, zoom | Must | Selesai |
| F5 | Undo/redo dan autosave timeline | Must | Selesai |
| F6 | Analisis auto-cut (silence + whisper + AI opsional) dengan progress SSE | Must | Selesai |
| F7 | Saran cut dapat dicentang; apply dengan ripple | Must | Selesai |
| F8 | Generator caption dari transkrip + editor gaya caption | Must | Selesai |
| F9 | Overlay teks: font, stroke, shadow, background, blend | Must | Selesai |
| F10 | Overlay gambar: posisi, skala, rotasi, opacity, blend | Must | Selesai |
| F11 | Preview sinkron dengan playhead dan caption karaoke | Must | Selesai |
| F12 | Export MP4 H.264/AAC dengan fallback encoder | Must | Selesai |
| F13 | Buka folder hasil export di desktop | Should | Selesai |
| F14 | Pemilihan font dari font sistem | Must | Selesai |
| F15 | Blend mode caption di hasil export | Won't (MVP) | Tidak didukung ASS |
| F16 | Overlay video sebagai layer | Won't (MVP) | Roadmap |

## 6. Kebutuhan Non-Fungsional

- **Privasi:** media tetap lokal. Hanya teks transkrip yang dikirim ke penyedia AI jika fitur AI diaktifkan.
- **Keamanan:** engine hanya listen di `127.0.0.1`; token acak per sesi desktop membatasi akses; engine berhenti saat shell ditutup.
- **Kinerja:** import 1 GB harus selesai tanpa memblokir UI; preview memakai elemen HTML5 dan overlay DOM, bukan render ulang ffmpeg.
- **Ketahanan:** encoder AMD AMF gagal → fallback libx264; fitur AI gagal → pipeline tetap menghasilkan cutlist berbasis aturan.
- **Usability:** semua proses panjang menampilkan progress dan pesan; error ditampilkan di dialog terkait.
- **Kompatibilitas:** Windows 10/11 x64 dengan WebView2; ffmpeg 8.x.

## 7. Kriteria Penerimaan MVP

1. Pengguna dapat membuat project 1280×720/30fps, mengimpor video + audio + gambar, dan menaruhnya di timeline.
2. Analisis voice-over menghasilkan daftar cut; menerapkan cut mengubah durasi clip dan menggeser clip setelahnya.
3. Caption tergenerate dari transkrip dan mengikuti trim/split clip.
4. Preview menampilkan video, overlay teks/gambar (termasuk blend), dan caption pada waktu yang benar.
5. Export menghasilkan MP4 yang bisa diputar, dengan video, audio, overlay, dan caption terbakar.
6. Project dan aset tetap ada setelah aplikasi ditutup dan dibuka kembali.
7. Menutup aplikasi (termasuk force kill) tidak meninggalkan proses engine yang masih listening.

## 8. Metrik Keberhasilan

- Waktu dari impor sampai export untuk video 10 menit < 15 menit di mesin target (720p/1080p, GPU AMD).
- ≥ 80% saran cut diterima tanpa koreksi pada rekaman voice-over bersih.
- Tidak ada kehilangan project/timeline setelah autosave (0 laporan korupsi data pada penggunaan normal).

## 9. Risiko & Mitigasi

| Risiko | Dampak | Mitigasi |
|---|---|---|
| whisper.cpp tidak terpasang | Transkrip mock, caption tidak bermakna | Peringatan jelas di UI + panduan instalasi |
| Model ggml besar / lambat di CPU | Analisis lama | Progress SSE, opsi mematikan AI, model small default |
| Kuota AI terbatas | Fitur AI gagal | Semua AI opsional; degradasi ke aturan lokal |
| Encoder GPU tidak tersedia | Export gagal | Fallback otomatis ke libx264 |
| Blend caption tidak didukung ASS | Hasil export beda dari preview | Peringatan di panel Caption + dokumentasi |
| File media dipindah/dihapus user | Clip tidak bisa dirender | Error per overlay/asset dengan pesan jelas (roadmap: relink media) |

## 10. Roadmap

**MVP+ (berikutnya)**

- Relink media yang hilang dan proxy untuk preview 4K.
- Overlay video sebagai layer dengan trim sendiri.
- Caption per-kata dengan animasi preset.
- Metadata YouTube (judul/deskripsi/chapter) dari transkrip.
- Riwayat export dan pengaturan default per project.

**Jangka panjang**

- Multi-track video (PIP) dan transisi.
- Kolaborasi/versioning project.
- Dukungan macOS/Linux.
