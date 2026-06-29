package service

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

// ========================== 文件类型定义 ==========================

// FileKind 支持的文件类型
type FileKind struct {
	ID   uint64
	Name string
}

var SupportedFileKinds = []FileKind{
	{ID: 1, Name: "纯文本 (TXT/MD)"},
	{ID: 2, Name: "PDF 文档"},
	{ID: 3, Name: "图片 (PNG/JPG)"},
}

// ========================== 模型信息定义 ==========================

// ModelInfo 可用模型信息
type ModelInfo struct {
	ID          uint64
	Name        string
	Description string
	MaxTokens   uint32
	FileKinds   []FileKind // 该模型支持的文件类型
}

// GetAvailableModels 返回当前可用的模型列表
func GetAvailableModels() []ModelInfo {
	chatModel := os.Getenv("DASHSCOPE_MODEL_CHAT")
	if chatModel == "" {
		chatModel = os.Getenv("PREMIUM_AI_MODEL_CHAT")
	}
	if chatModel == "" {
		chatModel = "qwen-plus"
	}
	return []ModelInfo{
		{
			ID:          1,
			Name:        chatModel,
			Description: "阿里云百炼 Qwen 大语言模型，用于对话、推理和推文生成",
			MaxTokens:   32768,
			FileKinds:   SupportedFileKinds,
		},
	}
}

// ========================== 文件解析器 ==========================

// ParseFile 根据文件类型解析文件内容为纯文本
// fileKindID: 1=纯文本, 2=PDF, 3=图片
// fileContent: 文件二进制内容
// 返回: 解析后的文本内容
func ParseFile(fileKindID uint64, fileContent []byte) (string, error) {
	if len(fileContent) == 0 {
		return "", fmt.Errorf("file content is empty")
	}

	switch fileKindID {
	case 1:
		return parsePlainText(fileContent)
	case 2:
		return parsePDF(fileContent)
	case 3:
		return parseImage(fileContent)
	default:
		return "", fmt.Errorf("unsupported file kind: %d", fileKindID)
	}
}

// parsePlainText 解析纯文本文件（TXT、Markdown 等）
func parsePlainText(content []byte) (string, error) {
	if !utf8.Valid(content) {
		return "", fmt.Errorf("file content is not valid UTF-8 text")
	}

	text := string(content)

	// 限制最大长度（防止超大文件）
	runes := []rune(text)
	if len(runes) > 50000 {
		text = string(runes[:50000]) + "\n\n[... 内容已截断，原文共 " + fmt.Sprintf("%d", len(runes)) + " 字符 ...]"
	}

	return strings.TrimSpace(text), nil
}

// parsePDF 解析 PDF 文件提取文本
func parsePDF(content []byte) (string, error) {
	reader := bytes.NewReader(content)

	pdfReader, err := pdf.NewReader(reader, int64(len(content)))
	if err != nil {
		return "", fmt.Errorf("failed to open PDF: %w", err)
	}

	totalPages := pdfReader.NumPage()
	if totalPages == 0 {
		return "", fmt.Errorf("PDF has no pages")
	}

	var textBuilder strings.Builder
	textBuilder.WriteString(fmt.Sprintf("[PDF 文档，共 %d 页]\n\n", totalPages))

	for i := 1; i <= totalPages; i++ {
		page := pdfReader.Page(i)
		if page.V.IsNull() {
			continue
		}

		pageText, err := page.GetPlainText(nil)
		if err != nil {
			// 单页解析失败不影响其他页
			textBuilder.WriteString(fmt.Sprintf("--- 第 %d 页（解析失败）---\n\n", i))
			continue
		}

		text := strings.TrimSpace(pageText)
		if text != "" {
			textBuilder.WriteString(fmt.Sprintf("--- 第 %d 页 ---\n", i))
			textBuilder.WriteString(text)
			textBuilder.WriteString("\n\n")
		}
	}

	result := strings.TrimSpace(textBuilder.String())
	if result == fmt.Sprintf("[PDF 文档，共 %d 页]", totalPages) {
		return "", fmt.Errorf("PDF contains no extractable text (may be scanned/image-based)")
	}

	// 截断过长内容
	runes := []rune(result)
	if len(runes) > 50000 {
		result = string(runes[:50000]) + "\n\n[... 内容已截断 ...]"
	}

	return result, nil
}

// parseImage 处理图片文件
// 当前方案：返回提示文本，后续可对接 OCR 或多模态视觉模型
func parseImage(content []byte) (string, error) {
	// 检测图片格式
	format := detectImageFormat(content)
	sizeKB := len(content) / 1024

	return fmt.Sprintf("[图片文件] 格式: %s, 大小: %dKB\n提示: 图片内容已上传，可在后续对话中引用。如需文字提取请使用 OCR 服务。", format, sizeKB), nil
}

// detectImageFormat 通过文件头魔数检测图片格式
func detectImageFormat(content []byte) string {
	if len(content) < 4 {
		return "unknown"
	}
	// PNG: 89 50 4E 47
	if content[0] == 0x89 && content[1] == 0x50 && content[2] == 0x4E && content[3] == 0x47 {
		return "PNG"
	}
	// JPEG: FF D8 FF
	if content[0] == 0xFF && content[1] == 0xD8 && content[2] == 0xFF {
		return "JPEG"
	}
	// GIF: 47 49 46
	if content[0] == 0x47 && content[1] == 0x49 && content[2] == 0x46 {
		return "GIF"
	}
	// WebP: 52 49 46 46 ... 57 45 42 50
	if len(content) > 11 && content[0] == 0x52 && content[1] == 0x49 && content[8] == 0x57 && content[9] == 0x45 {
		return "WebP"
	}
	return "unknown"
}
