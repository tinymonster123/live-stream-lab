# Architecture

设计语言：Module、Interface、Implementation、Depth、Seam、Adapter。

核心判断：FFmpeg 与 SRS 是两个 Adapter，不是一个架构。缺的是拥有会话生命周期的小接口深模块，以及围绕真实变化点的接缝。

## 三个平面

```text
┌──────────────────────────── Control plane ────────────────────────────┐
│                                                                       │
│   Admin / Web UI          StreamRegistry          AccessPolicy       │
│        │                       │                        │             │
│        └──────────┬────────────┴───────────┬────────────┘             │
│                   ▼                        ▼                          │
│             StreamSession  ◄──────  CallbackHandler  ◄──── SRS       │
│                   │                        │                          │
└───────────────────┼────────────────────────┼──────────────────────────┘
                    │ start / stop / status  │ authorize
                    ▼                        ▼
┌──────────────────────────── Media plane ──────────────────────────────┐
│                                                                       │
│  CaptureSource ──► Encoder ──► Publisher ──► IngestServer (SRS)       │
│       │               │            │               │                  │
│   camera/screen    copy/x264/   RTMP/SRT/WHIP      ├─► Transcoder     │
│   file/browser     videotoolbox                    │   (optional)     │
│                                                    ├─► Packager (SRS) │
│                                                    │   HLS/FLV/WHEP   │
│                                                    └─► Recorder       │
│                                                                       │
│  Playback.resolve ──► PlaybackDescriptor ──► Player (HLS/FLV/WHEP)    │
│                                                                       │
└───────────────────────────────────────────────────────────────────────┘
                    │ events / metrics
                    ▼
┌──────────────────────── Observability plane ──────────────────────────┐
│  Telemetry ──► Prometheus / Grafana / logs / alerts                   │
│  Supervisor ──► process health, port checks, disk checks              │
└───────────────────────────────────────────────────────────────────────┘
```

## 双向会议（M2+）

```text
Participant A ──WHIP──► SRS SFU ──WHEP──► Participant B
      ▲                    │
      │                    ├──rtc_to_rtmp──► FFmpeg recorder (per participant)
      │                    └──HLS/FLV──────► viewers
      │
      └──WebSocket──► Go control plane: Room actor / Roster / Subscription / JoinToken
```

- M1 用 `StreamSession`；M2+ 增加 `RoomSession`。
- T5 实现 `RoomSession`、Room Actor、Roster、Join Token 和 WebSocket；T6 接入 WHIP/WHEP：
  Participant 用 WHIP 推一路包含摄像头与麦克风的 Stream，控制面按已发布的 Track 生成 WHEP 订阅列表，
  Track 变化通过 `track.published` / `track.unpublished` 广播。
- T7 在媒体会话之上补交互状态：静音是 Participant 状态（`mute.changed`），摄像头开关复用 Track
  集合（关闭时不发布 `camera`）；二者都只改发送端的 Track `enabled`，不重新协商 SDP。显式 Leave 后
  用同一 `participantId` 重入会按 Join Token 重建 Roster 条目；1:1 帧延迟由 Playwright 读取视频帧里
  的帧序号测量。
- T8 把生命周期收进 Room actor：Participant 在信令 socket 上每 5 秒发一次心跳，15 秒没有心跳即
  `offline`（保留 `participantId` 与 Join Token，可重入）；没有任何 online Participant 时进入
  `closing`，60 秒后 `closed`。计时由 `RoomSession` 的单个 ticker 驱动，Room actor 只处理串行命令；
  host 可以踢人，被踢的 `participantId` 不能再加入，其信令 socket 以 `kicked` 关闭。
- `RoomSession`：Room 的控制面生命周期；拥有 Participant、Subscription、录制编排。
- `Room Actor`：每个 Room 一个 goroutine，串行处理 join/leave/tracks/mute/subscribe。
- `Signaling`：WebSocket 房间事件 + WHIP/WHEP 订阅列表；不自己交换 SDP。客户端报告 WHIP 成功后通过
  `PUT /rooms/{code}/tracks` 更新 Track 集合。
- `MeetingRecorder`：每个 Participant 一路 FFmpeg，产出 Recording Track + 时间线。
- `AIJobQueue`：Redis Streams 生产/消费；消息是 `AI Job`。
- `ArtifactStore`：Transcript / Summary 的文件与元数据。
- `JoinToken`：HMAC 签发与校验；SRS callback 只做翻译。
- Go 实现中，`RoomSession` 是 `internal/room.Session`，Broadcast 是 `internal/broadcast.Service`，
  HTTP/WS 适配在 `internal/api`；领域包不 import `net/http`。

```ts
interface RoomSession {
  create(spec: RoomSpec): Promise<RoomHandle>;
  join(roomId: string, principal: Principal): Promise<JoinGrant>;
  updateTracks(roomId: string, participantId: string, tracks: RoomTrack[]): Promise<void>;
  updateMute(roomId: string, participantId: string, muted: boolean): Promise<void>;
  leave(roomId: string, participantId: string): Promise<void>;
  heartbeat(roomId: string, participantId: string): Promise<void>;
  kick(roomId: string, hostParticipantId: string, participantId: string): Promise<void>;
  status(roomId: string): RoomStatus;
}

type JoinGrant = {
  participantId: string;
  joinToken: string;
  publishUrl: string;             // WHIP endpoint for this Participant's Stream
  subscriptions: SubscriptionTarget[]; // WHEP targets for Tracks already published
};
```

调用方只表达“创建/加入/上报 Track/静音/离开/心跳/踢人/查询”，WHIP/WHEP、SRS stream 命名、
订阅扇出、心跳超时与空房计时都藏在实现里。

## 深模块

### StreamSession

```ts
interface StreamSession {
  start(spec: BroadcastSpec): Promise<SessionHandle>; // 幂等
  stop(streamId: string): Promise<void>;              // 幂等
  status(streamId: string): SessionStatus;            // 纯查询
}

type BroadcastSpec = {
  streamId: string;
  source: SourceSpec;
  latency: 'ultra-low' | 'low' | 'standard';
  record: boolean;
};
```

实现隐藏：协议选择、FFmpeg 进程生命周期、SRS 配置/端口/reload/健康检查、状态机、重试、失败补偿、清理。

删除测试：删掉它，FFmpeg flags 与 SRS 配置会回到 UI、API、CLI 和测试里，复杂度在 N 个调用点重现。

### Playback

```ts
interface Playback {
  resolve(streamId: string, viewer: ViewerContext): Promise<PlaybackDescriptor>;
}

type PlaybackDescriptor = {
  protocol: 'hls' | 'flv' | 'whep';
  url: string;
  expiresAt: number;
  fallbacks: PlaybackDescriptor[]; // 有序备选，可能为空
};
```

实现隐藏：协议选择与回退、token 签名、SRS URL 构造、过期时间。

T4 的实现把回退表达为描述符里的有序列表：`low` 是 HTTP-FLV 首选 + HLS 备选，`standard` 只给
HLS。**顺序**由服务端拥有；**最终选择**由播放器拥有，因为只有它能判断当前浏览器能否真正播放
（Safari 原生 HLS、其他浏览器没有 HLS 解码器）。因此这一版不做 User-Agent 嗅探，也不要求客户端
上报能力——`ViewerContext` 目前折叠进 Latency Class。等出现按观众区分的策略（签名 token、地理位置、
ABR 选码率）时，`resolve` 再显式接收 `ViewerContext`。

Viewer count 是 `StreamSession.status` 查询的一部分，由 `IngestProbe` 在 `live` 时按需读取 SRS
`/api/v1/streams/` 的 `clients` 并减去发布者；不在后台轮询，也不用媒体面调用阻塞 `start`/`stop`。

### AccessPolicy

```ts
interface AccessPolicy {
  authorize(
    action: 'publish' | 'play',
    principal: Principal,
    streamId: string
  ): Promise<Decision>;
}

type Decision = { allow: boolean; reason?: string; ttl?: number };
```

SRS callback 契约：HTTP 200 + `{"code":0}` 放行，非 0 拒绝。`CallbackHandler` 负责把 `on_publish`/`on_play` 翻译成 `authorize`，再把 `Decision` 翻译回 SRS 响应。

### Recorder

```ts
interface Recorder {
  start(session: SessionHandle, policy: RecordPolicy): Promise<RecordingHandle>;
  stop(recordingId: string): Promise<Recording>;
}
```

实现隐藏：DVR 配置、文件名、finalize、存储、清理、VOD 登记。当前只有 SRS DVR 一个适配器，保持具体实现。

### Telemetry

```ts
interface Telemetry {
  report(event: TelemetryEvent): void;
  snapshot(streamId: string): StreamMetrics;
}
```

实现隐藏：SRS exporter 抓取、播放端上报、聚合、告警阈值。当前只有日志 + Prometheus 一个适配器，保持具体实现。

## 接缝

“一个适配器只是假设的接缝，两个适配器才是真实的接缝。”

| 接缝 | 接口 | 现有适配器 | 第二个适配器 | 现在建接缝 |
|---|---|---|---|---|
| 采集 | `CaptureSource.open` | FFmpeg avfoundation、文件 | 浏览器 `getDisplayMedia` | 只有浏览器推流才建 |
| 编码 | `Encoder.encode` | copy、x264/videotoolbox | 转码 | 要 |
| 推流 | `Publisher.publish` | RTMP、SRT、WHIP | 已有 3 个 | 要 |
| 接入 | `IngestServer` | SRS | 只有 SRS | 不要，具体适配器 |
| 转码 | `Transcoder.transcode` | FFmpeg CLI | GPU/Oryx | 只有 ABR 才建 |
| 打包 | `Packager.package` | SRS | 只有 SRS | 不要，具体适配器 |
| 播放 | `Playback.resolve` | HLS、FLV、WHEP | 已有 3 个 | 要 |
| 录制 | `Recorder.start/stop` | SRS DVR | 对象存储/外部 FFmpeg | 先具体 |
| 鉴权 | `AccessPolicy.authorize` | 本地 allowlist、token/JWT | 有 2 种策略 | 要 |
| 注册表 | `StreamRegistry` | SQLite/JSON | Postgres | 先具体 |
| 监控 | `Telemetry.report` | 日志 + exporter | Prometheus/Grafana | 先具体 |

## 缺失模块

### P0：没有就跑不起来

| 模块 | 缺失后果 |
|---|---|
| `StreamSession` | 开播逻辑散落在 UI/脚本/API，状态不一致，失败无法回收 |
| `RoomSession` + `Room Actor` | 房间状态、订阅扇出、断线重连没有归属；T5 实现 Roster 与 join/leave，T8 实现心跳、offline、空房关闭与踢人 |
| `Signaling` | 客户端不知道发布/订阅哪些 Stream；T5 已实现 Roster 事件，T6 已实现 Track 上报与 WHEP 订阅列表 |
| `MeetingRecorder` | 无法按 Participant 落盘，AI 没有输入 |
| `AIJobQueue` + `ArtifactStore` | AI 与录制链路断开，产物不可查 |
| `CaptureSource` | 换设备要改命令行；权限问题运行时才爆 |
| `Encoder` | 每个调用点都要学 `-g/-re/-tune`；copy/转码策略无法统一 |
| `Publisher` | 断线后流就死；加 SRT/WHIP 要改所有调用点 |
| `IngestServer` | 端口、配置、reload、守护散落；SRS 崩了没人知道 |
| `Playback` + Player | 没有真正播放端；兼容、重连、ABR 全要重做 |
| `AccessPolicy` + `CallbackHandler` | 任何人都能推流/拉流；token 与 SRS JSON 耦合 |
| `StreamRegistry` | 没有房间/流 ID/开播状态/权限归属 |

### P1：没有就不好运营

| 模块 | 作用 |
|---|---|
| `Recorder` + VOD 目录 | 存储策略、清理、FLV→MP4、回放索引、录制鉴权 |
| `Telemetry` + 面板/告警 | 抓取 exporter、播放端 QoE、告警 |
| `Supervisor` | 进程守护、端口检查、磁盘水位、日志轮转 |
| `TLS/ReverseProxy` | SRS 不自动申请证书；局域网手机访问需要 HTTPS |
| `Config`/secrets | stream key、证书、回调密钥集中管理 |

### P2：按产品需要

| 模块 | 触发条件 |
|---|---|
| `Transcoder` / ABR | 观众网络差异大，必须多码率 |
| `Chat` / 互动 | 弹幕、连麦、点赞；连麦另引入信令与回声消除 |
| `Edge` / 多节点 | 单机 SRS 撑不住观众规模 |
| `Analytics` | 需要观看时长、留存、卡顿分析 |

## 测试

- `StreamSession`：用假的 `CaptureSource`/`Encoder`/`Publisher`/`IngestServer` 测状态机、幂等、重试、清理。
- `Playback.resolve`：表驱动 viewer context（iOS/Chrome/局域网/弱网），断言 protocol/url/expiry。
- `AccessPolicy`：有效/过期/错房间/已撤销 token。
- 适配器契约测试：每个 `Publisher` 通过同一组 connect/publish/reconnect/stop；每个 `Playback` 描述符能被对应播放器消费。
- 集成 smoke：一条真实 FFmpeg → SRS → 播放器；一条 callback 拒绝推流。

接口就是测试面。测试跨过同一接缝，不进入 SRS 或 FFmpeg 内部。

## 边界规则

- 核心模块不 import FFmpeg/SRS 适配器；依赖方向从控制面指向媒体面。
- SRS 是独立进程，不链接；通过配置、HTTP API、callbacks 集成。
- 单适配器模块保持具体；第二个适配器出现再抽接口。
- 每个失败路径有可观测事件；`StreamSession` 的状态可查询、可恢复。
