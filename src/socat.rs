use crate::models::ProcessInfo;
use anyhow::{anyhow, Result};
use std::collections::HashMap;
use std::sync::Arc;
use tokio::process::{Command};
use tokio::sync::Mutex;

pub struct SocatManager {
    pub active_processes: Arc<Mutex<HashMap<i32, tokio::process::Child>>>,
}

impl SocatManager {
    pub fn new() -> Self {
        Self {
            active_processes: Arc::new(Mutex::new(HashMap::new())),
        }
    }
    pub async fn start_socat(&self, item: &ProcessInfo) -> Result<i32> {
        let listen_arg = format!("{}-LISTEN:{},reuseaddr,fork", item.conn_type, item.port);
        let fork_arg = format!("{}:{}:{}", item.fork_type, item.fork_ip, item.fork_port);

        let mut child = Command::new("socat")
            .arg(&listen_arg)
            .arg(&fork_arg)
            .spawn()
            .map_err(|e| anyhow!("无法启动 socat 进程: {}", e))?;

        let real_pid = child.id().ok_or_else(|| anyhow!("无法获取进程 PID"))? as i32;

        // --- 防僵尸进程核心逻辑 ---
        let processes_ptr = Arc::clone(&self.active_processes);
        tokio::spawn(async move {
            // 这里会异步阻塞，直到子进程结束
            let status = child.wait().await;
            println!("进程 {} 已退出, 状态: {:?}", real_pid, status);
            
            // 结束后从内存 map 中移除
            let mut lock = processes_ptr.lock().await;
            lock.remove(&real_pid);
        });

        println!("已启动 socat 任务 [{}], 端口: {}, PID: {}", item.name, item.port, real_pid);
        Ok(real_pid)
    }

    // 停止进程
    pub async fn stop_socat(&self, pid: i32) -> Result<()> {
        if pid <= 0 { return Ok(()); }
        
        // 尝试多种手段杀掉进程
        let cmd = format!("kill -9 {} 2>/dev/null", pid);
        let _ = Command::new("sh").arg("-c").arg(cmd).output().await;
        
        Ok(())
    }

    // 清理残留 (BusyBox 兼容版)
    // --- src/socat.rs ---

    pub async fn cleanup_remnants(&self, info: &ProcessInfo) -> Result<()> {
        // 修正点：grep 增加协议过滤，例如 grep "TCP6-LISTEN:7893"
        // 这样就不会在修改 UDP6:7893 时把 TCP6:7893 给杀了
        let cmd = format!(
            "ps -www | grep socat | grep \"{}-LISTEN:{}\" | grep -v grep | awk '{{print $1}}' | xargs -r kill -9",
            info.conn_type,
            info.port
        );
        
        let _ = Command::new("sh").arg("-c").arg(&cmd).output().await;
        tokio::time::sleep(std::time::Duration::from_millis(200)).await;
        Ok(())
    }

    // 获取当前运行中的 socat 列表 (带数据清洗)
    pub async fn get_running_socat_list(&self) -> Vec<ProcessInfo> {
        let mut list = Vec::new();
        
        // 执行 ps -www 确保完整路径，过滤掉干扰进程
        let output = Command::new("sh")
            .arg("-c")
            .arg("ps -www | grep socat | grep -v grep | grep -v cfddns")
            .output()
            .await;

        if let Ok(out) = output {
            let stdout = String::from_utf8_lossy(&out.stdout);
            
            for line in stdout.lines() {
                let parts: Vec<&str> = line.split_whitespace().collect();
                if parts.len() < 3 { continue; }

                // 1. 提取 PID (找第一个出现的纯数字)
                let pid = parts.iter()
                    .find(|s| s.chars().all(|c| c.is_ascii_digit()))
                    .and_then(|s| s.parse::<i32>().ok())
                    .unwrap_or(-1);

                // 2. 定位监听端 (特征：-LISTEN:)
                if let Some(listen_idx) = parts.iter().position(|s| s.contains("-LISTEN:")) {
                    let listen_str = parts[listen_idx];
                    
                    // 提取连接类型 (如 TCP6-LISTEN -> TCP6)
                    let conn_type = listen_str.split('-').next().unwrap_or("TCP6").to_uppercase();
                    
                    // 提取监听端口 (冒号后第一个段)
                    let port = listen_str.split(':')
                        .nth(1)
                        .and_then(|s| s.split(',').next())
                        .and_then(|s| s.parse::<i32>().ok())
                        .unwrap_or(0);

                    // 3. 定位转发目标 (特征：包含 TCP4: 或 UDP4:)
                    let mut f_ip = "unknown".to_string();
                    let mut f_port = 0;
                    let mut fork_type = "TCP".to_string();

                    if let Some(target_str) = parts.iter().find(|s| {
                        let s_up = s.to_uppercase();
                        s_up.contains("TCP4:") || s_up.contains("UDP4:")
                    }) {
                        // 由于只有 IPv4，格式固定为 PROTO:IP:PORT
                        // 例如 TCP4:192.168.1.100:8080
                        let target_parts: Vec<&str> = target_str.split(':').collect();
                        
                        if target_parts.len() >= 3 {
                            fork_type = if target_parts[0].to_uppercase().contains("UDP") {
                                "UDP".to_string()
                            } else {
                                "TCP".to_string()
                            };
                            f_ip = target_parts[1].to_string();
                            f_port = target_parts[2].parse::<i32>().unwrap_or(0);
                        }
                    }

                    list.push(ProcessInfo {
                        name: String::new(),
                        old_name: None,
                        pid,
                        conn_type,
                        port,
                        fork_ip: f_ip,
                        fork_port: f_port,
                        fork_type,
                    });
                }
            }
        }
        list
    }
}