package snowflake

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

const machineIDSlotKey = "machine_slots"

var (
	globalNode *customNode
)

const (
	// 定义 Snowflake 的基础参数
	epoch          = int64(1288834974657) // 自定义纪元时间 (例如 2010-11-04)
	workerIDBits   = 10
	sequenceBits   = 12
	maxWorkerID    = -1 ^ (-1 << workerIDBits) // 1023
	sequenceMask   = -1 ^ (-1 << sequenceBits) // 4095
	workerIDShift  = sequenceBits
	timestampShift = sequenceBits + workerIDBits
	// 最大容忍的时钟回拨阈值 (毫秒)
	maxTolerateTimeDifference = 5
)

type customNode struct {
	mu            sync.Mutex
	lastTimestamp int64 // 核心：永远记录上一次成功发号的时间
	workerID      int64
	sequence      int64
}

// Init 手动指定 workerID 初始化 Snowflake 节点（非 Redis 动态抢占模式）
func Init(workerID int64) error {
	var err error
	globalNode, err = NewCustomNode(workerID)
	if err != nil {
		return fmt.Errorf("failed to init custom node: %v", err)
	}
	return nil
}

// MustInit 初始化 Snowflake 节点
func MustInit(redisClient *redis.Client) {

	script := `-- 传入参数：hash的key，当前时间戳，超时阈值(比如30000毫秒)，当前Pod IP
local hash_key = KEYS[1]
local current_time = tonumber(ARGV[1])
local timeout = tonumber(ARGV[2])
local pod_ip = ARGV[3]

-- 遍历 0 到 1023 这 1024 个槽位 (1024 次循环在 Redis 内存中极快，毫无压力)
for i=0, 1023 do
    local slot_id = tostring(i)
    local slot_value = redis.call('HGET', hash_key, slot_id)
    
    if not slot_value then
        -- 情况 1：这个槽位从来没被人用过（是空的）
        -- 立刻抢占！写入当前时间和IP
        redis.call('HSET', hash_key, slot_id, current_time .. ":" .. pod_ip)
        return tonumber(slot_id) -- 成功返回拿到的槽位
    else
        -- 情况 2：槽位被用过，解析出它记录的“最后心跳时间”
        -- (Lua中可以用 string.match 解析字符串)
		local last_time, ip = string.match(slot_value, "^(%d+):(.+)$")
		last_time = tonumber(last_time)
		if (current_time - last_time) > timeout then
            -- 发现这个槽位的主人已经很久没发心跳了（判断为已死）
            -- 并且当前时间大于它的历史时间（跨越了时间墙，安全！）
            -- 抢占！覆盖原有数据
            redis.call('HSET', hash_key, slot_id, current_time .. ":" .. pod_ip)
            return tonumber(slot_id) -- 成功返回回收的槽位
        end
    end
end

-- 情况 3：1024 个槽位全满，且都在活跃状态
return -1
`

	ip := GetIP()
	if ip == "" {
		panic(fmt.Errorf("faild to get ip address"))
	}

	result, err := redisClient.Eval(context.Background(), script, []string{machineIDSlotKey}, time.Now().UnixMilli(), 30000, ip).Result()
	if err != nil {
		panic(fmt.Errorf("faild to init snowflake: %v", err))
	}

	workID := result.(int64)

	globalNode, err = NewCustomNode(workID)

	if err != nil {
		panic(fmt.Errorf("failed to init bwmarrin/snowflake node: %v", err))
	}

	go keepAlive(redisClient, workID, ip)
}

// GenerateID 提供给外部业务代码调用的公开 API
func GenerateID() (uint64, error) {
	if globalNode == nil {
		return 0, fmt.Errorf("发号器未初始化，请先调用 MustInit")
	}

	// 内部偷偷调用那个私有结构体的方法
	return globalNode.generate()
}

func NewCustomNode(workerID int64) (*customNode, error) {
	if workerID < 0 || workerID > maxWorkerID {
		return nil, fmt.Errorf("worker ID must be between 0 and %d", maxWorkerID)
	}
	return &customNode{
		workerID:      workerID,
		lastTimestamp: -1, // 初始化为 -1
		sequence:      0,
	}, nil
}

func (n *customNode) generate() (uint64, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	realNow := time.Now().UnixMilli()
	now := realNow

	if now < n.lastTimestamp {
		offset := n.lastTimestamp - now
		if offset > maxTolerateTimeDifference {
			// 回拨幅度太大（比如倒退了 10 秒），绝对不能借用未来！
			// 直接报错，外层业务捕获此错误后，应立刻去 Redis 申请“动态漂移”换新槽位
			return 0, fmt.Errorf("时钟回拨超过阈值(%dms)，必须申请新槽位", offset)
		}
		// 回拨幅度很小（<=5ms），安全！我们直接“借用”最后一次发号的时间
		now = n.lastTimestamp
	}

	// --- 【防线二：同一毫秒内的并发处理】 ---
	if now == n.lastTimestamp {
		n.sequence = (n.sequence + 1) & sequenceMask

		// 序列号用光了 (4096个全部耗尽)
		if n.sequence == 0 {

			// 此时需要判断：我们目前处于正常时间，还是在处理回拨的阴影期？
			if realNow < n.lastTimestamp {
				// 场景 A：我们是因为时钟回拨，正在“借用时间”。
				// 既然系统时间是错的，那我们就主动创造未来，强行加 1 毫秒！
				now++

				// 极度严谨的二次校验：防止在回拨期间并发实在太大，导致借用超标
				if now-realNow > maxTolerateTimeDifference {
					return 0, fmt.Errorf("回拨期间借用未来时间透支过多，超过容忍阈值")
				}
			} else {
				// 场景 B：时间一切正常，纯粹是并发太高把这 1 毫秒打穿了。
				// 老老实实自旋，等待真实的下一毫秒到来
				now = n.tilNextMillis(n.lastTimestamp)
			}
		}
	} else {
		// 时间正常走到下一毫秒，序列号归零
		n.sequence = 0
	}

	// 4. 更新底单，把最新使用的时间记录下来
	n.lastTimestamp = now

	// 5. 进行位运算，拼接 64 位整型 ID
	id := ((now - epoch) << timestampShift) |
		(n.workerID << workerIDShift) |
		(n.sequence)

	return uint64(id), nil
}

// 内部方法：一直等待，直到操作系统的真实时间严格大于 lastTimestamp
func (n *customNode) tilNextMillis(lastTimestamp int64) int64 {
	timestamp := time.Now().UnixMilli()

	// 核心：必须使用 for 循环死死卡住！绝不能用 if
	for timestamp <= lastTimestamp {
		// 每次循环重新获取当前时间
		timestamp = time.Now().UnixMilli()
	}

	return timestamp
}

func GetIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		panic(err)
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok &&
			!ipnet.IP.IsLoopback() &&
			ipnet.IP.To4() != nil {

			return ipnet.IP.String()
		}
	}
	return ""
}

// keepAlive 后台心跳续期
func keepAlive(redisClient *redis.Client, workerID int64, ip string) {
	// 超时是 30 秒，我们每 10 秒续期一次，确保绝对安全
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	ctx := context.Background()
	slotID := strconv.FormatInt(workerID, 10)

	for range ticker.C {
		val := fmt.Sprintf("%d:%s", time.Now().UnixMilli(), ip)
		// 只要服务没死，就不停地用当前时间覆盖 Redis 里的时间
		err := redisClient.HSet(ctx, machineIDSlotKey, slotID, val).Err()
		if err != nil {
			// 这里最好接入你们的日志库打印 Error，心跳失败不一定马上死，但需要警报
			fmt.Printf("snowflake keepalive warning: %v\n", err)
		}
	}
}
