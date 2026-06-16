package handler

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"twitter-clone/internal/gateway/middleware"
)

// UploadHandler 上传处理器
type UploadHandler struct {
	minioClient *minio.Client
	bucketName  string
	publicURL   string
}

// NewUploadHandler 创建上传处理器
func NewUploadHandler(endpoint, accessKey, secretKey, bucketName, publicURL string) *UploadHandler {
	// 初始化 MinIO 客户端
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false, // 本地开发用 HTTP，如果是生产环境可以通过端口或参数判断
	})
	if err != nil {
		panic(fmt.Sprintf("failed to initialize minio client: %v", err))
	}

	// 确保存储桶存在
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exists, err := minioClient.BucketExists(ctx, bucketName)
	if err != nil {
		panic(fmt.Sprintf("failed to check if bucket exists: %v", err))
	}
	if !exists {
		err = minioClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
		if err != nil {
			panic(fmt.Sprintf("failed to create bucket %s: %v", bucketName, err))
		}
	}

	// 设置公共只读 Policy
	policy := fmt.Sprintf(`{
		"Version": "2012-10-17",
		"Statement": [
			{
				"Effect": "Allow",
				"Principal": {
					"AWS": ["*"]
				},
				"Action": [
					"s3:GetObject"
				],
				"Resource": [
					"arn:aws:s3:::%s/*"
				]
			}
		]
	}`, bucketName)

	err = minioClient.SetBucketPolicy(ctx, bucketName, policy)
	if err != nil {
		panic(fmt.Sprintf("failed to set bucket policy: %v", err))
	}

	return &UploadHandler{
		minioClient: minioClient,
		bucketName:  bucketName,
		publicURL:   publicURL,
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
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}

	// 验证文件类型 (简单验证后缀)
	ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".gif" && ext != ".mp4" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file type"})
		return
	}

	// 验证文件大小 (例如 10MB)
	if fileHeader.Size > 10*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file too large (max 10MB)"})
		return
	}

	// 打开文件流
	srcFile, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to open file"})
		return
	}
	defer srcFile.Close()

	// 生成文件名 (UUID)
	newFilename := uuid.New().String() + ext
	dateDir := time.Now().Format("20060102")
	objectName := fmt.Sprintf("%s/%s", dateDir, newFilename)

	// 根据后缀推断 Content-Type
	contentType := "application/octet-stream"
	switch ext {
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".png":
		contentType = "image/png"
	case ".gif":
		contentType = "image/gif"
	case ".mp4":
		contentType = "video/mp4"
	}

	// 上传文件至 MinIO
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = h.minioClient.PutObject(ctx, h.bucketName, objectName, srcFile, fileHeader.Size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to upload file to object storage: %v", err)})
		return
	}

	// 生成访问 URL
	fullURL := fmt.Sprintf("%s/%s/%s", strings.TrimSuffix(h.publicURL, "/"), h.bucketName, objectName)

	c.JSON(http.StatusOK, gin.H{
		"url": fullURL,
	})
}
