use rust_embed::RustEmbed;
use axum::{
    body::Body,
    routing::{get, post, put, delete},
    extract::{Path, State, Json, Request},
    response::{IntoResponse, Response},
    middleware::{self, Next},
    http::{StatusCode, header},
    Router,
};
use std::sync::Arc;
use serde_json::{json};
use crate::models::{ProcessInfo, PasswordForm, BasicConfigForm};
use pnet_datalink;
use base64::{engine::general_purpose, Engine as _};
use tokio::process::Command;

#[derive(RustEmbed)]
#[folder = "static/"] 
struct Asset;

// 【第三步的代码：放在这里】
async fn static_handler(Path(path): Path<String>) -> impl IntoResponse {
    match Asset::get(&path) {
        Some(content) => {
            let mime = mime_guess::from_path(&path).first_or_octet_stream();
            Response::builder()
                .header(header::CONTENT_TYPE, mime.as_ref())
                .body(Body::from(content.data))
                .unwrap()
        }
        None => {
            Response::builder()
                .status(StatusCode::NOT_FOUND)
                .body(Body::from("404 Not Found"))
                .unwrap()
        }
    }
}


// --- 全局状态定义 ---
pub struct AppState {
    pub config: Arc<tokio::sync::Mutex<crate::models::Config>>,
    pub socat: crate::socat::SocatManager,
}

// --- 身份验证中间件 ---
async fn auth_middleware(
    State(state): State<Arc<AppState>>,
    req: Request,
    next: Next,
) -> Response {
    let auth_header = req.headers()
        .get(header::AUTHORIZATION)
        .and_then(|h| h.to_str().ok());

    let conf = state.config.lock().await;
    let expected_auth = format!("admin:{}", conf.admin_pwd);
    let expected_base64 = general_purpose::STANDARD.encode(expected_auth);
    let expected_header = format!("Basic {}", expected_base64);

    if let Some(auth) = auth_header {
        if auth == expected_header {
            drop(conf); // 及时释放锁
            return next.run(req).await;
        }
    }

    // 验证失败，要求认证
    Response::builder()
        .status(StatusCode::UNAUTHORIZED)
        .header(header::WWW_AUTHENTICATE, "Basic realm=\"Restricted\"")
        .body(axum::body::Body::empty()) // Axum 0.7 使用 Body::empty() 替代 BoxBody
        .unwrap()
        .into_response()
}

pub fn create_router(state: Arc<AppState>) -> Router {
    // API 路由组
    let api_routes = Router::new()
        .route("/status", get(get_status))
        .route("/setpassword", put(set_password))
        .route("/setconfig", put(set_config))
        .route("/socat_status", get(get_socat_status))
        .route("/add_socat", post(add_socat))
        .route("/change_socat", put(change_socat))
        .route("/del_socat", delete(del_socat))
        // 将中间件应用到 API 路由
        .layer(middleware::from_fn_with_state(state.clone(), auth_middleware));

    Router::new()
        .nest("/api", api_routes)
        .route("/", get(index_handler))
        .route("/static/*path", get(static_handler))
        .with_state(state)
}

// --- 处理函数实现 ---

async fn index_handler() -> impl IntoResponse {
    static_handler(Path("index.html".to_string())).await
}

pub async fn get_status(State(state): State<Arc<AppState>>) -> impl IntoResponse {
    let conf = state.config.lock().await;

    let mut interfaces_list = Vec::new();
    for iface in pnet_datalink::interfaces() {
        if iface.is_up() && !iface.is_loopback() {
            interfaces_list.push(iface.name);
        }
    }

    Json(json!({
        "status": 200,
        "message": "success",
        "ZoneID": &conf.zone_id,
        "Token": &conf.token,
        "IPv4": &conf.ip_addr,
        "IPv6": &conf.ip6_addr,
        "Type": &conf.record_type,
        "Proxy": conf.proxy,
        "InterFacesList": interfaces_list,
        "Domains": &conf.domains,
        "InterFace": &conf.interface,
        "TaskStatus": true,
        "ApiStatus": !conf.has_error, 
        "HasError": conf.has_error,
        "ScanTime": conf.scan_time,
        "Version": "1.0.1-Rust",
    }))
}

async fn set_password(
    State(state): State<Arc<AppState>>,
    Json(payload): Json<PasswordForm>,
) -> impl IntoResponse {
    if payload.password.trim().is_empty() {
        return Json(json!({"status": 1, "message": "密码不能为空"}));
    }

    let mut conf = state.config.lock().await;
    conf.admin_pwd = payload.password; // 只更新密码字段
    
    match conf.save_to_file().await {
        Ok(_) => Json(json!({"status": 200, "message": "Password updated"})),
        Err(e) => Json(json!({"status": 500, "message": e.to_string()})),
    }
}

async fn set_config(
    State(state): State<Arc<AppState>>,
    Json(req): Json<BasicConfigForm>, // 使用专用 Form
) -> impl IntoResponse {
    // 严谨校验示例
    if req.token.is_empty() || req.zone_id.is_empty() {
        return Json(json!({"status": 1, "message": "Token 或 ZoneID 缺失"}));
    }

    let mut conf = state.config.lock().await;
    
    // 局部赋值，绝对不会触碰 socat_list
    conf.zone_id = req.zone_id;
    conf.token = req.token;
    conf.domains = req.domains;
    conf.scan_time = req.scan_time;
    conf.interface = req.interface;
    conf.proxy = req.proxy;
    conf.record_type = req.record_type;
    conf.has_error = false;

    match conf.save_to_file().await {
        Ok(_) => Json(json!({"status": 200, "message": "Config saved"})),
        Err(e) => Json(json!({"status": 500, "message": e.to_string()})),
    }
}

pub async fn get_socat_status(
    State(state): State<Arc<AppState>>,
) -> impl IntoResponse {
    let socat_status: i32;
    let mut socat_version = String::new();
    let mut final_list: Vec<ProcessInfo> = Vec::new();

    // 检查环境
    let output = Command::new("sh")
        .arg("-c")
        .arg("socat -V | awk '/version/{print $3}'")
        .output()
        .await;

    match output {
        Ok(out) if out.status.success() => {
            socat_version = String::from_utf8_lossy(&out.stdout).trim().to_string();
            socat_status = 0;
        }
        _ => socat_status = 2,
    }

    if socat_status == 0 {
        // 直接调用管理器的方法
        let running_list = state.socat.get_running_socat_list().await;
        let conf = state.config.lock().await;
        
        for mut running_item in running_list {
            // 匹配逻辑：端口和协议
            if let Some(matched) = conf.socat_list.iter().find(|c| {
                running_item.port == c.port && running_item.conn_type == c.conn_type
            }) {
                running_item.name = matched.name.clone();
            } else {
                running_item.name = format!("未知任务:{}", running_item.port);
            }
            final_list.push(running_item);
        }
    }

    Json(serde_json::json!({
        "status": 200,
        "SocatStatus": socat_status,
        "SocatVersion": socat_version,
        "SocatList": final_list,
    }))
}


async fn add_socat(
    State(state): State<Arc<AppState>>,
    Json(mut payload): Json<ProcessInfo>,
) -> impl IntoResponse {
    let mut conf = state.config.lock().await;
    payload.name = payload.name.trim().to_string();

    if payload.conn_type.starts_with("TCP") {
        payload.fork_type = "TCP".to_string();
    } else if payload.conn_type.starts_with("UDP") {
        payload.fork_type = "UDP".to_string();
    }

    // --- 修正点 B: 严格冲突检查 ---
    for item in &conf.socat_list {
        if item.name == payload.name {
            return Json(json!({"status": 1, "message": "错误：任务名称已存在"}));
        }

        // 核心冲突：同协议 + 同端口
        if item.conn_type == payload.conn_type && item.port == payload.port {
            return Json(json!({
                "status": 1, 
                "message": format!("错误：{} 端口 {} 已被使用", item.conn_type, item.port)
            }));
        }
    }

    match state.socat.start_socat(&payload).await {
        Ok(pid) => {
            payload.pid = pid; 
            conf.socat_list.push(payload);
            let _ = conf.save_to_file().await;
            Json(json!({"status": 200, "message": "success"}))
        },
        Err(e) => Json(json!({"status": 1, "message": format!("启动失败: {}", e)}))
    }
}

async fn change_socat(
    State(state): State<Arc<AppState>>,
    Json(mut payload): Json<ProcessInfo>,
) -> impl IntoResponse {
    let mut conf = state.config.lock().await;
    
    // --- 修正点 C: 精准定位旧记录 ---
    // 必须同时满足：[旧名字] 或者 [旧端口 + 旧协议]
    let position = conf.socat_list.iter().position(|x| {
        if let Some(old_n) = &payload.old_name {
            x.name == *old_n
        } else {
            // 如果没传 old_name，则必须通过 协议+端口 唯一对齐
            // 这样就不会因为 TCP6 和 UDP6 端口号相同而杀错人
            x.name == payload.name || (x.port == payload.port && x.conn_type == payload.conn_type)
        }
    });

    if let Some(pos) = position {
        let old_task = conf.socat_list[pos].clone();

        // 执行精准击杀：只杀这个端口+这个协议的进程
        if old_task.pid > 0 {
            let _ = state.socat.stop_socat(old_task.pid).await;
        }
        // cleanup_remnants 内部也需要支持协议过滤（见下文补丁）
        let _ = state.socat.cleanup_remnants(&old_task).await;

        match state.socat.start_socat(&payload).await {
            Ok(new_pid) => {
                payload.pid = new_pid;
                payload.old_name = None; 
                conf.socat_list[pos] = payload; 
                let _ = conf.save_to_file().await;
                Json(json!({"status": 200, "message": "Updated Successfully"}))
            },
            Err(e) => {
                conf.socat_list[pos].pid = -1;
                let _ = conf.save_to_file().await;
                Json(json!({"status": 1, "message": format!("清理完成但启动失败: {}", e)}))
            }
        }
    } else {
        Json(json!({"status": 1, "message": "未找到原始任务记录"}))
    }
}

async fn del_socat(
    State(state): State<Arc<AppState>>,
    Json(payload): Json<crate::models::DeleteForm>, 
) -> impl IntoResponse {
    let mut conf = state.config.lock().await;
    
    // --- 修正点 1：多维定位逻辑 ---
    // 寻找索引的优先级：Name (最准) > PID (次之)
    let position = conf.socat_list.iter().position(|x| {
        (!payload.name.is_empty() && x.name == payload.name) || 
        (payload.pid > 0 && x.pid == payload.pid)
    });

    if let Some(pos) = position {
        // 备份一份旧数据，用于最后的精准清理
        let task_to_del = conf.socat_list[pos].clone();
        
        // --- 修正点 2：双重击杀逻辑 ---
        // 第一步：尝试用 PID 杀（针对当前运行中且没重启的情况，最快）
        if task_to_del.pid > 0 {
            let _ = state.socat.stop_socat(task_to_del.pid).await;
        }
        
        // 第二步：根据协议和端口执行“特征击杀”（针对程序重启、PID 失效的情况）
        // 确保那些赖在系统里的残留 socat 彻底消失
        if let Err(e) = state.socat.cleanup_remnants(&task_to_del).await {
            eprintln!("清理残留进程失败 (端口: {}): {}", task_to_del.port, e);
        }

        // 3. 从内存列表中移除
        conf.socat_list.remove(pos);
        
        // 4. 持久化到文件
        if let Err(e) = conf.save_to_file().await {
            return Json(serde_json::json!({
                "status": 500, 
                "message": format!("进程已清理，但配置删除失败: {}", e)
            }));
        }

        println!("Socat 任务已彻底清理: Name: {}, Port: {}", task_to_del.name, task_to_del.port);
        Json(serde_json::json!({"status": 200, "message": "success"}))
        
    } else {
        Json(serde_json::json!({"status": 1, "message": "未找到匹配的任务记录"}))
    }
}