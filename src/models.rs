use serde::{Deserialize, Serialize};

#[derive(Debug, Serialize, Deserialize, Clone)]
#[serde(rename_all = "PascalCase")]
pub struct Config {
    // 这些是前端会传的
    pub zone_id: String,
    pub token: String,
    pub domains: String,
    pub scan_time: u64,
    pub interface: String,
    pub proxy: bool,
    pub record_type: i32,
    pub ip_addr: String,
    pub ip6_addr: String,
    pub admin_pwd: String,
    pub has_error: bool,
    pub cf_api: String,
    pub socat_list: Vec<ProcessInfo>,
}

#[derive(Debug, Serialize, Deserialize, Clone)] // 加上 Debug
#[serde(rename_all = "PascalCase")]
pub struct ProcessInfo {
    pub name: String,
    #[serde(skip_serializing_if = "Option::is_none")] // 序列化时如果为空就隐藏
    pub old_name: Option<String>, 
    pub pid: i32,
    pub conn_type: String,
    pub port: i32,     // 它是 i32
    pub fork_ip: String,
    pub fork_port: i32, // 它是 i32
    pub fork_type: String,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            cf_api: "https://api.cloudflare.com/client/v4/zones/".to_string(),
            proxy: false,
            zone_id: "".to_string(),
            token: "".to_string(),
            interface: "".to_string(),
            record_type: 0,
            ip_addr: "".to_string(),
            ip6_addr: "".to_string(),
            admin_pwd: "1234567890".to_string(),
            domains: "".to_string(),
            scan_time: 30,
            has_error: false,
            socat_list: vec![],
        }
    }
}
#[derive(Debug, Serialize, Deserialize, Clone)]
pub struct CfError {
    pub code: i32,
    pub message: String,
}

#[derive(Debug, Serialize, Deserialize, Clone)]
pub struct ResultInfo {
    pub page: i32,
    pub per_page: i32,
    pub count: i32,
    pub total_count: i32,
    pub total_pages: i32,
}

#[derive(Debug, Serialize, Deserialize, Clone)]
pub struct CloudflareResult {
    pub id: String,
    // 对应 Go 的 zone_id 等字段，加 #[serde(default)] 是为了兼容
    // 那些在某些接口（如更新）中可能缺失但结构体要求的字段
    #[serde(default)]
    pub zone_id: String,
    #[serde(default)]
    pub zone_name: String,
    pub name: String,
    #[serde(rename = "type")]
    pub record_type: String,
    pub content: String,
    pub proxiable: bool,
    pub proxied: bool,
    pub ttl: i32,
    #[serde(default)]
    pub locked: bool,
    // Meta 和时间戳如果不参与逻辑对比，可以用 Value 跳过或直接注释掉
    #[serde(default)]
    pub meta: serde_json::Value,
}

/// 核心：使用泛型 T 来兼容 []Result 和 Result
#[derive(Debug, Serialize, Deserialize, Clone)]
pub struct Success<T> {
    pub success: bool,
    pub errors: Vec<CfError>,
    pub messages: Vec<serde_json::Value>,
    // 这里的 result 可能是 Vec<CloudflareResult> 或 CloudflareResult
    pub result: T,
    // result_info 只有在 GET 列表时才有，所以用 Option
    pub result_info: Option<ResultInfo>,
}


#[derive(Debug, Deserialize)]
#[serde(rename_all = "PascalCase")]
pub struct BasicConfigForm {
    #[serde(rename = "ZoneID")]
    pub zone_id: String,
    
    pub token: String,
    pub record_type: i32,
    pub proxy: bool,
    
    #[serde(rename = "Interfaces")] 
    pub interface: String,
    
    pub domains: String,
    pub scan_time: u64,
}

// --- 2. 密码修改申请表 ---
// 对应 PUT /api/setpassword
#[derive(Debug, Deserialize)]
#[serde(rename_all = "PascalCase")]
pub struct PasswordForm {
    pub password: String,
}

#[derive(Deserialize)]
#[serde(rename_all = "PascalCase")]
pub struct DeleteForm {
    pub pid: i32,
    pub name: String,
}

impl Config {
    pub async fn save_to_file(&self) -> anyhow::Result<()> {
        let yaml = serde_yaml::to_string(self)?;
        tokio::fs::write("config.yaml", yaml).await?;
        Ok(())
    }
}