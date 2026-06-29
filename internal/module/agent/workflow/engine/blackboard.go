package engine

import (
	"sync"
)

// Blackboard 维护工作流生命周期内的全局上下文状态树
type Blackboard struct {
	mu    sync.RWMutex
	state map[string]map[string]interface{} // 结构：NodeID -> { FieldName: Value }
}

// NewBlackboard 初始化一个新的黑板实例
func NewBlackboard() *Blackboard {
	return &Blackboard{
		state: make(map[string]map[string]interface{}),
	}
}

// LoadSnapshot replaces the blackboard state with a defensive copy of a
// persisted checkpoint snapshot.
func (b *Blackboard) LoadSnapshot(snapshot map[string]map[string]interface{}) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.state = make(map[string]map[string]interface{}, len(snapshot))
	for nodeID, fields := range snapshot {
		fieldsCopy := make(map[string]interface{}, len(fields))
		for k, v := range fields {
			fieldsCopy[k] = v
		}
		b.state[nodeID] = fieldsCopy
	}
}

// GetSnapshot 获取当前状态树的深拷贝，供各子协程安全、无锁地访问只读视图
func (b *Blackboard) GetSnapshot() map[string]map[string]interface{} {
	b.mu.RLock()
	defer b.mu.RUnlock()

	snapshot := make(map[string]map[string]interface{})
	for nodeID, fields := range b.state {
		fieldsCopy := make(map[string]interface{})
		for k, v := range fields {
			fieldsCopy[k] = v
		}
		snapshot[nodeID] = fieldsCopy
	}
	return snapshot
}

// ApplyDelta 通过追加写入 (Append-Only) 将节点执行后的输出 Delta 应用到全局状态树上
func (b *Blackboard) ApplyDelta(nodeID string, delta map[string]interface{}) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.state[nodeID]; !ok {
		b.state[nodeID] = make(map[string]interface{})
	}

	for k, v := range delta {
		b.state[nodeID][k] = v
	}
}

// GetValue 读取指定节点的具体输出字段
func (b *Blackboard) GetValue(nodeID, field string) (interface{}, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if fields, ok := b.state[nodeID]; ok {
		if val, exists := fields[field]; exists {
			return val, true
		}
	}
	return nil, false
}
