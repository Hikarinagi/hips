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
│   ├── errors/
│   └── imaging/
├── scripts/
└── docker/
```

## 快速开始

### 环境要求

- Go 1.21+
- libvips
- Cloudflare R2 账户

### 安装依赖

```bash
sudo apt-get install libvips-dev
```

### 环境变量

```bash
export R2_ENDPOINT=https://your-account-id.r2.cloudflarestorage.com
export R2_ACCESS_KEY=your-access-key
export R2_SECRET_KEY=your-secret-key
export R2_BUCKET=your-bucket-name
export PORT=8080  # 可选
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