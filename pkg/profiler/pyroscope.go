package profiler

import (
	"log"
	"os"

	"github.com/grafana/pyroscope-go"
)

// Init 自适应启动 Pyroscope 持续 Profiling
func Init(appName string) {
	//注意：该服务应用于docker生产环境，所以不进行godotenv.Load()操作
	serverAddr := os.Getenv("PYROSCOPE_SERVER_ADDRESS")
	if serverAddr == "" {
		log.Printf("ℹ️ [Profiler] PYROSCOPE_SERVER_ADDRESS env is empty, profiling disabled for: %s", appName)
		return
	}

	log.Printf("🔥 [Profiler] Starting Pyroscope Continuous Profiler for: %s, uploading to: %s", appName, serverAddr)
	_, err := pyroscope.Start(pyroscope.Config{
		ApplicationName: appName,
		ServerAddress:   serverAddr,
		Logger:          pyroscope.StandardLogger,
		ProfileTypes: []pyroscope.ProfileType{
			pyroscope.ProfileCPU,
			pyroscope.ProfileAllocObjects,
			pyroscope.ProfileAllocSpace,
			pyroscope.ProfileInuseObjects,
			pyroscope.ProfileInuseSpace,
		},
	})
	if err != nil {
		log.Printf("⚠️ [Profiler] Failed to start Pyroscope profiler: %v", err)
	} else {
		log.Printf("✅ [Profiler] Pyroscope profiler started successfully")
	}
}
