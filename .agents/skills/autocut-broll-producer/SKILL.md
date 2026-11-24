---
name: autocut-broll-producer
description: Produce end-to-end YouTube Shorts, TikTok, Reels, and video essays by combining DropLoad (YouTube search, metadata/chapter inspection, and full or clipped video material downloads related to the script), BRollEngine (Remotion 1080x1920 60fps Hook & B-Roll motion graphics), and AutoCut Workstation (voice-over auto-cut, word-level transcript, blurred 9:16 background fill for 16:9 gameplay/trailers, smart-gated captions, multi-track timeline assembly, live desktop UI sync, and hardware-accelerated MP4 export) via the unified autocut-mcp server.
---

# AutoCut + BRollEngine + DropLoad AI Video Producer (`autocut-broll-producer`)

Use this skill whenever the user asks to create, edit, assemble, or render a Short, Reel, TikTok, or video essay using **DropLoad** (`D:\Dev\.Projects\DropLoad`), **BRollEngine** (`D:\Dev\.Projects\BRollEngine`), and **AutoCut** (`D:\Dev\.Projects\AutoCut`).

---

## 1. Architecture & Tooling Overview

The unified **`autocut-mcp`** server (`D:\Dev\.Projects\AutoCut\bin\autocut-mcp.exe`) connects **DropLoad**, **BRollEngine**, and **AutoCut Workstation** into a single workflow and automatically syncs with the **AutoCut Workstation Desktop UI** (`%APPDATA%\com.txshd.autocut`).

- **MCP Tools** (available via native MCP or CLI `D:\Dev\.Projects\AutoCut\bin\autocut-mcp.exe --call <tool> --args-file <json>`):
  - **DropLoad Material Sourcing Tools** (`DROPLOAD_DIR=D:\Dev\.Projects\DropLoad`):
    - `dropload_status` — Verify DropLoad directory, `yt-dlp`/`ffmpeg`/`cookies.txt` paths, material storage folder, and optional live DropLoad HTTP MCP API (`http://localhost:3001/mcp`) status.
    - `dropload_search_youtube` — Search YouTube for relevant gameplay, trailers, interviews, or news B-roll candidates matching each beat of the script (supports `max_results`, `min_duration`, `max_duration`, `exclude_live`).
    - `dropload_extract_info` — Inspect video metadata, duration, **chapters/timestamps**, subtitles, and available formats before downloading.
    - `dropload_download_material` — Download a YouTube/media video (full or exact `clip_start`..`clip_end` range) via DropLoad into `DropLoad/storage/downloads/materials/<topic>/`, with optional direct auto-import into an AutoCut `project_id` and optional `extract_broll_bg: true` to also generate a muted 1280×720 parallax background in `BRollEngine/public/videos/<topic>/`.
    - `dropload_batch_download_materials` — Download multiple script-beat YouTube materials in one batch (accepts direct `url` or automatic `query` search per beat) and auto-import all assets into an AutoCut `project_id`.
    - `dropload_list_materials` — List previously downloaded video/audio materials stored in DropLoad (filterable by `topic`) for reuse without re-downloading.
  - **BRollEngine Tools**:
    - `broll_list_compositions` — List all Remotion compositions (`ImpactPoster`, `HookScene`, `ArrowHook`, `BlurPoster`, `DeskriptifBroll`, `DynamicBroll`, `GameDeal`, `PrizePool`, `PriceRamp`, `CareerTimeline`, etc.).
    - `broll_extract_bg_video` — Cut a muted `960×540` snippet (`±4.5s`) from source `.mkv`/`.mp4` footage into `BRollEngine/public/videos/<topic>/<slug>.mp4` for `DeskriptifBroll` parallax backgrounds. **Always pass `pre_blur: true`** (and set `blur: 0, dim: 0` in the spec) so FFmpeg bakes blur/dim in `<0.5s` instead of slow per-frame CSS blur in Chrome.
    - `broll_write_spec` — Save a composition spec JSON into `BRollEngine/specs/topics/<topic>/<name>.json` (set `"sfx": false` so B-Roll clips stay muted and render faster).
    - `broll_render_still` — Render a PNG/JPG frame for safe-area QA or cover thumbnail (`<id>-Cover.jpg`).
    - `broll_render_clip` / `broll_render_batch` — Render `1080×1920` MP4 clips (**prefer `broll_render_batch` with `fps: 30`** to share 1 Webpack bundle + GPU ANGLE for 10×–20× faster renders) and auto-import them into an AutoCut `project_id`.
  - **AutoCut Tools**:
    - `autocut_create_project` — Create a project (default `1080×1920 @ 60fps`).
    - `autocut_import_asset` — Import `.wav`, `.mp3`, `.mp4`, `.mkv`, `.png`, `.jpg` files into the project (creates thumbnails & waveforms).
    - `autocut_analyze_vo` — Run silence detection + `whisper.cpp` transcription + filler/retake detection on the voice-over asset.
    - `autocut_apply_cuts` — Apply cuts to a voice-over clip with ripple.
    - `autocut_set_scene_transitions` — Apply transitions across all video scene cuts (`mode: "variety"` or a specific type: `pull_in`, `pull_out`, `blink`, `flash`, `fade`, `slide_up`, `slide_down`, `slide_right`, `slide_left`, `spin_in`).
    - `autocut_assemble_short` — **Composite Timeline Assembler**: builds the complete multi-track Timeline (Voice-Over + B-Roll inserts with built-in SFX + 16:9 gameplay/trailer cuts with `background_fill: "blur"` + automatic scene transitions `scene_transition: "variety"` + smart-gated captions + optional BGM) in one call.
    - `autocut_upsert_clip` / `autocut_update_timeline` / `autocut_generate_captions` — Fine-grained timeline and caption manipulation.
    - `autocut_export_project` — Render the final MP4 with hardware encoder fallback (`h264_amf` → `libx264`).

---

## 2. Standard Production Pipeline (The `1105.mp4` Formula)

Follow these 7 steps to produce a high-retention vertical Short (`1080×1920 @ 60fps`):

### Step 1 — Create Project & Analyze Voice-Over / Script
1. Call `autocut_create_project` with `name`, `width: 1080`, `height: 1920`, `fps: 60`.
2. Call `autocut_import_asset` to import the raw voice-over (e.g. `#audio.wav`) and any local B-roll footage files already on disk.
3. Call `autocut_analyze_vo` on the voice-over asset to obtain the word-level transcript (`words[]` with `start` and `end` timestamps) and suggested `cuts[]`.

### Step 2 — Source & Download YouTube Video Materials via DropLoad (`D:\Dev\.Projects\DropLoad`)
Whenever raw footage for the script/transcript is not yet on disk (or extra visual beats are needed):
1. Check existing downloaded materials first with `dropload_list_materials` (filtered by `topic`) so you reuse local footage instead of re-downloading.
2. Break the script / voice-over transcript into key visual beats (e.g. game reveal trailer, gameplay clip, tournament highlight, news moment).
3. For each beat, call `dropload_search_youtube` with a focused query (e.g. `"Among Us official gameplay trailer no commentary"`, `max_duration: 900`, `exclude_live: true`) to find clean, high-resolution candidate videos.
4. Call `dropload_extract_info` on chosen URLs to inspect **chapters**, exact timestamps, and formats.
5. Download full videos or targeted clip ranges (`clip_start` / `clip_end`) with `dropload_download_material` (or `dropload_batch_download_materials` for multiple beats at once), setting:
   - `topic`: topic slug (saved under `D:\Dev\.Projects\DropLoad\storage\downloads\materials\<topic>\`)
   - `project_id`: AutoCut project ID (auto-imports downloaded clips straight into AutoCut & syncs the Desktop UI)
   - `quality`: `"1080"` (or `"720"` for fast B-roll)
   - `extract_broll_bg: true` on beats that also need a muted 1280×720 parallax background in `BRollEngine/public/videos/<topic>/`.

### Step 3 — Map Transcript Beats to Hook & B-Roll Inserts
Inspect the word timestamps from `autocut_analyze_vo` and structure the visual timeline:
1. **Opening Hook (`0.0s – ~2.5s`)**:
   - Use `ImpactPoster`, `HookScene`, `ArrowHook`, or `BlurPoster` for the opening sentence/question (e.g., *"Kenapa Among Us baru viral 2 tahun kemudian?"*).
   - Render a cover thumbnail (`<id>-Cover.jpg`) using `broll_render_still`.
2. **Main Narrative Points (`2–4 inserts`, `~4.4s` each)**:
   - Map each key narrative point to a `DeskriptifBroll` (or `DynamicBroll` / data card `StatCounter` / `GameDeal` / `PrizePool`).
   - For `DeskriptifBroll`:
     - Extract a `4.5s` background video from the downloaded/local `.mp4`/`.mkv` footage using `broll_extract_bg_video` (or via `extract_broll_bg: true` in `dropload_download_material`).
     - Provide a transparent full-body character cutout PNG (`character.src`), `kicker`, `title` (≤ 8 words) + `titleHighlight`, `text` (12–18 words) + `textHighlight`, and `note`.
     - Alternate `characterSide` (`"right"` → `"left"` → `"right"`) and accent colors across points to keep visual variety.
3. **Gameplay / Trailer Cutaways (between B-Roll cards)**:
   - Fill every gap between the Hook and the `DeskriptifBroll` cards with `2.0s – 4.0s` action cuts from the imported `.mp4`/`.mkv` gameplay or trailer assets.

### Step 4 — Render Hook & B-Roll Clips in BRollEngine (Fast Batch Mode)
For each spec (`<topic>-01.json` .. `<topic>-04.json`):
1. Write the specs via `broll_write_spec` with `"sfx": false`, `"blur": 0, "dim": 0` (paired with `pre_blur: true` from `broll_extract_bg_video`).
2. Render all clips in **one single bundle** via `broll_render_batch` with `fps: 30`, `concurrency: 3`, and `project_id` set (and `skip_if_exists: true` if already rendered).

### Step 5 — Assemble the Multi-Track Timeline (`autocut_assemble_short`)
Call `autocut_assemble_short` with:
- **`voiceover`**: `{ "asset_id": "<vo_asset_id>", "volume": 1.0, "cuts": [...] }` — **always trim long silence pauses** (`> 0.35s`) leaving only tight `~0.15s–0.20s` natural breaths and removing trailing dead air so pacing stays punchy with zero awkward gaps!
- **`visual_segments`**:
  - **For BRollEngine Opening Hook segments (`ImpactPoster`, `HookScene`, `ArrowHook`, `BlurPoster`)**:
    - `suppress_captions: true` — automatically hides AutoCut captions during the Hook so they never overlap Hook typography!
    - `keep_audio: true`, `volume: 0.9` — **keep Opening Hook SFX unmuted** (`"sfx": true` in Hook spec) for punchy intro impact!
    - `background_fill: ""` (already native 9:16 `1080×1920`).
  - **For BRollEngine B-Roll card segments (`DeskriptifBroll`, `DynamicBroll`, data cards)**:
    - `suppress_captions: true` — automatically hides AutoCut captions during this segment so they never overlap BRollEngine's built-in text panels!
    - `keep_audio: false`, `volume: 0` — **mute B-Roll cards** (and set `"sfx": false` in B-Roll specs) so they never distract from the Voice-Over!
    - `background_fill: ""` (already native 9:16 `1080×1920`).
  - **For 16:9 Gameplay / Trailer `.mp4`/`.mkv` segments**:
    - `suppress_captions: false` — captions appear over gameplay/trailer segments.
    - `keep_audio: false`, `volume: 0` — mutes raw gameplay/trailer audio so it never clashes with the voice-over.
    - `background_fill: "blur"` — fills the top and bottom of the 9:16 frame with a blurred, dimmed 9:16 duplicate of the video instead of black bars!
- **`caption_style`**:
  - Use dark translucent background box (`background_color: "#000000"`, `background_alpha: 0.68`, `font_family: "Merriweather"` or `"Plus Jakarta Sans"`, `font_size: 46`, `position_y: 0.78`, `words_per_chunk: 5`, `highlight: false` for editorial box style, or `highlight: true` for karaoke highlight).

### Step 6 — Apply CapCut Transitions & Impact SFX (`D:\Videos\TxShd\Sounds`)
1. Every scene cut on the timeline **MUST** use a CapCut two-phase transition paired with a matching impact sound effect from `D:\Videos\TxShd\Sounds` on the `"Transition SFX"` (`id: "audio-sfx"`) track.
2. `autocut_assemble_short` (with `scene_transition: "variety"` by default) and `autocut_set_scene_transitions` (with `with_sfx: true` by default) automatically import the required sound files from `D:\Videos\TxShd\Sounds` and place timed SFX clips at each scene cut boundary.
3. Always verify that `transition_sfx_count > 0` in the response (or run `autocut_set_scene_transitions` with `with_sfx: true` if transitions were modified manually).

### Step 7 — Export & Verify
1. Call `autocut_export_project` with `project_id`, `filename`, and optional external `output_path`.
2. Verify that `width == 1080`, `height == 1920`, `fps == 60`, `has_audio == true`, and `duration_sec` matches the voice-over timeline.
3. Inform the user that the project is also live and editable inside the **AutoCut Workstation** desktop app.

---

## 3. Mandatory Rule: CapCut Transitions + Impact SFX (`D:\Videos\TxShd\Sounds`)

Every scene-to-scene transition in AutoCut operates as a **CapCut Two-Phase Transition**:
- **Phase A (Outgoing Tail of Scene $A$, $[t_{\text{cut}} - h_{\text{out}}, t_{\text{cut}}]$)**: Scene $A$ accelerates into the cut with quadratic ease-in ($u^2$).
- **Phase B (Incoming Head of Scene $B$, $[t_{\text{cut}}, t_{\text{cut}} + h_{\text{in}}]$)**: Scene $B$ decelerates out of the cut with quadratic ease-out ($r^2$).
- **Impact SFX (`audio-sfx` track)**: Every transition **MUST** be accompanied by its matching sound effect from `D:\Videos\TxShd\Sounds`, pre-rolled slightly before $t_{\text{cut}}$ (`lead`) so the peak transient lands directly on the visual cut:

| Transition Type | CapCut Visual Choreography (Outgoing $A \to$ Incoming $B$) | Mandatory Sound File (`D:\Videos\TxShd\Sounds\`) | Trim & Sync (`in`, `dur`, `lead`, `vol`) |
| :--- | :--- | :--- | :--- |
| **`pull_in`** | $A$ zooms in $1.0\times \to 1.36\times$ ($u^2$); $B$ settles $1.28\times \to 1.0\times$ ($r^2$) | `punch.mp3` | `in: 0.00s, dur: 0.35s, lead: 0.05s, vol: 0.54` |
| **`pull_out`** | $A$ pushes $1.0\times \to 1.18\times$ ($u^2$); $B$ pulls back $1.34\times \to 1.0\times$ ($r^2$) | `woosh.mp3` | `in: 0.24s, dur: 0.40s, lead: 0.15s, vol: 0.54` |
| **`blink`** | Horizontal eyelid bars close to black on $A$, then snap open on $B$ | `camera_zLdd1zp.mp3` | `in: 0.04s, dur: 0.45s, lead: 0.10s, vol: 0.55` |
| **`flash`** | $A$ ramps exposure to pure white flash; $B$ decays from white flash + $1.10\times \to 1.0\times$ | `camera_zLdd1zp.mp3` | `in: 0.00s, dur: 0.58s, lead: 0.08s, vol: 0.58` |
| **`slide_up`** | $A$ whips upward ($-42\%$, $1.14\times$); $B$ whips in from bottom ($+42\% \to 0$, $1.14\times \to 1.0\times$) | `woosh.mp3` | `in: 0.26s, dur: 0.36s, lead: 0.14s, vol: 0.55` |
| **`slide_down`** | $A$ whips downward ($+42\%$, $1.14\times$); $B$ whips in from top ($-42\% \to 0$, $1.14\times \to 1.0\times$) | `woosh.mp3` | `in: 0.26s, dur: 0.36s, lead: 0.14s, vol: 0.55` |
| **`slide_left`** | $A$ whips left ($-42\%$, $1.14\times$); $B$ whips in from right ($+42\% \to 0$, $1.14\times \to 1.0\times$) | `woosh.mp3` | `in: 0.26s, dur: 0.36s, lead: 0.14s, vol: 0.55` |
| **`slide_right`** | $A$ whips right ($+42\%$, $1.14\times$); $B$ whips in from left ($-42\% \to 0$, $1.14\times \to 1.0\times$) | `woosh.mp3` | `in: 0.26s, dur: 0.36s, lead: 0.14s, vol: 0.55` |
| **`spin_in`** | $A$ spins $+16^\circ$ & zooms $1.28\times$; $B$ settles from $-16^\circ \to 0^\circ$ & $1.28\times \to 1.0\times$ | `dbz-teleport.mp3` | `in: 0.02s, dur: 0.48s, lead: 0.10s, vol: 0.50` |
| **`fade`** | $A$ dips to black ($1 \to 0$); $B$ emerges from black ($0 \to 1$) | `pop_7e9Is8L.mp3` | `in: 0.00s, dur: 0.26s, lead: 0.04s, vol: 0.52` |
