use tokio::process::Command;
use anyhow::{Result, anyhow};
use reqwest::Client;
use std::time::Duration;

/// 封装 Go 中的 GetIpAddr 完整逻辑
pub struct IPList {
    pub ip_addr: String,
    pub ip6_addr: String,
}

/// 对应 Go 的 GetIpAddr 完整逻辑
pub async fn get_ip_addr(interface: &str) -> IPList {
    let mut ipv4 = String::new();
    let mut ipv6 = String::new();

    // --- IPv4 逻辑 ---
    // 严格复现你的命令：awk '{print $2}' | awk -F "/" '{print $1}'
    // Rust 中 format! 里的 {{ }} 会被渲染为 { }
    let cmd_v4 = format!(
        "ip -4 addr show dev {} | grep \"scope global\" | awk '{{print $2}}' | awk -F \"/\" '{{print $1}}'",
        interface
    );

    match run_command_with_res(&cmd_v4).await {
        Ok(res) if !res.is_empty() => {
            // 取第一行，对应 Go 的 Split(ip, "\n")[0]
            ipv4 = res.lines().next().unwrap_or("").trim().to_string();
        }
        _ => {
            if let Ok(ip) = get_data("http://4.ipw.cn").await { ipv4 = ip; }
        }
    }

    // --- IPv6 逻辑 ---
    let cmd_v6 = format!(
        "ip -6 addr show dev {} | grep \"scope global\" | awk '{{print $2}}' | awk -F \"/\" '{{print $1}}'",
        interface
    );

    match run_command_with_res(&cmd_v6).await {
        Ok(res) if !res.is_empty() => {
            // IPv6 重点：获取多行时只取第一行
            ipv6 = res.lines().next().unwrap_or("").trim().to_string();
        }
        _ => {
            if let Ok(ip) = get_data("http://6.ipw.cn").await { ipv6 = ip; }
        }
    }

    IPList { ip_addr: ipv4, ip6_addr: ipv6 }
}

/// 封装 Shell 命令执行
async fn run_command_with_res(cmd: &str) -> Result<String> {
    // 1. 打印即将执行的完整命令，方便你直接复制到终端测试
    // println!("DEBUG: 执行命令 -> [{}]", cmd);

    let output = Command::new("sh")
        .arg("-c")
        .arg(cmd)
        .output()
        .await?;

    // 2. 捕获 stdout 并转为字符串
    let stdout = String::from_utf8_lossy(&output.stdout).trim().to_string();
    // 3. 捕获 stderr 并转为字符串
    let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();

    if output.status.success() {
        // if stdout.is_empty() {
        //     println!("DEBUG: 命令执行成功，但 stdout 为空。");
        // } else {
        //     println!("DEBUG: 命令输出 (stdout) -> [{}]", stdout);
        // }
        Ok(stdout)
    } else {
        // 4. 如果命令执行状态码不是 0，打印错误信息
        // eprintln!("DEBUG: 命令执行失败！状态码: {:?}", output.status.code());
        // eprintln!("DEBUG: 错误信息 (stderr) -> [{}]", stderr);
        Err(anyhow!("Command failed with stderr: {}", stderr))
    }
}

/// 对应 Go 的 getData
async fn get_data(url: &str) -> Result<String> {
    let client = Client::builder()
        .timeout(Duration::from_secs(5))
        .build()?;
    let res = client.get(url).send().await?.text().await?;
    Ok(res.trim().to_string())
}

/// 获取当前执行文件所在目录 (对应 Go 的 GetCurrentPath)
pub fn get_current_dir() -> Result<std::path::PathBuf> {
    let path = std::env::current_exe()?;
    Ok(path.parent().unwrap().to_path_buf())
}