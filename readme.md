hikarinagi的图片处理服务(Hikarinagi Image Process Service)，[bimg](https://github.com/h2non/bimg) 负责核心的图片处理，[gin](https://github.com/gin-gonic/gin) 负责网络请求处理

## 快速开始

### 环境要求

- Go 1.24+
- libvips
- Cloudflare R2 账户

### 安装依赖

```bash
sudo apt install libvips-dev
sudo yum install libvips-dev
brew install vips (For MacOS)
```

### 环境变量

| 环境变量 | 描述 | 默认值 | 是否必须 |
|---------|------|--------|----------|
| `R2_ENDPOINT` | Cloudflare R2存储端点地址 | - | ✅ |
| `R2_ACCESS_KEY` | R2访问密钥 | - | ✅ |
| `R2_SECRET_KEY` | R2秘密密钥 | - | ✅ |
| `R2_BUCKET` | R2存储桶名称 | - | ✅ |
| `PORT` | 服务监听端口 | `8080` | ❌ |
| `MAX_WORKERS` | 最大worker数量 | CPU核心数×2 | ❌ |
| `MAX_QUEUE_SIZE` | 最大队列大小 | MAX_WORKERS×10 | ❌ |
| `TASK_TIMEOUT` | 任务执行超时时间 | `30s` | ❌ |
| `ENABLE_ASYNC` | 启用异步处理 | `true` | ❌ |
| `BUFFER_SIZE` | 缓冲区大小 | `100` | ❌ |
| `VIPS_CONCURRENCY` | libvips内部线程数 | CPU核心数 | ❌ |
| `VIPS_CACHE_SIZE` | libvips操作缓存数量 | `200` | ❌ |
| `VIPS_CACHE_MEM_MB` | libvips缓存内存限制(MB) | `256` | ❌ |
| `NET_MAX_IDLE_CONNS` | 最大空闲连接数 | `100` | ❌ |
| `NET_MAX_IDLE_CONNS_PER_HOST` | 每个host的最大空闲连接数 | `20` | ❌ |
| `NET_MAX_CONNS_PER_HOST` | 每个host的最大连接数 | `50` | ❌ |
| `NET_DIAL_TIMEOUT` | 连接超时时间 | `10s` | ❌ |
| `NET_KEEP_ALIVE` | Keep-Alive时间 | `30s` | ❌ |
| `NET_IDLE_CONN_TIMEOUT` | 空闲连接超时时间 | `90s` | ❌ |
| `NET_REQUEST_TIMEOUT` | 整体请求超时时间 | `60s` | ❌ |
| `NET_DISABLE_COMPRESSION` | 是否禁用压缩 | `false` | ❌ |
| `CACHE_L1_ENABLED` | 启用L1内存缓存 | `true` | ❌ |
| `CACHE_L2_ENABLED` | 启用L2 Redis缓存 | `true` | ❌ |
| `CACHE_L3_ENABLED` | 启用L3磁盘缓存 | `true` | ❌ |
| `CACHE_L1_MAX_MEMORY_MB` | L1最大内存限制(MB) | `1024` | ❌ |
| `CACHE_L2_MAX_MEMORY_MB` | L2最大内存限制(MB) | `3072` | ❌ |
| `CACHE_L3_MAX_DISK_GB` | L3最大磁盘空间(GB) | `10` | ❌ |
| `REDIS_ADDR` | Redis服务器地址 | `localhost:6379` | ❌ |
| `REDIS_PASSWORD` | Redis密码 | - | ❌ |
| `REDIS_DB` | Redis数据库编号 | `0` | ❌ |
| `CACHE_DISK_DIR` | 磁盘缓存目录 | `./cache` | ❌ |
| `CACHE_PROMOTE_THRESHOLD` | 缓存提升访问次数阈值 | `3` | ❌ |
| `CACHE_DEMOTE_THRESHOLD` | 缓存降级访问次数阈值 | `1` | ❌ |
| `CACHE_SYNC_INTERVAL` | 缓存同步间隔 | `5m` | ❌ |
| `THIRD_PARTY_PROVIDERS_JSON` | 第三方图片提供方列表(JSON数组) | - | ❌ |

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

### 第三方图片处理

通过配置第三方提供方后，可以直接请求远程图片地址：

1) 配置环境变量（注意JSON需使用双引号）：

```bash
export THIRD_PARTY_PROVIDERS_JSON='[{"name":"bangumi","allowed_hosts":["lain.bgm.tv"]}]'
```

2) 请求示例：

```http
GET /bangumi/https%3A%2F%2Flain.bgm.tv%2Fpic%2Fcover%2Fl%2F1c%2Fba%2F343241_TWFSN.jpg?w=600&h=400&f=avif&q=85
```

说明：
- 第一路径段为 `provider` 名称，对应 `name`
- 第二段为远程图片完整URL，建议URL编码；服务端也会尝试解码
- 仅当URL的 `host` 在对应 `allowed_hosts` 白名单中时才会拉取

### 健康检查

```
GET /health
```

### 部署

默认启用 `libvips` 的多线程处理，推荐部署在核心数不小于16，RAM不小于32GiB的实例上来达到最佳性能

### 缓存

- **L1 - 内存**: 最热门的图片，LRU策略，默认1GB
- **L2 - Redis**: 比较热门的图片，默认3GB
- **L3 - Disk**: 热门原图，默认10GB
- **L4 - CDN**: 主要缓存层

## 许可证

AGPL-3.0
