package profiler

import (
	"os"
	"testing"
)

func TestInit_Disabled(t *testing.T) {
	// 确保没有环境变量时不会 crash 且正常返回
	os.Unsetenv("PYROSCOPE_SERVER_ADDRESS")
	Init("test-app-disabled")
}

func TestInit_InvalidAddress(t *testing.T) {
	// 传入非法地址，应该能够优雅捕获错误而不崩溃
	os.Setenv("PYROSCOPE_SERVER_ADDRESS", "http://invalid-address-format")
	defer os.Unsetenv("PYROSCOPE_SERVER_ADDRESS")

	Init("test-app-invalid")
}
