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
pnpm exec nx run contracts:build   # node libs/contracts/validate.mjs
```

生成代码（Go `oapi-codegen`、TypeScript `openapi-typescript`、Python Pydantic）在后续切片接入；生成物不要手改（ADR-0006）。

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
- 控制面目前只有 `/healthz`，Room 与 Broadcast 接口在后续切片实现。
