use std::collections::HashMap;
use std::fs;
use std::panic::{catch_unwind, AssertUnwindSafe};
use std::process::{Command, Stdio};
use std::sync::Arc;
use std::time::{Duration, Instant};

use maa_framework::controller::Controller;
use tauri::{AppHandle, State};

use super::types::{ControllerConfig, InstanceRuntime, MaaState};

fn marker() -> Result<std::path::PathBuf, String> {
    Ok(super::utils::get_exe_directory()?.join("config/experimental-input.enabled"))
}

#[tauri::command]
pub fn mda_experimental_status() -> bool {
    marker().map(|p| p.is_file()).unwrap_or(false)
}

#[tauri::command]
pub fn mda_set_experimental(enabled: bool) -> Result<(), String> {
    let _input_probe_guard = PROBE_TASK_GATE
        .try_read()
        .map_err(|_| "输入检测进行中，请稍后更改设置。")?;
    let path = marker()?;
    if enabled {
        fs::create_dir_all(path.parent().ok_or("配置目录无效")?).map_err(|e| e.to_string())?;
        if !path.is_file() {
            fs::OpenOptions::new()
                .write(true)
                .create_new(true)
                .open(path)
                .map_err(|e| e.to_string())?;
        }
    } else if path.exists() {
        fs::remove_file(path).map_err(|e| e.to_string())?;
    }
    Ok(())
}

const EXPERIMENTAL_METHODS: u64 = (1 << 11) | (1 << 12);
pub static PROBE_TASK_GATE: tokio::sync::RwLock<()> = tokio::sync::RwLock::const_new(());

fn is_experimental(config: &ControllerConfig) -> bool {
    matches!(config, ControllerConfig::Win32 { mouse_method, keyboard_method, .. }
        if (mouse_method | keyboard_method) & EXPERIMENTAL_METHODS != 0)
}

fn task_in_progress(instance: &InstanceRuntime) -> bool {
    instance.stop_in_progress
        || instance
            .tasker
            .as_ref()
            .is_some_and(|tasker| tasker.running())
        || instance.task_run_state.overall_status.as_deref() == Some("Running")
}

fn default_for(
    original: &ControllerConfig,
    template: &ControllerConfig,
) -> Result<ControllerConfig, String> {
    match (original, template) {
        (
            ControllerConfig::Win32 { handle, .. },
            ControllerConfig::Win32 {
                screencap_method,
                mouse_method,
                keyboard_method,
                display_short_side,
                ..
            },
        ) if (mouse_method | keyboard_method) & EXPERIMENTAL_METHODS == 0 => {
            Ok(ControllerConfig::Win32 {
                handle: *handle,
                screencap_method: *screencap_method,
                mouse_method: *mouse_method,
                keyboard_method: *keyboard_method,
                display_short_side: *display_short_side,
            })
        }
        _ => Err("无法切换到默认输入方式。".into()),
    }
}

fn connect(config: &ControllerConfig, app: Option<&AppHandle>) -> Result<Controller, String> {
    let ControllerConfig::Win32 {
        handle,
        screencap_method,
        mouse_method,
        keyboard_method,
        display_short_side,
    } = config
    else {
        return Err("输入方式不支持自动切换。".into());
    };
    let controller = Controller::new_win32(
        *handle as *mut std::ffi::c_void,
        *screencap_method,
        *mouse_method,
        *keyboard_method,
    )
    .map_err(|e| e.to_string())?;
    if let Some(app) = app {
        let app = app.clone();
        controller
            .add_sink(move |message, details| {
                super::utils::emit_callback_event(&app, message, details)
            })
            .map_err(|e| e.to_string())?;
    }
    controller
        .set_screenshot_target_short_side(display_short_side.unwrap_or(720))
        .map_err(|e| e.to_string())?;
    let id = controller.post_connection().map_err(|e| e.to_string())?;
    let deadline = Instant::now() + Duration::from_secs(8);
    loop {
        let status = controller.status(id);
        if status.is_success() {
            return Ok(controller);
        }
        if status.is_failed() || Instant::now() >= deadline {
            return Err("连接输入方式失败。".into());
        }
        std::thread::sleep(Duration::from_millis(25));
    }
}

fn run_probe(backend: &str) -> Result<String, String> {
    let root = super::utils::get_exe_directory()?;
    let mut cmd = Command::new(root.join("maafw/experimental-input-probe.exe"));
    cmd.current_dir(&root)
        .arg(backend)
        .arg(std::process::id().to_string())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped());
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        cmd.creation_flags(0x08000000);
    }
    let mut child = cmd.spawn().map_err(|e| format!("无法启动输入检测：{e}"))?;
    let deadline = Instant::now() + Duration::from_secs(10);
    loop {
        match child.try_wait() {
            Err(error) => {
                child
                    .kill()
                    .map_err(|e| format!("无法读取检测状态：{error}；无法结束检测：{e}"))?;
                child
                    .wait()
                    .map_err(|e| format!("无法读取检测状态：{error}；无法确认检测已结束：{e}"))?;
                return Err(format!("无法读取检测状态：{error}"));
            }
            Ok(Some(_)) => break,
            Ok(None) if Instant::now() >= deadline => {
                child
                    .kill()
                    .map_err(|e| format!("无法结束超时的检测：{e}"))?;
                child
                    .wait()
                    .map_err(|e| format!("无法确认检测已结束：{e}"))?;
                return Err("检测超时。".into());
            }
            Ok(None) => std::thread::sleep(Duration::from_millis(25)),
        }
    }
    let output = child
        .wait_with_output()
        .map_err(|e| format!("无法读取检测结果：{e}"))?;
    let text = if output.status.success() {
        &output.stdout
    } else {
        &output.stderr
    };
    let message = String::from_utf8_lossy(text).trim().to_owned();
    if output.status.success() {
        Ok(message)
    } else {
        Err(message)
    }
}

fn with_restore<T, S>(
    state: &mut S,
    attempt: impl FnOnce(&mut S) -> Result<T, String>,
    restore: impl FnOnce(&mut S) -> Result<(), String>,
) -> Result<T, String> {
    let result = catch_unwind(AssertUnwindSafe(|| attempt(state)))
        .unwrap_or_else(|_| Err("检测异常。".into()));
    let restored = catch_unwind(AssertUnwindSafe(|| restore(state)))
        .unwrap_or_else(|_| Err("恢复异常。".into()));
    match (result, restored) {
        (result, Ok(())) => result,
        (Ok(_), Err(error)) => Err(format!("恢复原输入方式失败：{error}")),
        (Err(error), Err(restore_error)) => {
            Err(format!("{error}；恢复原输入方式失败：{restore_error}"))
        }
    }
}

struct SavedController {
    instance_id: String,
    config: ControllerConfig,
}

struct ProbeSwitch<'a> {
    state: &'a Arc<MaaState>,
    saved: Vec<SavedController>,
    defaults: HashMap<ControllerConfig, Controller>,
    detached: bool,
    app: &'a AppHandle,
}

impl ProbeSwitch<'_> {
    fn switch_to_default(&mut self, template: Option<&ControllerConfig>) -> Result<(), String> {
        if self.saved.is_empty() {
            return Ok(());
        }
        let template = template.ok_or("找不到默认输入方式。")?;
        for saved in &self.saved {
            default_for(&saved.config, template)?;
        }
        let mut taskers = Vec::new();
        let mut controllers = Vec::new();
        {
            let mut instances = self.state.instances.lock().map_err(|e| e.to_string())?;
            let mut pool = self
                .state
                .controller_pool
                .lock()
                .map_err(|e| e.to_string())?;
            if self
                .saved
                .iter()
                .any(|saved| !instances.contains_key(&saved.instance_id))
            {
                return Err("找不到当前实例。".into());
            }
            self.detached = true;
            for saved in &self.saved {
                let instance = instances.get_mut(&saved.instance_id).unwrap();
                if let Some(tasker) = instance.tasker.take() {
                    taskers.push(tasker);
                }
                if let Some(controller) = instance.controller.take() {
                    controllers.push(controller);
                }
                instance.controller_config = None;
                if let Some(controller) = pool.remove(&saved.config) {
                    controllers.push(controller);
                }
            }
        }
        drop(taskers);
        drop(controllers);
        for saved in &self.saved {
            let config = default_for(&saved.config, template)?;
            if !self.defaults.contains_key(&config) {
                self.defaults
                    .insert(config.clone(), connect(&config, None)?);
            }
            let mut instances = self.state.instances.lock().map_err(|e| e.to_string())?;
            let instance = instances
                .get_mut(&saved.instance_id)
                .ok_or("找不到当前实例。")?;
            instance.controller = self.defaults.get(&config).cloned();
            instance.controller_config = Some(config);
        }
        Ok(())
    }

    fn restore(&mut self) -> Result<(), String> {
        if !self.detached {
            return Ok(());
        }
        let mut controllers = Vec::new();
        {
            let mut instances = self.state.instances.lock().map_err(|e| e.to_string())?;
            for saved in &self.saved {
                if let Some(instance) = instances.get_mut(&saved.instance_id) {
                    if let Some(controller) = instance.controller.take() {
                        controllers.push(controller);
                    }
                    instance.controller_config = None;
                }
            }
        }
        drop(controllers);
        self.defaults.clear();
        let mut restored: HashMap<ControllerConfig, Result<Controller, String>> = HashMap::new();
        let mut error = None;
        for saved in &self.saved {
            if !restored.contains_key(&saved.config) {
                restored.insert(saved.config.clone(), connect(&saved.config, Some(self.app)));
            }
        }
        let mut instances = self.state.instances.lock().map_err(|e| e.to_string())?;
        let mut pool = self
            .state
            .controller_pool
            .lock()
            .map_err(|e| e.to_string())?;
        for saved in &self.saved {
            if let Some(instance) = instances.get_mut(&saved.instance_id) {
                match restored.get(&saved.config) {
                    Some(Ok(controller)) => instance.controller = Some(controller.clone()),
                    Some(Err(e)) => error = Some(e.clone()),
                    None => error = Some("找不到原输入方式。".into()),
                }
                instance.controller_config = Some(saved.config.clone());
            }
        }
        for (config, result) in restored {
            if let Ok(controller) = result {
                pool.insert(config, controller);
            }
        }
        match error {
            Some(error) => Err(error),
            None => Ok(()),
        }
    }
}

#[tauri::command]
pub async fn mda_probe_experimental(
    app: AppHandle,
    state: State<'_, Arc<MaaState>>,
    backend: String,
    default_config: Option<ControllerConfig>,
) -> Result<String, String> {
    if !mda_experimental_status() {
        return Err("实验性输入尚未开启".into());
    }
    if backend != "Logitech" && backend != "Razer" {
        return Err("输入类型无效".into());
    }
    let gate = PROBE_TASK_GATE
        .try_write()
        .map_err(|_| "任务正在准备，请稍后检测。")?;
    if !mda_experimental_status() {
        return Err("实验性输入尚未开启".into());
    }
    let state = state.inner().clone();
    tauri::async_runtime::spawn_blocking(move || {
        let _gate = gate;
        let saved = {
            let instances = state.instances.lock().map_err(|e| e.to_string())?;
            if instances.values().any(task_in_progress) {
                return Err("任务运行中，无法检测输入。".into());
            }
            let mut saved = Vec::new();
            for (id, instance) in instances.iter() {
                if let (Some(config), Some(controller)) =
                    (&instance.controller_config, &instance.controller)
                {
                    if is_experimental(config) {
                        if !instance.agent_clients.is_empty() || !instance.agent_children.is_empty()
                        {
                            return Err("请等待当前任务完全结束后再检测。".into());
                        }
                        if !controller.connected() {
                            return Err("输入方式正在连接，请稍后重试。".into());
                        }
                        saved.push(SavedController {
                            instance_id: id.clone(),
                            config: config.clone(),
                        });
                    }
                }
            }
            saved
        };
        let ids = saved
            .iter()
            .map(|entry| entry.instance_id.clone())
            .collect::<Vec<_>>();
        let mut switching = ProbeSwitch {
            state: &state,
            saved,
            defaults: HashMap::new(),
            detached: false,
            app: &app,
        };
        let result = with_restore(
            &mut switching,
            |switching| {
                switching.switch_to_default(default_config.as_ref())?;
                run_probe(&backend)
            },
            |switching| switching.restore(),
        );
        for id in ids {
            super::utils::emit_state_changed(&app, &id, "controller-changed");
        }
        result
    })
    .await
    .map_err(|e| e.to_string())?
}

#[cfg(test)]
mod tests {
    use super::{default_for, is_experimental, task_in_progress, with_restore, PROBE_TASK_GATE};
    use crate::commands::types::{ControllerConfig, InstanceRuntime};

    #[test]
    fn task_and_connection_preparation_can_overlap_but_exclude_detection() {
        let first = PROBE_TASK_GATE.try_read().unwrap();
        let second = PROBE_TASK_GATE.try_read().unwrap();
        assert!(PROBE_TASK_GATE.try_write().is_err());
        drop(first);
        drop(second);
        let detection = PROBE_TASK_GATE.try_write().unwrap();
        assert!(PROBE_TASK_GATE.try_read().is_err());
        drop(detection);
    }

    #[test]
    fn completed_task_history_does_not_block_detection() {
        let mut instance = InstanceRuntime::default();
        instance.task_run_state.pending_task_ids.push(123);
        instance.task_run_state.overall_status = Some("Succeeded".into());
        assert!(!task_in_progress(&instance));
        instance.task_run_state.overall_status = Some("Running".into());
        assert!(task_in_progress(&instance));
        instance.task_run_state.overall_status = Some("Failed".into());
        instance.stop_in_progress = true;
        assert!(task_in_progress(&instance));
    }

    #[test]
    fn default_switch_preserves_window_and_uses_default_methods() {
        let original = ControllerConfig::Win32 {
            handle: 123,
            screencap_method: 32,
            mouse_method: 4096,
            keyboard_method: 4096,
            display_short_side: Some(720),
        };
        let default = ControllerConfig::Win32 {
            handle: 0,
            screencap_method: 18,
            mouse_method: 32,
            keyboard_method: 4,
            display_short_side: Some(1080),
        };
        let expected = ControllerConfig::Win32 {
            handle: 123,
            screencap_method: 18,
            mouse_method: 32,
            keyboard_method: 4,
            display_short_side: Some(1080),
        };
        assert!(is_experimental(&original));
        assert_eq!(default_for(&original, &default).unwrap(), expected);
        assert!(default_for(&original, &original).is_err());
    }

    #[test]
    fn restores_after_success_error_and_panic() {
        for outcome in 0..3 {
            let mut state = "original";
            let result = with_restore(
                &mut state,
                |state| {
                    *state = "default";
                    match outcome {
                        0 => Ok("ok"),
                        1 => Err("failed".into()),
                        _ => panic!("cancelled"),
                    }
                },
                |state| {
                    *state = "original";
                    Ok(())
                },
            );
            assert_eq!(state, "original");
            assert_eq!(result.is_ok(), outcome == 0);
        }
    }

    #[test]
    fn reports_restore_failure() {
        let result = with_restore(&mut (), |_| Ok("ok"), |_| Err("disconnected".into()));
        assert!(result.unwrap_err().contains("恢复原输入方式失败"));
    }
}
