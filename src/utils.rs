use tokio::process::Command;
use crate::models::Config;
use anyhow::{Result, anyhow};

/// 封装 Go 中的 GetIpAddr 完整逻辑
pub struct IPList {
    pub ip_addr: String,
    pub ip6_addr: String,
}

async fn fetch_ip_from_apis(urls: &[String]) -> String {
    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(3))
        .build()
        .unwrap_or_default();

    for url in urls {
        if let Ok(resp) = client.get(url).send().await {
            if let Ok(text) = resp.text().await {
                let ip = text.trim().to_string();
                // 简单校验：非空且包含 IP 特征符号
                if !ip.is_empty() && (ip.contains('.') || ip.contains(':')) {
                    return ip;
                }
            }
        }
    }
    String::new()
}

/// 对应 Go 的 GetIpAddr 完整逻辑
pub async fn get_ip_addr(conf: &Config) -> IPList {
    let mut ipv4 = String::new();
    let mut ipv6 = String::new();

    if conf.use_api {
        // --- 模式 A: 远程接口获取 ---
        ipv4 = fetch_ip_from_apis(&conf.ip_sources_v4).await;
        ipv6 = fetch_ip_from_apis(&conf.ip_sources_v6).await;
    } else {
        // --- 模式 B: 本地网卡获取 ---
        
        // IPv4 逻辑
        let cmd_v4 = format!(
            "ip -4 addr show dev {} | grep \"scope global\" | awk '{{print $2}}' | awk -F \"/\" '{{print $1}}'",
            conf.interface
        );
        if let Ok(res) = run_command_with_res(&cmd_v4).await {
            ipv4 = res.lines().next().unwrap_or("").trim().to_string();
        }

        // IPv6 逻辑
        let cmd_v6 = format!(
            "ip -6 addr show dev {} | grep \"scope global\" | awk '{{print $2}}' | awk -F \"/\" '{{print $1}}'",
            conf.interface
        );
        if let Ok(res) = run_command_with_res(&cmd_v6).await {
            ipv6 = res.lines().next().unwrap_or("").trim().to_string();
        }

        // --- 最后的保底 ---
        // 如果网卡模式没拿到 IP（比如网卡名写错了），可以视情况是否要自动跳到 API 模式保底
        // 这里暂时保持纯净，网卡没拿到就空着，方便用户排查网卡配置问题
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

/// 获取当前执行文件所在目录 (对应 Go 的 GetCurrentPath)
pub fn get_current_dir() -> Result<std::path::PathBuf> {
    let path = std::env::current_exe()?;
    Ok(path.parent().unwrap().to_path_buf())
}