# live-stream-lab

本地直播实验台。媒体面使用 `../FFmpeg` 与 `../srs`；本仓库围绕它们做控制面、播放端和运维，不重写媒体面。

## Agent skills

### Issue tracker

Issue 存在本仓库的 GitHub Issues，使用 `gh` CLI。见 `docs/agents/issue-tracker.md`。

### Triage labels

五个默认 triage 角色，标签名与角色同名。见 `docs/agents/triage-labels.md`。

### Domain docs

Single-context：根 `CONTEXT.md` + `docs/adr/`。见 `docs/agents/domain.md`。

## 先读

- `CONTEXT.md` — 领域词汇表。命名概念、写 issue、写测试前读。
- `docs/pipeline.md` — 完整 12 段 pipeline、双向会议链路、延迟预算、FFmpeg/SRS 的覆盖边界。改动采集、接入、打包或播放前读。
- `docs/architecture.md` — 模块、接口、接缝、缺失模块。新增功能、协议或适配器前读。
- `docs/decisions.md` — ADR 索引、开放问题、里程碑。选定客户端或改变范围前读。
- `docs/adr/` — 已接受的架构决策。

## 当前阶段

里程碑：M1 单向 Broadcast → M2 1:1 Meeting + 录制 + AI → M3 2–6 人 Room。

M1 已完成：文件/摄像头 → FFmpeg `-c copy` → RTMP → SRS → HTTP-FLV/HLS → 浏览器播放器，含观众数与 CI。
M2 进行中：T5 实现 Room 控制面（Room Code、Join Token、Roster、WebSocket join/leave）；T6 接入
WHIP 推流、WHEP 订阅、Track 上报、Caddy HTTPS 与浏览器本地/远端画面；T7 完成双向 1:1（静音、
摄像头开关、离开/重入、帧延迟测量）；T8 完成 Room 生命周期加固（心跳、offline、空房关闭、踢人）。
下一步 T9（每个 Participant 一路录制）。

Spec: [issue #1](https://github.com/tinymonster123/live-stream-lab/issues/1)。

## 代码结构

- `apps/control-plane/cmd/control-plane/main.go`：环境变量、装配与监听；只调用各包构造函数
- `apps/control-plane/internal/api/`：HTTP / WebSocket 适配层（`router.go`、`respond.go`、`broadcast.go`、`room.go`、`socket.go`）
- `apps/control-plane/internal/room/`：Room 领域（`session.go`、`actor.go`、`token.go`）
- `apps/control-plane/internal/broadcast/`：Broadcast 领域（`service.go`、`ffmpeg.go`、`probe.go`、`playback.go`）
- `apps/control-plane/internal/contracts/`：`contracts:generate` 产出的 Go 模型，不要手改
- `apps/control-plane/internal/id/`：跨领域共用的随机 ID
- `apps/web/src/components|hooks|lib/`：React 面板、hook、API 客户端与媒体/播放逻辑
- `apps/ai-worker/src/ai_worker/`：Python `src` layout

依赖方向固定为 `cmd → api → {room, broadcast} → contracts`；领域包不 import `net/http`。

## 不变量

- `StreamSession` 拥有 Broadcast 开播生命周期，调用方只用 `start/stop/status`；`RoomSession` 拥有
  Meeting 生命周期，调用方只用 `create/join/updateTracks/updateMute/leave/heartbeat/kick/status`。
- `Playback.resolve` 拥有协议选择；调用方只拿 `PlaybackDescriptor`。
- `AccessPolicy` 拥有 allow/deny；SRS callback 只做翻译。
- SRS 是 SFU；Go 控制面拥有 Roster、Subscription 和 Join Token。
- 每个 Participant 一路 FFmpeg 录制；AI worker 原生运行以保留 Metal/MLX。
- 基础设施用 Compose，Go 控制面和 Python AI worker 跑原生。
- 跨语言契约只在 `libs/contracts` 定义（OpenAPI + JSON Schema）。
- 第二个适配器出现才建接缝；单适配器模块保持具体实现。
- 源是 H.264/AAC 且只需一路时用 `-c copy`；转码由测量结果触发。

## 版本

- 运行：SRS `v6.0-r2` + Homebrew FFmpeg（见 ADR-0008）。
- 本地源码（只读/实验）：`../FFmpeg` `n9.1-dev`、`../srs` `v8.0-d0-13-g205cca3b4`（develop，unstable）。
