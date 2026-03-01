package handler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"twitter-clone/internal/gateway/middleware"
)

// UploadHandler 上传处理器
type UploadHandler struct {
	uploadDir string
	baseURL   string
}

// NewUploadHandler 创建上传处理器
func NewUploadHandler(uploadDir, baseURL string) *UploadHandler {
	// 确保上传目录存在
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		panic(fmt.Sprintf("failed to create upload dir: %v", err))
	}

	return &UploadHandler{
		uploadDir: uploadDir,
		baseURL:   baseURL,
	}
}

// UploadFile 上传文件
// POST /api/v1/upload
func (h *UploadHandler) UploadFile(c *gin.Context) {
	// 验证用户登录
	_, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	// 获取文件
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}

	// 验证文件类型 (简单验证后缀)
	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".gif" && ext != ".mp4" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file type"})
		return
	}

	// 验证文件大小 (例如 10MB)
	if file.Size > 10*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file too large (max 10MB)"})
		return
	}

	// 生成文件名 (UUID)
	newFilename := uuid.New().String() + ext

	// 按日期分目录 (避免单目录文件过多)
	dateDir := time.Now().Format("20060102")
	saveDir := filepath.Join(h.uploadDir, dateDir)
	if err := os.MkdirAll(saveDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create directory"})
		return
	}

	savePath := filepath.Join(saveDir, newFilename)

	// 保存文件
	if err := c.SaveUploadedFile(file, savePath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save file"})
		return
	}

	// 生成访问 URL
	// baseURL/uploads/20231010/xxx.jpg
	// 注意 windows 下 filepath.Join 可能用反斜杠，URL 需要正斜杠
	relPath := fmt.Sprintf("uploads/%s/%s", dateDir, newFilename)
	fullURL := fmt.Sprintf("%s/%s", h.baseURL, relPath)

	c.JSON(http.StatusOK, gin.H{
		"url": fullURL,
	})
}
