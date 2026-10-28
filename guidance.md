Panduan Operasional & Prinsip Desain (TxShD Video Pipeline)

Dokumen ini berisi panduan tingkat tinggi, prinsip desain, dan aturan operasional untuk pengembangan dan penggunaan tool internal TxShD.

1. Prinsip Utama Sistem

Sistem ini dirancang dengan pendekatan yang sangat spesifik untuk menjaga privasi, menghemat biaya, dan mengoptimalkan resource:

100% Local Execution: Tool ini tidak boleh di-deploy ke layanan cloud seperti Railway, VPS, atau server publik mana pun. Aplikasi harus selalu berjalan secara lokal di mesin kreator.

Manajemen Data & Storage:

Data Sementara (Ephemeral): Video mentah (raw) dan video hasil render (final) berukuran besar dan hanya dibutuhkan sementara. File-file ini harus dihapus secara otomatis dari sistem setelah didownload oleh user.

Data Permanen: Hanya file ringan dan krusial yang disimpan secara permanen, yaitu: Audio hasil cut, Transcript STT (format JSON), dan Cutlist/metadata (format JSON).

2. Strategi Penggunaan AI (Alokasi Peran)

Sistem menggunakan beberapa AI dan alat pemrosesan dengan tugas yang dipisah secara ketat berdasarkan efisiensi dan biaya:

FFmpeg (Non-AI): Digunakan murni untuk deteksi silence (jeda kosong) berdasarkan energi audio. Ini adalah cara termurah dan tercepat tanpa memerlukan transkripsi.

DeepSeek API (Cloud): Bertanggung jawab atas "Reasoning" teks skala besar. DeepSeek digunakan untuk mendeteksi filler word, kalimat menggantung, dan pengulangan take. Pemanggilan API ke DeepSeek akan tinggi karena perannya mengevaluasi draf transcript, sehingga menggunakan provider dengan billing per-token yang jelas sangat direkomendasikan.

Gemini API (Google AI Studio): Penggunaan Gemini harus diminimalkan. API key ini terhubung ke akun Google AI Pro yang memiliki limitasi kuota harian dan bukan billing API murni.

Hanya panggil Gemini 1-2 kali per video.

Tugas Gemini dibatasi khusus untuk: (1) QA Pass (Sanity check final pada cutlist) dan (2) Generate metadata YouTube (Judul, Deskripsi, Chapter timestamps).

3. Peringatan Limitasi & Kuota

Sebelum melakukan eskalasi sistem atau menambah fitur yang menggunakan Gemini, wajib melakukan pengecekan kuota dan rate limit API key di dashboard Google AI Studio. Asumsi bahwa langganan AI Pro memberikan kuota berlimpah di luar UI (gemini.google.com) tidak selalu benar dan harus diverifikasi secara berkala.