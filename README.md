# live-stream-lab

本地直播/会议实验台。SRS 是媒体面，FFmpeg 负责采集与录制，Go 控制面拥有会话生命周期，Python AI worker 做会后转写与摘要，Web 是第一个客户端。

架构、pipeline 与决策见：

- [`CONTEXT.md`](CONTEXT.md) — 领域词汇表
- [`docs/architecture.md`](docs/architecture.md) — 模块、接口、接缝
- [`docs/pipeline.md`](docs/pipeline.md) — 12 段链路与延迟预算
- [`docs/decisions.md`](docs/decisions.md) / [`docs/adr/`](docs/adr/) — 架构决策
- [`docs/dev.md`](docs/dev.md) — 开发环境与命令

## 快速开始

前置：Node >= 22.12、pnpm 11、Go >= 1.24、uv、Docker Desktop、FFmpeg（版本见 [`docs/dev.md`](docs/dev.md)）。

```bash
pnpm install
pnpm infra:up
```

`pnpm infra:up` 一条命令启动 SRS、Redis、Caddy，并等待三个容器通过健康检查。`pnpm infra:ps` 查看状态，`pnpm infra:down` 停止。

Go 控制面和 Python AI worker 不在 Compose 里：它们原生运行，AI worker 才能使用 Metal/MLX（ADR-0004）。

## M1 Broadcast

```bash
pnpm infra:up
pnpm exec nx run control-plane:dev   # 终端 A
pnpm exec nx run web:dev             # 终端 B
```

打开 <http://localhost:5173>，选择文件或摄像头源，创建并启动 Broadcast。控制面拉起 FFmpeg
推流到 SRS，浏览器通过 HTTP-FLV 播放。curl 流程、fixture 生成和局域网配置见
[`docs/dev.md`](docs/dev.md#broadcastm1)。

## M2 Meeting

同样是两个原生进程。打开两个标签页：一个 Create Meeting，另一个填 Room Code 加入。两个标签页各自
用 WHIP 推摄像头/麦克风，并通过 WHEP 订阅对方，页面上同时显示本地预览和远端画面；Mute/Camera 按钮
控制自己的麦克风和摄像头，Leave 之后可以 Rejoin，并复用同一个 participantId。

局域网设备需要 HTTPS：用 `SITE_ADDRESS=<LAN IP> SRS_CANDIDATE=<LAN IP> pnpm infra:up` 启动基础设施，
访问 `https://<LAN IP>:8443`，并按 [`docs/dev.md`](docs/dev.md#caddy-https局域网设备) 信任 Caddy 的内部 CA。
完整的 curl 流程、Track/静音上报、1:1 验收和延迟测量见
[`docs/dev.md`](docs/dev.md#meetingm2-t5t7)。

## 常用命令

| 命令 | 作用 |
|---|---|
| `pnpm infra:up` / `down` / `ps` / `logs` | 启动、停止、查看基础设施 |
| `pnpm test` | 运行全部项目的测试 |
| `pnpm build` | 构建 Web、Go 控制面、Python 包，并校验契约 |
| `pnpm typecheck` | TypeScript 类型检查 |
| `pnpm lint` / `pnpm format` | Biome 检查 / 格式化 |
| `pnpm exec nx run web:dev` | 启动 Vite 开发服务器（5173） |
| `pnpm exec nx run control-plane:dev` | 原生启动 Go 控制面（8081） |
| `pnpm exec nx run ai-worker:dev` | 原生启动 Python worker 的占位入口 |

## 目录

```text
apps/
  web/              # Vite + React 客户端
  control-plane/    # Go 控制面
  ai-worker/        # Python AI worker
libs/
  contracts/        # OpenAPI 3.1 + JSON Schema，跨语言唯一契约来源
infra/
  compose.yaml      # SRS + Redis + Caddy
  srs/srs.conf
  caddy/Caddyfile
scripts/
  compose.sh        # 自动定位 Docker Desktop CLI 的 compose 包装
```

## 端口

| 端口 | 服务 |
|---|---|
| 1935/tcp | SRS RTMP 接入 |
| 1985/tcp | SRS HTTP API |
| 8080/tcp | SRS HTTP-FLV / HLS |
| 8000/udp | SRS WebRTC |
| 6379/tcp | Redis |
| 2019/tcp | Caddy admin API |
| 8088/tcp | Caddy HTTP 入口 |
| 8081/tcp | Go 控制面（原生运行） |
| 5173/tcp | Vite 开发服务器 |

跨语言 payload 只在 [`libs/contracts`](libs/contracts) 定义；不要在其他地方手写重复类型。
