# ooxx.ooo WebDAV 代理服务

这是一个基于 Go 编写的 WebDAV 代理服务，用于将 [ooxx.ooo](https://ooxx.ooo) 图床平台封装为标准 WebDAV 接口，支持通过 WebDAV 客户端（如 macOS Finder、RaiDrive、Cyberduck 等）上传、下载、删除和浏览图片。

---

## ✨ 功能特性

- **WebDAV 兼容**：完整支持 `GET`、`PUT`、`DELETE`、`PROPFIND` 方法。
- **自动 CSRF 处理**：每次上传前动态获取 `_xsrf` Cookie，绕过反爬机制。
- **本地数据库缓存**：使用 SQLite 存储文件元信息（包括原始文件名、CDN 链接、大小等）。
- **远程删除支持**：调用 ooxx.ooo 提供的删除链接实现远程清理。
- **基础认证保护**：仅允许通过 Basic Auth 的请求访问（可自定义账号密码）。
- **重试机制**：上传失败时自动重试最多 3 次，提升稳定性。
- **辅助 API**：提供 `/api/get-url?path=xxx.png` 接口，用于从原始文件名反查 CDN 路径。

---

## 🛠️ 开发环境要求

- Go 1.20+
- SQLite（通过 `modernc.org/sqlite` 驱动，无需外部依赖）
- 可访问互联网（需连接 `https://ooxx.ooo`）

---

## 🚀 快速部署

### 1. 克隆项目

```bash
git clone https://github.com/yourname/ooxx-webdav.git
cd ooxx-webdav
```

> 注：请将上述 URL 替换为你实际的仓库地址。

### 2. 修改认证凭据（可选但推荐）

在 `main.go` 中找到 `basicAuth` 函数，修改用户名和密码：

```go
if !ok || username != "your@email.com" || password != "YourSecurePassword!" {
```

建议使用强密码并避免提交到公开仓库。

### 3. 构建二进制文件

```bash
go build -o webdav-server .
```

### 4. 启动服务

```bash
./webdav-server
```

默认监听 `http://localhost:24876`。

> 如需修改端口，编辑 `main.go` 中的 `port := "24876"` 行。

### 5. 配置 WebDAV 客户端

- **地址**：`http://your-server-ip:24876/`
- **用户名**：你在代码中设置的邮箱
- **密码**：你在代码中设置的密码

> ⚠️ 建议通过反向代理（如 Nginx）添加 HTTPS 和 IP 限制以增强安全性。

---

## 🧪 开发与调试

### 本地运行（带日志）

```bash
go run main.go
```

程序会输出详细日志，包括：

- XSRF 获取状态
- 上传尝试次数
- 数据库操作
- 远程删除结果

### 支持的文件类型

`.jpg` / `.jpeg`
`.png`
`.gif`
`.webp`

其他格式将被拒绝。

### 数据库存储结构

| 字段          | 类型     | 说明                                    |
| ------------- | -------- | --------------------------------------- |
| `id`          | TEXT     | ooxx.ooo 返回的 slug（如 `ZGI5M`）      |
| `name`        | TEXT     | WebDAV 路径中的文件名（如 `photo.png`） |
| `filename`    | TEXT     | 原始上传文件名（同 `name`）             |
| `delete_url`  | TEXT     | ooxx.ooo 提供的删除链接                 |
| `image_url`   | TEXT     | CDN 图片直链                            |
| `uploaded_at` | DATETIME | 本地记录时间                            |
| `size`        | INTEGER  | 文件字节大小                            |

---

## 🔒 安全提示

- 本服务**未启用 HTTPS**，生产环境务必通过 Nginx/Caddy 添加 TLS。
- Basic Auth 凭据以明文传输，必须配合 HTTPS 使用。
- 不要将服务暴露在公网无防护状态下。
- 定期清理 `files.db` 避免敏感信息泄露。

---

## 📦 依赖说明

- `modernc.org/sqlite`：纯 Go 实现的 SQLite 驱动，零外部依赖。
- 标准库：`net/http`, `database/sql`, `encoding/json/xml`, `mime/multipart` 等。

无第三方 HTTP 客户端，完全使用 Go 原生能力模拟浏览器行为。

---

## 📝 示例：通过 curl 上传

```bash
curl -u "admin:123456" \
  --upload-file ./test.png \
  http://localhost:24876/test.png
```

上传后可通过 WebDAV 客户端或直接访问 `http://localhost:24876/test.png` 查看。

---

## 🔄 更新日志

✅ 支持文件大小 (`size`) 记录与 WebDAV `getcontentlength` 返回
✅ 修复 `displayname` HTML 转义问题
✅ 增加重试机制应对临时网络波动
✅ 新增 `/api/get-url` 辅助接口

---

## 📬 联系 & 贡献

欢迎提交 Issue 或 PR！  

---

> Copyright © 2025. All rights reserved.  
> This project is not affiliated with ooxx.ooo.
