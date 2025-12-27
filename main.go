package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/textproto"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	UPLOAD_API = "https://ooxx.ooo/upload"
	DB_PATH    = "ooxx.db"
)

type FileInfo struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Filename   string    `json:"filename"`
	DeleteURL  string    `json:"delete_url"`
	ImageURL   string    `json:"image_url"`
	UploadedAt time.Time `json:"uploaded_at"`
	Size       int64     `json:"size"` 
}

// 定义响应结构
type UploadFileResponse struct {
	ImageURL    string `json:"image_url"`
	DeleteURL   string `json:"delete_url"`
	Filename    string `json:"filename"`
	Slug        string `json:"slug"`
	DeleteToken string `json:"delete_token"`
}

var (
	xsrfCacheValue string
	xsrfCacheTime  time.Time
	xsrfMutex      sync.RWMutex
)

type Prop struct {
	Resourcetype *struct {
		Collection *struct{} `xml:"D:collection,omitempty"`
	} `xml:"D:resourcetype,omitempty"`
	Getcontentlength *int64 `xml:"D:getcontentlength,omitempty"`
	Getlastmodified  string `xml:"D:getlastmodified,omitempty"`
	Getcontenttype   string `xml:"D:getcontenttype,omitempty"`
	Displayname      string `xml:"D:displayname,omitempty"`
}

type Propstat struct {
	Prop   Prop   `xml:"D:prop"`
	Status string `xml:"D:status"`
}

type PropfindResponseItem struct {
	Href  string   `xml:"D:href"`
	Props Propstat `xml:"D:propstat"`
}

type PropfindResponse struct {
	XMLName   xml.Name               `xml:"D:multistatus"`
	XmlnsD    string                 `xml:"xmlns:D,attr"`
	Responses []PropfindResponseItem `xml:"D:response"`
}

func initDB() *sql.DB {
	db, err := sql.Open("sqlite", DB_PATH)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	_, err = db.Exec(`
	CREATE TABLE IF NOT EXISTS files (
		id TEXT PRIMARY KEY,
		name TEXT,
		filename TEXT,
		delete_url TEXT,
		image_url TEXT,
		uploaded_at DATETIME,
		size INTEGER  
	)
	`)
	if err != nil {
		log.Fatalf("Failed to create table: %v", err)
	}
	return db
}

func getFileName(r *http.Request) string {
	path := r.URL.Path
	if strings.HasPrefix(path, "/") {
		path = path[1:]
	}
	if strings.HasSuffix(path, "/") {
		path = path[:len(path)-1]
	}
	return path
}

func getFile(db *sql.DB, name string) (*FileInfo, error) {
	var info FileInfo
	err := db.QueryRow("SELECT id, filename, delete_url, image_url, uploaded_at, size FROM files WHERE Filename = ?", name).Scan(
		&info.ID, &info.Filename, &info.DeleteURL, &info.ImageURL, &info.UploadedAt, &info.Size,
	)
	if err != nil {
		return nil, err
	}
	info.Name = name
	return &info, nil
}

func listAllFiles() ([]*FileInfo, error) {
	db := initDB()
	defer db.Close()

	rows, err := db.Query("SELECT id, name, filename, delete_url, image_url, uploaded_at, size FROM files")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []*FileInfo
	for rows.Next() {
		var info FileInfo
		//err := rows.Scan(&info.ID, &info.Name, &info.Filename, &info.DeleteURL, &info.ImageURL, &info.UploadedAt)
		err := rows.Scan(&info.ID, &info.Name, &info.Filename, &info.DeleteURL, &info.ImageURL, &info.UploadedAt, &info.Size)
		if err != nil {
			return nil, err
		}
		files = append(files, &info)
	}
	return files, nil
}

func deleteFile(db *sql.DB, name string) (*FileInfo, error) {
	info, err := getFile(db, name)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec("DELETE FROM files WHERE name = ?", name)
	if err != nil {
		return nil, err
	}
	return info, nil
}

func getValidXSRF() (string, error) {

	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar:     jar,
		Timeout: 1 * time.Second,
	}

	req, err := http.NewRequest("GET", "https://ooxx.ooo/upload", nil)
	if err != nil {
		return "", err
	}

	// 设置浏览器-like headers（保持不变）
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:145.0) Gecko/20100101 Firefox/145.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.8,zh-TW;q=0.7,zh-HK;q=0.5,en-US;q=0.3,en;q=0.2")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	req.Header.Set("Sec-GPC", "1")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch upload page: %w", err)
	}
	defer resp.Body.Close()

	// 从响应 cookies 中提取 _xsrf
	URL := &url.URL{Scheme: "https", Host: "ooxx.ooo"}
	for _, cookie := range jar.Cookies(URL) {
		if cookie.Name == "_xsrf" {
			log.Printf("✅ Fresh _xsrf: %.20s...", cookie.Value)
			return cookie.Value, nil
		}
	}


	log.Println("❌ No _xsrf cookie. Received cookies:")
	for _, c := range jar.Cookies(&url.URL{Scheme: "https", Host: "ooxx.ooo"}) {
		log.Printf("  %s=%s", c.Name, c.Value)
	}

	body, _ := io.ReadAll(resp.Body)
	if len(body) > 0 {
		snippet := string(body)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		log.Printf("Response snippet: %.200s...", snippet)
	}

	return "", fmt.Errorf("no _xsrf cookie found in response")
}


func handleGET(w http.ResponseWriter, r *http.Request) {
	name := getFileName(r)
	if name == "" && r.Method == "000" {
		// 根路径：返回支持 WebDAV 的 XML
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<D:multistatus xmlns:D="DAV:">
  <D:response>
    <D:href>/</D:href>
    <D:propstat>
      <D:prop>
        <D:displayname>Root</D:displayname>
        <D:getcontentlength></D:getcontentlength>
        <D:getlastmodified></D:getlastmodified>
        <D:creationdate></D:creationdate>
        <D:getetag></D:getetag>
        <D:resourcetype>
          <D:collection/>
        </D:resourcetype>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
</D:multistatus>`)
		return
	} else if name == "" {
		http.Error(w, "Not implemented", http.StatusNotImplemented)
		return
	}

	db := initDB()
	info, err := getFile(db, name)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// 代理图片内容
	resp, err := http.Get(info.ImageURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Printf("Proxy failed for %s: %v", info.ImageURL, err)
		http.Error(w, "Upstream unavailable", http.StatusServiceUnavailable)
		if resp != nil {
			resp.Body.Close()
		}
		return
	}
	defer resp.Body.Close()

	// 设置 Content-Type
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		ext := strings.ToLower(filepath.Ext(name))
		switch ext {
		case ".jpg", ".jpeg":
			contentType = "image/jpeg"
		case ".png":
			contentType = "image/png"
		case ".gif":
			contentType = "image/gif"
		case ".webp":
			contentType = "image/webp"
		default:
			contentType = "application/octet-stream"
		}
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")

	if r.Method == "GET" {
		io.Copy(w, resp.Body)
	}
	// HEAD: 只发 headers
}

func handlePROPFIND(w http.ResponseWriter, r *http.Request) {
	name := getFileName(r)

	if name == "" {
		// 列出根目录（collection）
		files, err := listAllFiles()
		if err != nil {
			http.Error(w, "DB error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		responses := []PropfindResponseItem{
			{
				Href: "/", // 根 collection，href 为 "/"
				Props: Propstat{
					Prop: Prop{
						Resourcetype: &struct {
							Collection *struct{} `xml:"D:collection,omitempty"`
						}{
							Collection: &struct{}{},
						},
						Displayname: "/", 
					},
					Status: "HTTP/1.1 200 OK",
				},
			},
		}

		for _, info := range files {
			href := "/" + url.PathEscape(info.Filename) // 文件 href 不以 / 结尾
			ext := strings.ToLower(filepath.Ext(info.Filename))
			contentType := "application/octet-stream"
			switch ext {
			case ".jpg", ".jpeg":
				contentType = "image/jpeg"
			case ".png":
				contentType = "image/png"
			case ".gif":
				contentType = "image/gif"
			case ".webp":
				contentType = "image/webp"
			}

			prop := Prop{
				Getcontentlength: &info.Size,
				Getlastmodified:  info.UploadedAt.Format(time.RFC1123Z),
				Getcontenttype:   contentType,
				Displayname:      filepath.Base(info.Filename), // ✅ 移除 html.EscapeString
			}
			responses = append(responses, PropfindResponseItem{
				Href: href,
				Props: Propstat{
					Prop:   prop,
					Status: "HTTP/1.1 200 OK",
				},
			})
		}

		w.Header().Set("Content-Type", `application/xml; charset="utf-8"`)
		w.Header().Set("DAV", "1, 2")
		w.WriteHeader(http.StatusMultiStatus)
		xml.NewEncoder(w).Encode(PropfindResponse{
			XmlnsD:    "DAV:",
			Responses: responses,
		})
		return
	}

	// 查询单个文件
	db := initDB()
	info, err := getFile(db, name)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	href := "/" + url.PathEscape(info.Name)
	ext := strings.ToLower(filepath.Ext(info.Name))
	contentType := "application/octet-stream"
	switch ext {
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".png":
		contentType = "image/png"
	case ".gif":
		contentType = "image/gif"
	case ".webp":
		contentType = "image/webp"
	}

	prop := Prop{
		Getcontentlength: &info.Size,
		Getlastmodified:  info.UploadedAt.Format(time.RFC1123Z),
		Getcontenttype:   contentType,
		Displayname:      filepath.Base(info.Filename), 
	}

	response := PropfindResponseItem{
		Href: href,
		Props: Propstat{
			Prop:   prop,
			Status: "HTTP/1.1 200 OK",
		},
	}

	w.Header().Set("Content-Type", `application/xml; charset="utf-8"`)
	w.Header().Set("DAV", "1, 2")
	w.WriteHeader(http.StatusMultiStatus)
	xml.NewEncoder(w).Encode(PropfindResponse{
		XmlnsD:    "DAV:",
		Responses: []PropfindResponseItem{response},
	})
}

const maxUploadRetries = 3

func handlePUT(w http.ResponseWriter, r *http.Request) {
	log.Printf("PUT request received.")
	name := getFileName(r)
	if name == "" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	ext := strings.ToLower(filepath.Ext(name))
	allowed := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true}
	if !allowed[ext] {
		http.Error(w, "Unsupported file type", http.StatusBadRequest)
		return
	}

	content, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Read body failed", http.StatusInternalServerError)
		return
	}
	fileSize := int64(len(content))

	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar:     jar,
		Timeout: 60 * time.Second,
	}

	xsrfCookieValue, err := getValidXSRF()
	if err != nil {
		log.Printf("Failed to get XSRF: %v", err)
		http.Error(w, "CSRF setup failed", http.StatusInternalServerError)
		return
	}

	contentType := "application/octet-stream"
	switch ext {
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".png":
		contentType = "image/png"
	case ".gif":
		contentType = "image/gif"
	case ".webp":
		contentType = "image/webp"
	}

	var uploadResp []UploadFileResponse
	var lastErr error

	// 🔁 重试最多 maxUploadRetries 次
	for attempt := 1; attempt <= maxUploadRetries; attempt++ {
		log.Printf("📤 Upload attempt %d/%d for %s", attempt, maxUploadRetries, name)

		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)

		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="files[]"; filename="%s"`, name))
		h.Set("Content-Type", contentType)

		fileWriter, err := writer.CreatePart(h)
		if err != nil {
			lastErr = fmt.Errorf("create multipart part: %w", err)
			continue
		}
		if _, err = fileWriter.Write(content); err != nil {
			lastErr = fmt.Errorf("write content: %w", err)
			continue
		}

		if err = writer.WriteField("_xsrf", xsrfCookieValue); err != nil {
			lastErr = fmt.Errorf("write _xsrf: %w", err)
			continue
		}
		writer.Close()

		req, _ := http.NewRequest("POST", "https://ooxx.ooo/upload", &buf)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Referer", "https://ooxx.ooo/upload")
		req.Header.Set("Origin", "https://ooxx.ooo")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		req.Header.Set("Cookie", "_xsrf="+url.QueryEscape(xsrfCookieValue))
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:145.0) Gecko/20100101 Firefox/145.0")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("HTTP request failed: %w", err)
			log.Printf("Attempt %d failed: %v", attempt, err)
			if attempt < maxUploadRetries {
				time.Sleep(2 * time.Second) // 简单退避
			}
			continue
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
			log.Printf("Attempt %d failed with status %d: %s", attempt, resp.StatusCode, string(body))
			if attempt < maxUploadRetries {
				time.Sleep(2 * time.Second)
			}
			continue
		}

		// 尝试解析响应
		var files []UploadFileResponse
		if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
			resp.Body.Close()
			lastErr = fmt.Errorf("decode response: %w", err)
			log.Printf("Attempt %d: invalid JSON: %v", attempt, err)
			if attempt < maxUploadRetries {
				time.Sleep(2 * time.Second)
			}
			continue
		}
		resp.Body.Close()

		if len(files) == 0 {
			lastErr = fmt.Errorf("empty file list in response")
			log.Printf("Attempt %d: empty file list", attempt)
			if attempt < maxUploadRetries {
				time.Sleep(2 * time.Second)
			}
			continue
		}

		// ✅ 成功！跳出循环
		uploadResp = files
		break
	}

	// ❌ 所有重试都失败
	if len(uploadResp) == 0 {
		log.Printf("❌ All %d upload attempts failed for %s: %v", maxUploadRetries, name, lastErr)
		http.Error(w, "Upload failed after "+fmt.Sprint(maxUploadRetries)+" attempts", http.StatusServiceUnavailable)
		return
	}

	result := uploadResp[0]
	cdnPicName := result.Slug + ext

	db := initDB()
	_, err = db.Exec(`
		INSERT INTO files (id, name, filename, delete_url, image_url, uploaded_at, size)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, result.Slug, cdnPicName, name, result.DeleteURL, result.ImageURL, time.Now(), fileSize)
	if err != nil {
		log.Printf("DB save error: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Location", "/"+name)
	w.WriteHeader(http.StatusCreated)
	log.Printf("✅ Uploaded as WebDAV path: /%s", name)
}

func handleDELETE(w http.ResponseWriter, r *http.Request) {
	name := getFileName(r)
	if name == "" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	db := initDB()
	var actualName, deleteURL string

	err := db.QueryRow(`
		SELECT name, delete_url 
		FROM files 
		WHERE name = ? OR filename = ?
		LIMIT 1
	`, name, name).Scan(&actualName, &deleteURL)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Not found", http.StatusNotFound)
		} else {
			http.Error(w, "DB error", http.StatusInternalServerError)
		}
		return
	}

	if deleteURL != "" {
		client := &http.Client{Timeout: 10 * time.Second}
		req, _ := http.NewRequest("GET", deleteURL, nil) // 注意：ooxx 用 GET 删除！
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("⚠️ Remote delete failed (network): %v", err)
		} else {
			resp.Body.Close()
			if resp.StatusCode == 200 || resp.StatusCode == 404 {
				// 200: 成功；404: 已删或不存在（也算成功）
				log.Printf("🗑️ Remote deleted via: %s", deleteURL)
			} else {
				log.Printf("⚠️ Remote delete returned status: %d", resp.StatusCode)
			}
		}
	}
	
	// 删除本地记录
	_, err = db.Exec("DELETE FROM files WHERE name = ? OR filename = ?", actualName)
	if err != nil {
		http.Error(w, "Local delete failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
	log.Printf("✅ Deleted local record for: %s", name)
}

func basicAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "123456" {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		h(w, r)
	}
}

func handleGetURL(w http.ResponseWriter, r *http.Request) {
	// 只允许 GET
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 获取 path 查询参数
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "Missing 'path' query parameter", http.StatusBadRequest)
		return
	}

	// 去掉开头的 /（因为 WebDAV 路径是 /xxx.png，但数据库 name 不带 /）
	name := strings.TrimPrefix(path, "/")
	if name == "" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// 查询数据库：根据原始 filename 查找记录
	db := initDB()
	var storedName string
	err := db.QueryRow(`
		SELECT name FROM files WHERE filename = ?
	`, name).Scan(&storedName)

	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "File not found", http.StatusNotFound)
		} else {
			log.Printf("DB error in get-url: %v", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
		}
		return
	}

	// 返回 JSON：{ "name": "ZGI5M.png" }
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]string{
		"url": storedName,
	})
}

func main() {
	db := initDB()
	defer db.Close()

	mux := http.NewServeMux()

	mux.HandleFunc("/api/get-url", handleGetURL)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET", "HEAD":
			basicAuth(handleGET)(w, r)
		case "PROPFIND":
			basicAuth(handlePROPFIND)(w, r)
		case "PUT":
			basicAuth(handlePUT)(w, r)
		case "DELETE":
			basicAuth(handleDELETE)(w, r)
		default:
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	})

	port := "24876"
	log.Printf("🚀 ooxx.ooo WebDAV Server Started on http://localhost:%s", port)
	log.Printf("   Username: webdav")
	log.Printf("   Password: 123456")
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
