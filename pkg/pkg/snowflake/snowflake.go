package snowflake

import (
	"fmt"
	"github.com/bwmarrin/snowflake"
	"sync"
)

var (
	node *snowflake.Node
	once sync.Once
)

// Init 初始化 Snowflake 节点
// nodeID: 节点ID (0-1023)，多机部署时每台机器使用不同的 nodeID
func Init(nodeID int64) error {
	var err error
	once.Do(func() {
		node, err = snowflake.NewNode(nodeID)
	})
	return err
}

// MustInit 初始化 Snowflake 节点（panic 版本）
func MustInit(nodeID int64) {
	if err := Init(nodeID); err != nil {
		panic(fmt.Sprintf("faild to init snowflake: %v", err))
	}
}

// GenerateID 生成唯一 ID
func GenerateID() uint64 {
	if node == nil {
		panic("snowflake not initialized, call Init() first")
	}
	return uint64(node.Generate().Int64())
}

// GetNode 获取 Snowflake 节点（用于测试）
func GetNode() *snowflake.Node {
	return node
}
