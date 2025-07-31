# hikarinagi image process service(HIPS)

切图服务，libvips负责核心的图片处理，gin负责网络请求处理

```
hips/
├── cmd/server/
├── internal/
│   ├── cache/
│   ├── config/
│   ├── handler/
│   ├── server/
│   └── service/
├── pkg/
│   ├── concurrent/
│   ├── errors/
│   └── imaging/
├── scripts/
└── docker/
```

## 快速开始

### 环境要求

- Go 1.24+
- libvips
- Cloudflare R2 账户

### 安装依赖

```bash
sudo apt-get install libvips-dev
```

### 环境变量

#### 基础配置

```bash
export R2_ENDPOINT=https://your-account-id.r2.cloudflarestorage.com
export R2_ACCESS_KEY=your-access-key
export R2_SECRET_KEY=your-secret-key
export R2_BUCKET=your-bucket-name
export PORT=8080
```

#### 并发处理配置（可选）

```bash
export MAX_WORKERS=16          # 最大worker数，默认为CPU核心数*2
export MAX_QUEUE_SIZE=160      # 最大队列大小，默认为MAX_WORKERS*10
export TASK_TIMEOUT=30s
export ENABLE_ASYNC=true       # 启用异步处理，默认true
export BUFFER_SIZE=100         # 缓冲区大小，默认100
```

#### 网络连接优化配置（可选）

```bash
export NET_MAX_IDLE_CONNS=100              # 最大空闲连接数，默认100
export NET_MAX_IDLE_CONNS_PER_HOST=20      # 每个host的最大空闲连接数，默认20
export NET_MAX_CONNS_PER_HOST=50           # 每个host的最大连接数，默认50
export NET_DIAL_TIMEOUT=10s                # 连接超时，默认10秒
export NET_KEEP_ALIVE=30s                  # Keep-Alive时间，默认30秒
export NET_IDLE_CONN_TIMEOUT=90s           # 空闲连接超时，默认90秒
export NET_REQUEST_TIMEOUT=60s             # 整体请求超时，默认60秒
export NET_DISABLE_COMPRESSION=false       # 是否禁用压缩，默认false
```

### 构建和运行

```bash
./scripts/build.sh
# 或者直接构建
go build -o bin/hips cmd/server/main.go

# 运行
./bin/hips
```

## 使用方法

### 图片处理

```
GET /{图片路径}?参数
```

支持的参数：
- `w`: 宽度 (最大 2048px)
- `h`: 高度 (最大 2048px)
- `q`: 质量 (1-100，默认 85)
- `f`: 格式 (jpeg/png/webp/avif)
- `crop`: 裁剪模式 (fit/fill/crop)
- `gravity`: 重心位置 (center/north/south/east/west)
- `blur`: 模糊程度 (0-100，默认 0)

示例：
```
http://localhost:8080/path/to/image.jpg?w=300&h=200&q=85&f=webp&blur=2
```

### 健康检查

```
GET /health
```

## 许可证

MIT License