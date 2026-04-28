use crate::models::{Config, Success, CloudflareResult};
use anyhow::{Result, anyhow};
use reqwest::Client;
use serde_json::json;
use std::time::Duration;

pub struct CloudflareClient {
    client: Client,
    token: String,
}

impl CloudflareClient {
    pub fn new(token: &str) -> Self {
        Self {
            client: Client::builder()
                .timeout(Duration::from_secs(10))
                .build()
                .unwrap(),
            token: token.to_string(),
        }
    }

    /// 获取 DNS 记录信息 (对应 Go 的 CloudFlareApi GET)
    /// 注意这里使用 Success<Vec<CloudflareResult>> 处理列表返回
    async fn get_record_info(&self, zone_id: &str, domain: &str, record_type: &str) -> Result<Option<(String, String)>> {
        let url = format!(
            "https://api.cloudflare.com/client/v4/zones/{}/dns_records?name={}&type={}",
            zone_id, domain, record_type
        );

        let response = self.client
            .get(url)
            .bearer_auth(&self.token)
            .send()
            .await?;

        let text = response.text().await?;
        
        // 指明解析为 Vec 列表
        let resp: Success<Vec<CloudflareResult>> = serde_json::from_str(&text).map_err(|e| {
            anyhow!("GET 解析失败: {}, 原始报文: {}", e, text)
        })?;

        if !resp.success {
            let err_code = resp.errors.first().map(|e| e.code).unwrap_or(0);
            let err_msg = resp.errors.first().map(|e| e.message.as_str()).unwrap_or("Unknown error");
            return Err(anyhow!("CF_ERROR_{}: {}", err_code, err_msg));
        }

        if !resp.result.is_empty() {
            let record = &resp.result[0];
            Ok(Some((record.id.clone(), record.content.clone())))
        } else {
            Ok(None)
        }
    }

    /// 核心业务逻辑：更新或创建记录，并同步修改配置与文件
    pub async fn update_ddns(&self, conf: &mut Config, record_type: &str, new_ip: &str) -> Result<()> {
        if new_ip.is_empty() {
            return Err(anyhow!("新 IP 为空，跳过更新"));
        }

        // 1. 获取线上记录
        let record_info = self.get_record_info(&conf.zone_id, &conf.domains, record_type).await?;

        // 2. 匹配 Method 和 URL
        let (method, url) = match record_info {
            Some((id, old_ip)) => {
                if old_ip == new_ip {
                    println!("{} 记录已是最新 ({})，无需操作", record_type, new_ip);
                    if record_type == "A" { conf.ip_addr = new_ip.to_string(); }
                    else { conf.ip6_addr = new_ip.to_string(); }
                    return Ok(());
                }
                (reqwest::Method::PUT, format!("https://api.cloudflare.com/client/v4/zones/{}/dns_records/{}", conf.zone_id, id))
            },
            None => {
                (reqwest::Method::POST, format!("https://api.cloudflare.com/client/v4/zones/{}/dns_records", conf.zone_id))
            }
        };

        // 3. 执行请求
        let body = json!({
            "type": record_type,
            "name": conf.domains,
            "content": new_ip,
            "ttl": 1,
            "proxied": conf.proxy
        });

        let resp = self.client
            .request(method, url)
            .bearer_auth(&self.token)
            .json(&body)
            .send()
            .await?;

        let text = resp.text().await?;

        // 关键点：更新/创建接口返回的是单体 Result 对象
        // 我们解析为 Success<serde_json::Value> 以兼容单体返回并确保 success 为 true
        let result: Success<serde_json::Value> = serde_json::from_str(&text).map_err(|e| {
            anyhow!("操作接口解析失败: {}, 报文: {}", e, text)
        })?;

        if result.success {
            if record_type == "A" {
                conf.ip_addr = new_ip.to_string();
            } else {
                conf.ip6_addr = new_ip.to_string();
            }

            // 同步写入配置文件
            conf.save_to_file().await.map_err(|e| anyhow!("保存配置失败: {}", e))?;
            
            println!("Cloudflare {} 更新成功并已保存到文件", record_type);
            Ok(())
        } else {
            let err_code = result.errors.first().map(|e| e.code).unwrap_or(0);
            
            // 如果你想把具体的错误信息也带出来，可以这么写：
            let err_msg = result.errors.first()
                .map(|e| e.message.as_str())
                .unwrap_or("Unknown error");

            Err(anyhow!("CF_ERROR_{}: {}", err_code, err_msg))
        }
    }
}