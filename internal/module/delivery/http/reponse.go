package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Response 统一响应结构
type Response struct {
	Code    int         `test_data:"code"`           // 业务状态码
	Message string      `test_data:"message"`        // 提示消息
	Data    interface{} `test_data:"data,omitempty"` // 响应数据
}

// SuccessResponse 成功响应
func SuccessResponse(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Code:    0,
		Message: "success",
		Data:    data,
	})
}

// CreatedResponse 创建成功响应
func CreatedResponse(c *gin.Context, data interface{}) {
	c.JSON(http.StatusCreated, Response{
		Code:    0,
		Message: "created",
		Data:    data,
	})
}

// ErrorResponse 错误响应
func ErrorResponse(c *gin.Context, httpStatus int, code int, message string) {
	c.JSON(httpStatus, Response{
		Code:    code,
		Message: message,
	})
}

// BadRequestResponse 400 错误
func BadRequestResponse(c *gin.Context, message string) {
	ErrorResponse(c, http.StatusBadRequest, 40000, message)
}

// UnauthorizedResponse 401 错误
func UnauthorizedResponse(c *gin.Context, message string) {
	ErrorResponse(c, http.StatusUnauthorized, 40100, message)
}

// ForbiddenResponse 403 错误
func ForbiddenResponse(c *gin.Context, message string) {
	ErrorResponse(c, http.StatusForbidden, 40300, message)
}

// NotFoundResponse 404 错误
func NotFoundResponse(c *gin.Context, message string) {
	ErrorResponse(c, http.StatusNotFound, 40400, message)
}

// ConflictResponse 409 错误（资源冲突）
func ConflictResponse(c *gin.Context, message string) {
	ErrorResponse(c, http.StatusConflict, 40900, message)
}

// InternalServerErrorResponse 500 错误
func InternalServerErrorResponse(c *gin.Context, message string) {
	ErrorResponse(c, http.StatusInternalServerError, 50000, message)
}

// ValidationErrorResponse 参数验证错误
func ValidationErrorResponse(c *gin.Context, err error) {
	BadRequestResponse(c, err.Error())
}
