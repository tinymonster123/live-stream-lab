# Streaming pipeline

这是运行时链路，不是代码模块划分。模块与接缝见 `architecture.md`。

完整链路由 AWS Streaming Media Lens、Mux 的 glass-to-glass 拆解、SRS 官方文档交叉得到。

## 12 段

| # | 阶段 | 输入 → 输出 | FFmpeg + SRS 覆盖 | 缺口 |
|---|---|---|---|---|
| 1 | 采集 | 摄像头/麦克风/屏幕/文件 → 原始帧 | FFmpeg `avfoundation` 可抓；无设备 UI、权限、场景合成 | 采集模块 |
| 2 | 编码 | 原始帧 → H.264/AAC 等压缩帧 | FFmpeg libavcodec/CLI 可做 | 编码策略模块 |
| 3 | 贡献封装 | 压缩帧 → FLV / MPEG-TS | FFmpeg muxer 可做 | 通常藏进推流模块 |
| 4 | 接入/传输 | 流 → 源站 | SRS 支持 RTMP / SRT / WHIP | 推流模块 |
| 5 | 源站接收/remux | 接入流 → 内部流 | SRS 本身 | SRS 生命周期适配器 |
| 6 | 转码/处理（可选） | 1 路 → N 路码率 | SRS 可 fork FFmpeg；无码率阶梯管理 | 转码模块 |
| 7 | 打包 | 码率 → HLS / FLV / WHEP 描述符 | SRS 可做协议转换 | 打包策略、segment/GOP |
| 8 | 分发 | 分片/清单 → 客户端 | SRS 自带 HTTP 服务 | 反代、HTTPS、CORS |
| 9 | 播放 | 描述符 → 屏幕画面 | 无；SRS 只有 demo 页面 | 播放器模块 |
| 10 | 控制面 | 命令/事件 → 鉴权与生命周期 | SRS 只有 HTTP API + callbacks | 会话编排、注册表、鉴权 |
| 11 | 录制/回放 | 流 → 文件 → VOD | SRS 只有基础 DVR | 录制策略、存储、VOD |
| 12 | 监控/运维 | 指标/日志 → 面板/告警 | SRS 有 exporter、API、日志 | Prometheus/Grafana、告警、守护 |

## 延迟预算

| 阶段 | 典型耗时 | 说明 |
|---|---|---|
| 采集 + 编码 | 10–100ms | 硬编更快 |
| 接入 | 50ms–2s | RTMP/SRT/WHIP 差异大 |
| 转码 | 50–500ms | 不做转码就是 0 |
| 打包/分片 | HLS 2–10s；FLV/WebRTC ≈ 0 | 最大延迟来源之一 |
| 分发 | 本地 ≈ 0；CDN 20–200ms | 局域网可忽略 |
| 播放器缓冲 | 0.5–3s | HLS 通常缓冲几个 segment |

协议决定延迟下限，分片大小和播放器缓冲决定实际值。

## 协议

| 协议 | 传输 | 典型端到端延迟 | 适用 |
|---|---|---|---|
| RTMP | TCP | 1–3s（含分发常见 3–10s） | 编码器兼容性最好 |
| SRT | UDP + ARQ | 可调，约 0.5s–数秒 | 公网/弱网上行 |
| WebRTC/WHIP | UDP + DTLS-SRTP | <500ms | 真交互、浏览器推流 |
| HTTP-FLV | TCP/HTTP | 3–5s，可调低 | 低延迟网页播放 |
| HLS | HTTP | 8–30s，优化后 6–8s | 兼容性、iOS、CDN |
| LL-HLS | HTTP | 2–4s | 需要 CDN/服务器支持 partial segment；SRS 不提供 |
| WHEP | UDP + DTLS-SRTP | <500ms | 低延迟网页播放 |

## 双向会议链路（M2+）

1. 入会：浏览器 → Go 控制面（Room Code + Join Token）
2. 发布：浏览器 `--WHIP-->` SRS，每个 Participant/Track 一路 Stream
3. 编排：Go 返回该 Participant 的 WHEP 订阅列表；房间事件走 WebSocket
4. 订阅：浏览器 `--WHEP-->` SRS
5. 录制：SRS `rtc_to_rtmp` → 每个 Participant 一路 FFmpeg → Recording Track + 时间线
6. AI：Go → Redis Streams → Python worker（mlx-whisper）→ Transcript / Summary
7. 回放：Recording Track 派生混流文件，交给 Web 播放器

SRS 是 stream-based SFU；Roster、Subscription、Join Token 都由 Go 控制面拥有。`rtc_to_rtmp` 默认关闭，双向会议录制时打开；非 localhost 的 WHIP 必须 HTTPS。

## 覆盖边界

FFmpeg 提供：

- 采集（avfoundation/dshow/alsa 等）
- 编码（libx264、videotoolbox、AAC、Opus）
- 封装（FLV、MPEG-TS、MP4）
- 转码与滤镜
- RTMP/SRT 推流；不提供 WHIP

SRS 提供：

- 接入：RTMP、SRT、WHIP、GB28181
- 协议转换：RTMP/SRT → HLS、HTTP-FLV、WebRTC；WebRTC → RTMP/HLS
- HTTP 分发、CORS、HTTPS 配置
- callbacks：`on_publish`、`on_unpublish`、`on_play`、`on_stop`、`on_dvr`、`on_hls`
- HTTP API、Prometheus exporter、基础 DVR、edge/forward

两者都不提供：

- 用户/房间/权限系统
- 播放器产品（只有 demo）
- 转码码率阶梯编排与 ABR 母版
- VOD 目录、录制生命周期、云存储
- 自动 HTTPS、证书续期
- 进程守护、告警、客户端 QoE

## 来源

- AWS Streaming Media Lens: <https://docs.aws.amazon.com/wellarchitected/latest/streaming-media-lens/streaming-architecture.html>
- Mux low-latency: <https://www.mux.com/articles/low-latency-live-streaming-developers-guide-ll-hls-webrtc-cmaf>
- Mux ingest protocols: <https://www.mux.com/articles/rtmp-vs-srt-vs-webrtc-live-streaming-ingest-protocols>
- SRS docs: <https://ossrs.io/lts/en-us/docs/v6/doc/introduction>
- SRS vs Oryx: <https://ossrs.io/lts/en-us/docs/v6/doc/getting-started-oryx>
- FFmpeg devices: <https://ffmpeg.org/ffmpeg-devices.html>
- FFmpeg protocols: <https://ffmpeg.org/ffmpeg-protocols.html>
