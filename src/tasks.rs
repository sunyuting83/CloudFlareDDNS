use crate::models::Config;
use crate::utils::get_ip_addr;
use crate::cloudflare::CloudflareClient;
use tokio::time::{sleep, Duration};
use std::sync::Arc;
use tokio::sync::Mutex;

pub async fn start_ddns_task(config: Arc<Mutex<Config>>) {
    println!("DDNS 轮询服务已启动...");
    
    // 关键点：引入“首次运行”标记位
    let mut is_first_run = true;

    loop {
        // 1. 获取当前配置的扫描间隔和熔断状态
        let (scan_time, has_error) = {
            let conf = config.lock().await;
            (conf.scan_time as u64, conf.has_error)
        };

        // 2. 如果处于熔断状态，每 10 秒尝试一次（看看用户是否在网页端修好了配置）
        if has_error {
            sleep(Duration::from_secs(10)).await;
            continue;
        }

        // 3. 执行核心循环，并将 is_first_run 标记传入
        run_cycle(Arc::clone(&config), is_first_run).await;
        
        // 第一次跑完后，将标记置为 false，后续进入正常省流模式
        is_first_run = false;

        // 4. 动态等待
        sleep(Duration::from_secs(scan_time)).await;
    }
}

async fn run_cycle(config: Arc<Mutex<Config>>, is_first_run: bool) {
    let mut conf = config.lock().await;

    // 基础校验：配置不全则跳过
    if !conf.domains.contains('.') || conf.token.len() < 20 || conf.zone_id.len() < 20 {
        return;
    }

    // 获取当前网卡的真实 IP
    let ip_data = get_ip_addr(&conf.interface).await;
    let cf = CloudflareClient::new(&conf.token);

    // 根据 RecordType 决定处理 IPv4 还是 IPv6
    match conf.record_type {
        0 | 1 | 2 => {
            // --- 处理 IPv4 (A 记录) ---
            if (conf.record_type == 0 || conf.record_type == 2) && !ip_data.ip_addr.is_empty() {
                // 逻辑：(IP 变了) 或者 (程序刚启动)
                if conf.ip_addr != ip_data.ip_addr || is_first_run {
                    if is_first_run {
                        println!("程序启动/网络重置，执行 IPv4 强制校验...");
                    } else {
                        println!("IPv4 发生变化: {} -> {}，同步云端...", conf.ip_addr, ip_data.ip_addr);
                    }
                    
                    if let Err(e) = cf.update_ddns(&mut *conf, "A", &ip_data.ip_addr).await {
                        handle_task_error(&mut *conf, e).await;
                    }
                }
            }

            // --- 处理 IPv6 (AAAA 记录) ---
            if !conf.has_error && (conf.record_type == 1 || conf.record_type == 2) && !ip_data.ip6_addr.is_empty() {
                // 逻辑：(IP 变了) 或者 (程序刚启动)
                if conf.ip6_addr != ip_data.ip6_addr || is_first_run {
                    if is_first_run {
                        println!("程序启动/网络重置，执行 IPv6 强制校验...");
                    } else {
                        println!("IPv6 发生变化: {} -> {}，同步云端...", conf.ip6_addr, ip_data.ip6_addr);
                    }

                    if let Err(e) = cf.update_ddns(&mut *conf, "AAAA", &ip_data.ip6_addr).await {
                        handle_task_error(&mut *conf, e).await;
                    }
                }
            }
        }
        _ => {}
    }
}

/// 统一错误处理
async fn handle_task_error(conf: &mut Config, err: anyhow::Error) {
    let err_str = err.to_string();
    
    let is_fatal_config_error = err_str.contains("10000") 
                             || err_str.contains("6003") 
                             || err_str.contains("7001")
                             || err_str.contains("CF_ERROR_4");

    let is_network_issue = err_str.contains("timeout") 
                        || err_str.contains("connection") 
                        || err_str.contains("dns")
                        || err_str.contains("pool");

    if is_network_issue {
        eprintln!("DDNS 任务由于网络波动暂时失败: {}。等待下一轮重试...", err_str);
        return;
    }

    if is_fatal_config_error {
        eprintln!("检测到致命配置错误: {}。触发安全熔断，任务已停止。", err_str);
        conf.has_error = true;
    } else {
        eprintln!("检测到业务错误: {}。停止任务以保护账户安全。", err_str);
        conf.has_error = true;
    }

    if conf.has_error {
        let _ = conf.save_to_file().await;
    }
}