# Development

## 工具链

| 工具 | 版本 | 用途 |
|---|---|---|
| Node | 24.20.0（>= 22.12） | Web、Nx 运行环境 |
| pnpm | 11.24.0 | JavaScript 包管理 |
| Go | 1.27.1（模块声明 1.24） | 控制面 |
| uv | 0.12.7 | Python 虚拟环境与依赖 |
| Python | 3.13 | AI worker（`.python-version`） |
| Docker Desktop | 29.8.2 | SRS、Redis、Caddy |
| SRS | `v6.0-r2` 镜像 | 媒体面（ADR-0008） |
| FFmpeg | Homebrew 版 | 控制面拉起推流进程（ADR-0008） |

## 首次安装

```bash
pnpm install
cd apps/ai-worker && uv sync --all-groups
```

`pnpm install` 会安装 Nx、Vite、Vitest、React、TypeScript 和 Biome。Go 依赖在 `apps/control-plane` 下用 Go modules 管理；AI worker 的依赖由 `uv.lock` 固定。

## 启动基础设施

```bash
pnpm infra:up
```

这一条命令等价于 `docker compose -f infra/compose.yaml up -d --wait`，会等待 SRS、Redis、Caddy 的健康检查全部通过。

```bash
pnpm infra:ps     # 状态
pnpm infra:logs   # 跟踪日志
pnpm infra:down   # 停止并移除容器
```

验证：

```bash
curl http://localhost:1985/api/v1/versions   # SRS HTTP API
curl http://localhost:8088/                  # Caddy
```

### Docker CLI 不在 PATH

本机的 Docker Desktop CLI 位于 `/Applications/Docker.app/Contents/Resources/bin`，默认不在 `PATH` 上。`scripts/compose.sh` 会自动定位 `docker`，并把同目录加入 `PATH`，因此凭证助手 `docker-credential-desktop` 也能被找到。直接调用 `docker compose` 时需要自己处理这两点。

### SRS 的 CANDIDATE

`infra/srs/srs.conf` 使用环境变量 `$CANDIDATE`，Compose 默认设为 `127.0.0.1`，只适合本机浏览器。局域网其他设备播放 WebRTC 时，用 Mac 的局域网 IP 启动：

```bash
SRS_CANDIDATE=192.168.1.20 pnpm infra:up
```

RTMP 接入和 HTTP-FLV/HLS 不受这个变量影响。

## Broadcast（M1）

控制面拥有 Broadcast 生命周期：它返回 publish/playback 描述，并在 `start` 时拉起 FFmpeg、在
`stop` 时结束进程。

```bash
# 生成一个 H.264/AAC fixture
ffmpeg -f lavfi -i testsrc=size=640x360:rate=30 -f lavfi -i sine=frequency=440 \
  -t 30 -c:v libx264 -pix_fmt yuv420p -c:a aac /tmp/fixture.mp4

pnpm exec nx run control-plane:dev
```

```bash
curl -s -X POST localhost:8081/api/v1/broadcasts \
  -H 'content-type: application/json' \
  -d '{"source":{"type":"file","path":"/tmp/fixture.mp4","loop":true},"latencyClass":"low"}'

curl -s -X POST localhost:8081/api/v1/broadcasts/<broadcastId>/start
curl -s localhost:8081/api/v1/broadcasts/<broadcastId>
curl -s -X POST localhost:8081/api/v1/broadcasts/<broadcastId>/stop
```

控制面返回的 `playback` 是一个有序策略：`protocol` 和 `url` 是首选协议，`fallbacks` 是备选。
`low` 返回 HTTP-FLV 首选、HLS 备选；`standard` 只返回 HLS。Web 播放器按顺序尝试自己能播放的协议
（HTTP-FLV 用 `mpegts.js`，HLS 用 Safari 原生支持），因此不需要服务端猜测客户端能力。也可以把
`publish.rtmpUrl` 和 `publish.streamKey` 交给 OBS 或其他 FFmpeg。

`source.type`：

- `file`：`path` 是控制面所在机器上的路径；`loop` 为 `true` 时循环播放。
- `camera`：`path` 是 avfoundation 设备（默认 `0:0`），需要给终端授予摄像头/麦克风权限。

`start` 会等待 SRS API 报告该 Stream 处于 publishing 状态后才返回 `live`；等待超时会结束 FFmpeg
并把状态置为 `failed`。文件源必须是 H.264/AAC（控制面用 `-c copy`）。

`ultra-low`（WHEP）尚未实现，创建时返回 400；它在 T6 落地。

### HLS

SRS 的 HLS 在 `infra/srs/srs.conf` 的 `hls` 块开启，播放地址形如
`http://127.0.0.1:8080/live/<streamKey>.m3u8`。分片边界跟随关键帧，`hls_fragment` 只是上限；
`hls_cleanup on` 在直播中删除过期分片，`hls_dispose 60` 在停播 60 秒后删除该 Stream 的全部分片和
播放列表。分片写在容器内的 `./objs/nginx/html`，不挂载到宿主机，容器重建即清空。

Safari 用原生 HLS 播放；Chrome/Firefox 需要 MSE 版 HLS 库，暂不支持（会显示播放错误）。

回退发生在首选协议无法启动时（HTTP-FLV 5 秒内没有画面、或 HLS 10 秒内没有元数据）。已经起播后
掉流只显示播放错误，由状态轮询与 Offline 状态接管，不做自动重连。

### Viewer count 与状态

`GET /api/v1/broadcasts/{broadcastId}` 返回 `status` 和 `viewerCount`。`viewerCount` 来自 SRS
`/api/v1/streams/` 的 `clients` 字段，只在 `live` 时按需查询；查询失败时保留上一次的已知值，
不阻塞 `start`/`stop`。SRS 把发布者也计入 `clients`，控制面会减掉它；剩下的仍是连接数（一次
HLS 播放会打开多个连接），不是去重后的观众人数。Web 端在 `starting`/`live`/`stopping` 期间每 3
秒轮询一次，停播或失败后显示 Offline 状态并销毁播放器。

### 验收脚本

```bash
DURATION=10 scripts/soak-broadcast.sh /tmp/fixture.mp4   # 快速冒烟
scripts/soak-broadcast.sh /tmp/fixture.mp4               # 默认 600 秒
```

脚本会创建并启动 Broadcast，每 5 秒检查状态仍为 `live` 且 `playback.url` 返回 FLV 数据，最后停止。

### 控制面环境变量

| 变量 | 默认值 | 作用 |
|---|---|---|
| `CONTROL_PLANE_ADDR` | `:8081` | HTTP 监听地址 |
| `SRS_RTMP_BASE` | `rtmp://127.0.0.1:1935/live` | 推流基地址 |
| `SRS_FLV_BASE` | `http://127.0.0.1:8080/live` | HTTP-FLV 播放基地址 |
| `SRS_HLS_BASE` | `http://127.0.0.1:8080/live` | HLS 播放基地址 |
| `SRS_API_BASE` | `http://127.0.0.1:1985` | SRS HTTP API：确认 Stream 已开始推流、踢掉发布者、查询观众数；设为空可跳过确认 |
| `FFMPEG_BIN` | `ffmpeg` | FFmpeg 可执行文件 |

局域网观看时，把 `SRS_FLV_BASE`、`SRS_HLS_BASE` 改成 `http://<Mac LAN IP>:8080/live` 并设置
`SRS_CANDIDATE=<Mac LAN IP>`。

## 原生应用

Compose 只跑基础设施。Go 控制面和 Python worker 原生运行：

```bash
pnpm exec nx run web:dev            # Vite, http://localhost:5173
pnpm exec nx run control-plane:dev  # Go, http://localhost:8081/healthz
pnpm exec nx run ai-worker:dev      # Python 占位入口
```

Linux 容器拿不到 Metal/MLX，所以 AI worker 不能放进 Compose（ADR-0003、ADR-0004）。

## 测试与构建

```bash
pnpm test        # nx run-many -t test
pnpm build       # nx run-many -t build
pnpm typecheck   # nx run-many -t typecheck
pnpm lint        # biome check .
pnpm format      # biome format --write .
```

各项目的测试：

- `web`：Vitest，`react-dom/server` 渲染断言，不依赖 jsdom。
- `control-plane`：`go test ./...`，用 `httptest` 驱动 `newMux()`。
- `ai-worker`：`uv run --frozen pytest`。
- `contracts`：`node --test`，校验 OpenAPI 文档结构、内部 `$ref` 和三份 JSON Schema。

只跑一个项目：

```bash
pnpm exec nx run web:test
pnpm exec nx run control-plane:test
pnpm exec nx run ai-worker:test
pnpm exec nx run contracts:test
```

## CI

`.github/workflows/ci.yml` 在 pull request、push 到 `main` 和手动触发时运行三个并行 job：

| Job | 内容 |
|---|---|
| Build, test, typecheck, lint | `nx affected` 跑受影响项目的 build/test/typecheck；`go test -race ./...`；`biome check .` |
| Playwright smoke | 安装 Chromium，启动 Vite，检查首屏与 Broadcast 表单 |
| Compose smoke | 用官方 Docker Hub 镜像启动 SRS/Redis/Caddy，等待健康检查，验证三个服务后清理 |

`nx affected` 在 PR 上用 base/head SHA，在 push 上用 `before`/`sha`。CI 通过 `SRS_IMAGE`、
`REDIS_IMAGE`、`CADDY_IMAGE` 覆盖 Compose 默认的 `docker.m.daocloud.io` 前缀。

本地等价命令：

```bash
pnpm install --frozen-lockfile
(cd apps/ai-worker && uv sync --all-groups --frozen)
pnpm exec nx affected -t build test typecheck
pnpm exec playwright install chromium
pnpm exec nx run web:e2e
pnpm lint
```

## 契约

`libs/contracts` 是跨语言 payload 的唯一来源：

```text
libs/contracts/
  openapi.json          # HTTP + WebSocket（x-websocket 扩展）
  schemas/ai-job.json
  schemas/transcript.json
  schemas/summary.json
  validate.mjs          # 结构与 $ref 校验
```

```bash
pnpm exec nx run contracts:build     # node libs/contracts/validate.mjs
pnpm exec nx run contracts:generate  # 重新生成 Go / TypeScript 绑定
```

`contracts:generate` 产出两个已提交的生成文件：

- `apps/control-plane/models.gen.go`（`oapi-codegen`）
- `libs/contracts/generated/api.d.ts`（`openapi-typescript`）

生成物不要手改（ADR-0006）。Go 生成步骤默认使用 `GOPROXY=https://goproxy.cn`，可用同名环境变量覆盖。

## 网络限制与镜像

这台机器直连部分上游会超时或被 TLS 重置，仓库里已经按可用路径配置：

- **Docker Hub**：Compose 镜像使用 `docker.m.daocloud.io` 前缀。若换成可直连的网络，把前缀去掉即可。
- **PyPI**：`apps/ai-worker/pyproject.toml` 的 `[[tool.uv.index]]` 默认使用清华镜像。其他网络可用 `UV_DEFAULT_INDEX=https://pypi.org/simple` 覆盖。
- **python-build-standalone**（`uv python install` 的下载源）：GitHub Release 下载被阻断。可用 Homebrew 安装 3.13（`brew install python@3.13`），或从
  `https://registry.npmmirror.com/-/binary/python-build-standalone/20260825/`
  用 `curl` 下载对应 tarball，解压到
  `~/.local/share/uv/python/cpython-3.13.15-macos-aarch64-none`（去掉顶层 `python/` 目录）。
- **Hugging Face**：后续 MLX 模型下载走 `hf-mirror.com`。

## 已知边界

- SRS 端口映射到 0.0.0.0，局域网设备需要访问；只应在可信局域网内使用。
- Redis 和 Caddy admin API 只绑定到 `127.0.0.1`，不对局域网暴露。
- Caddy 在 T1 只是占位响应；HTTPS、内部 CA 与路由在 M2 接入。
- 控制面目前实现 Broadcast 的 create/start/stop/status 与 `/healthz`；Room 接口在后续切片实现。
- Broadcast 状态保存在内存中，控制面重启会丢失。
