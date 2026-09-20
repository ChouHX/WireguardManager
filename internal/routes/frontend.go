package routes

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// assetCacheControl 带内容哈希的构建产物可长期缓存
const assetCacheControl = "public, max-age=31536000, immutable"

// MountFrontend 让后端直接托管前端构建产物，用于单进程/单容器部署模式。
//
// root 指向前端构建输出目录（Vite 的 dist）。挂载后：
//   - /assets/** 走带哈希的静态资源，并附带长缓存头；
//   - 其它未匹配路径回退到 index.html，支持前端路由刷新；
//   - /api/** 未匹配时仍返回 JSON 404，不会把接口错误伪装成页面。
func MountFrontend(r *gin.Engine, root string) error {
	indexPath := filepath.Join(root, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		return fmt.Errorf("index.html not found under %s: %w", root, err)
	}

	// 静态资源长缓存：仅对带哈希的 /assets 目录生效
	r.Use(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/assets/") {
			c.Header("Cache-Control", assetCacheControl)
		}
		c.Next()
	})

	if assetsDir := filepath.Join(root, "assets"); dirExists(assetsDir) {
		r.Static("/assets", assetsDir)
	}
	if logoPath := filepath.Join(root, "logo.svg"); fileExists(logoPath) {
		r.StaticFile("/logo.svg", logoPath)
	}

	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		// 接口与探针路径不做页面回退，避免错误响应用 HTML 掩盖
		if strings.HasPrefix(path, "/api/") || path == "/health" || path == "/ready" {
			c.JSON(http.StatusNotFound, gin.H{
				"success":   false,
				"message":   "Endpoint not found",
				"timestamp": time.Now().Unix(),
			})
			return
		}

		c.File(indexPath)
	})

	return nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
