mod models;
mod utils;
mod socat;
mod cloudflare;
mod web;
mod tasks;

use std::sync::Arc;
use tokio::sync::Mutex;
use crate::models::Config;
use crate::socat::SocatManager;
use crate::web::{AppState, create_router};

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt::init();

    let current_dir = utils::get_current_dir()?;
    let config_path = current_dir.join("config.yaml");
    
    let config = if config_path.exists() {
        let content = std::fs::read_to_string(&config_path)?;
        serde_yaml::from_str(&content)?
    } else {
        let default_conf = Config::default();
        let yaml = serde_yaml::to_string(&default_conf)?;
        std::fs::write(&config_path, yaml)?;
        default_conf
    };

    let initial_socat_list = config.socat_list.clone();

    // 只创建一个 Arc<Mutex<Config>>
    let shared_config = Arc::new(Mutex::new(config));

    // 这里 config 接收的就是刚才创建的 Arc
    let shared_state = Arc::new(AppState {
        config: Arc::clone(&shared_config), 
        socat: SocatManager::new(),
    });

    // 4. 启动 DDNS 定时任务
    let task_config = Arc::clone(&shared_config);
    tokio::spawn(async move {
        tasks::start_ddns_task(task_config).await;
    });

    // 5. 启动初始的 Socat 任务
    for item in &initial_socat_list {
        shared_state.socat.start_socat(item).await?;
    }

    let app = create_router(shared_state);
    let listener = tokio::net::TcpListener::bind("0.0.0.0:3060").await?;
    println!("Server listening on http://0.0.0.0:3060");
    
    axum::serve(listener, app)
        .with_graceful_shutdown(shutdown_signal())
        .await?;

    Ok(())
}

async fn shutdown_signal() {
    tokio::signal::ctrl_c()
        .await
        .expect("failed to install CTRL+C handler");
}