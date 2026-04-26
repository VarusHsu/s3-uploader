# s3-uploader

一个基于 Go + Gin 的 S3 预签名上传服务，内置拖拽上传前端页面。

## 运行

```bash
export AWS_REGION=us-west-2
export LISTEN_ADDR=:50001
# 生产建议填写明确域名，多个域名用逗号分隔
export CORS_ALLOW_ORIGINS=http://localhost:50001,http://localhost:5173

go run .
```

启动后访问：`http://localhost:50001/`

## 前端上传流程

1. 前端拖拽/选择文件。
2. 调用后端 `POST /upload-url` 获取预签名 URL。
3. 浏览器直接 `PUT` 到 S3。

## 跨域配置说明

前端直传 S3 时会有两段跨域：

- 前端页面 -> 本服务 (`/upload-url`)
- 前端页面 -> S3 预签名地址 (`PUT`)

### 1) API CORS

通过环境变量 `CORS_ALLOW_ORIGINS` 控制允许来源：

- `*`：允许所有来源（仅开发调试建议）
- `https://app.example.com,https://admin.example.com`：生产白名单

### 2) S3 Bucket CORS

需要在桶上配置 CORS（示例）：

```json
[
  {
	"AllowedHeaders": ["*"],
	"AllowedMethods": ["PUT", "GET", "HEAD"],
	"AllowedOrigins": [
	  "http://localhost:50001",
	  "http://localhost:5173"
	],
	"ExposeHeaders": ["ETag"],
	"MaxAgeSeconds": 600
  }
]
```

如果前端和 API 不同域名，`CORS_ALLOW_ORIGINS` 与 S3 `AllowedOrigins` 都要包含前端域名。

