# cfddns: 高性能动态域名管理与 Socat 转发工具

![Rust](https://img.shields.io/badge/language-Rust-orange.svg)
![Platform](https://img.shields.io/badge/platform-OpenWrt%20%7C%20Linux%20%7C%20ARM64-blue.svg)

本项目是一个专为 OpenWrt 及嵌入式设备设计的“单文件”网络管理工具。集成 Cloudflare DDNS 自动更新与 Socat 端口转发管理，采用 Rust 编写，极致轻量且稳健。

## 🚀 核心优势

### 1. 极致省流的 IP 扫描方案
* **内存状态机对比**：程序在内存中维护当前 IP 状态，每 30 秒执行一次本地网络检查。
* **零冗余请求**：只有当本地侦测到有效 IP 变更时，才会触发 Cloudflare API 请求。
* **为什么敢 30s 扫描一次？** 因为 99% 的扫描仅消耗极微量的 CPU 周期进行本地内存比对，不会触发频繁的远程 API 调用，既保证了实时性，又不会被 CF 封禁。

### 2. 强力端口转发控制 (Socat)
* **PID + 特征双重控制**：不仅追踪 PID，还通过 `ps -www` 深度解析进程参数（协议+端口）。
* **残留自动清理**：即便系统非正常重启导致 PID 失效，程序在启动或修改任务时也能通过特征识别精准击杀“僵尸”进程，彻底杜绝 `Address already in use`。
* **协议自动对齐**：支持 TCP/UDP/TCP6/UDP6，后端自动确保转发两端协议一致。

### 3. “全弹打包”部署方案
* **静态资源嵌入**：通过 `rust-embed` 将前端 HTML/JS/CSS 直接编译进二进制文件。
* **单文件运行**：无需再携带 `static` 文件夹，不依赖工作目录路径，丢到 `/etc` 或 `/` 都能完美运行。

## 🛠️ 编译指南 (Arch Linux 环境)

针对 OpenWrt (x86_64 或 ARM64) 进行静态编译：

```bash
# 1. 准备环境
rustup target add x86_64-unknown-linux-musl
rustup target add aarch64-unknown-linux-musl
sudo pacman -S musl aarch64-linux-gnu-gcc

# 2. 编译 (以 ARM64 为例)
CC_aarch64_unknown_linux_musl=aarch64-linux-gnu-gcc \
cargo build --target aarch64-unknown-linux-musl --release

# 3. 瘦身 (必须执行，可减小 70% 体积)
aarch64-linux-gnu-strip target/aarch64-unknown-linux-musl/release/cfddns
```
==================================================
## 接口规范 (API) 说明
所有 API 均受 Auth 中间件保护，访问时必须加上 api 前缀。

| 接口地址    |     方法  |   功能描述 |
| ------------ | ------------ | ------------ |
| /api/socat_status |  GET  |    获取转发列表及运行状态
| /api/add_socat    |  POST |    自动对齐协议并开启新转发
| /api/change_socat |  PUT  |    精准定位旧任务并平滑重启
| /api/del_socat    |  DELETE |  物理清理进程并移除配置
| /                 |  GET   |   访问嵌入的 Web UI 首页

注意：

首页直接通过根路径 / 访问，内部自动指向 index.html。

所有静态资源都在 /static/ 路径下，由二进制文件直接读取。