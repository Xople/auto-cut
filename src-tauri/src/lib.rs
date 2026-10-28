use std::net::TcpListener;
use std::path::PathBuf;
use std::sync::Mutex;

use rand::RngCore;
use serde::Serialize;
use tauri::{Manager, RunEvent};
use tauri_plugin_shell::process::{CommandChild, CommandEvent};
use tauri_plugin_shell::ShellExt;

#[derive(Clone, Serialize)]
struct ApiInfo {
    base_url: String,
    token: String,
}

struct SidecarState(Mutex<Option<CommandChild>>);

#[tauri::command]
fn api_info(info: tauri::State<'_, ApiInfo>) -> ApiInfo {
    info.inner().clone()
}

#[tauri::command]
fn reveal_export(app: tauri::AppHandle, project_id: String, output_name: String) -> Result<(), String> {
    let safe_id = !project_id.is_empty()
        && project_id
            .chars()
            .all(|c| c.is_ascii_alphanumeric() || matches!(c, '-' | '_'));
    let safe_name = !output_name.is_empty()
        && !output_name.contains("..")
        && !output_name.contains('/')
        && !output_name.contains('\\');
    if !safe_id || !safe_name {
        return Err("nama file export tidak valid".into());
    }
    let path = data_dir(&app)
        .join("projects")
        .join(&project_id)
        .join("exports")
        .join(&output_name);
    if !path.exists() {
        return Err(format!("file export tidak ditemukan: {}", path.display()));
    }

    #[cfg(target_os = "windows")]
    {
        std::process::Command::new("explorer")
            .arg(format!("/select,{}", path.display()))
            .spawn()
            .map_err(|e| e.to_string())?;
    }
    #[cfg(target_os = "macos")]
    {
        std::process::Command::new("open")
            .arg("-R")
            .arg(&path)
            .spawn()
            .map_err(|e| e.to_string())?;
    }
    #[cfg(all(unix, not(target_os = "macos")))]
    {
        let folder = path.parent().unwrap_or(&path);
        std::process::Command::new("xdg-open")
            .arg(folder)
            .spawn()
            .map_err(|e| e.to_string())?;
    }
    Ok(())
}

fn free_port() -> u16 {
    TcpListener::bind("127.0.0.1:0")
        .and_then(|listener| listener.local_addr())
        .map(|addr| addr.port())
        .unwrap_or(8137)
}

fn random_token() -> String {
    let mut bytes = [0u8; 24];
    rand::thread_rng().fill_bytes(&mut bytes);
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}

fn data_dir(app: &tauri::AppHandle) -> PathBuf {
    app.path()
        .app_data_dir()
        .unwrap_or_else(|_| std::env::temp_dir().join("autocut-workstation"))
}

fn stop_sidecar(app: &tauri::AppHandle) {
    if let Some(state) = app.try_state::<SidecarState>() {
        if let Ok(mut guard) = state.0.lock() {
            if let Some(child) = guard.take() {
                let _ = child.kill();
            }
        }
    }
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let app = tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .invoke_handler(tauri::generate_handler![api_info, reveal_export])
        .setup(|app| {
            let port = free_port();
            let token = random_token();
            let data = data_dir(app.handle());
            let _ = std::fs::create_dir_all(&data);

            app.manage(ApiInfo {
                base_url: format!("http://127.0.0.1:{port}"),
                token: token.clone(),
            });

            let mut command = app
                .shell()
                .sidecar("autocut-server")?
                .env("PORT", port.to_string())
                .env("STORAGE_DIR", data.to_string_lossy().to_string())
                .env("AUTOCUT_WATCH_STDIN", "1")
                .env("AUTOCUT_TOKEN", token);

            if std::env::var_os("WHISPER_CLI_PATH").is_none() {
                command = command.env(
                    "WHISPER_CLI_PATH",
                    data.join("bin/whisper/whisper-cli.exe").to_string_lossy().to_string(),
                );
            }
            if std::env::var_os("WHISPER_MODEL_PATH").is_none() {
                command = command.env(
                    "WHISPER_MODEL_PATH",
                    data.join("bin/whisper/models/ggml-small.bin").to_string_lossy().to_string(),
                );
            }

            match command.spawn() {
                Ok((mut events, child)) => {
                    app.manage(SidecarState(Mutex::new(Some(child))));
                    tauri::async_runtime::spawn(async move {
                        while let Some(event) = events.recv().await {
                            match event {
                                CommandEvent::Stdout(line) => {
                                    println!("[sidecar] {}", String::from_utf8_lossy(&line));
                                }
                                CommandEvent::Stderr(line) => {
                                    eprintln!("[sidecar] {}", String::from_utf8_lossy(&line));
                                }
                                CommandEvent::Terminated(payload) => {
                                    eprintln!("[sidecar] exited: {:?}", payload.code);
                                    break;
                                }
                                _ => {}
                            }
                        }
                    });
                }
                Err(err) => {
                    eprintln!("[autocut] gagal menjalankan sidecar: {err}");
                    app.manage(SidecarState(Mutex::new(None)));
                }
            }
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("error while building AutoCut Workstation");

    app.run(|app_handle, event| match event {
        RunEvent::ExitRequested { .. } | RunEvent::Exit => stop_sidecar(app_handle),
        _ => {}
    });
}
