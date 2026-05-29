package service

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// HotspotInfo 慢热点函数定义
type HotspotInfo struct {
	FunctionName string  // 函数堆栈最深处的名字
	SampleValue  int     // 采样次数
	Percentage   float64 // 占比
}

// ProfilingAnalyzer 负责拉取和解析 Pyroscope profiling 堆栈
type ProfilingAnalyzer struct {
	pyroscopeAddr string
}

func NewProfilingAnalyzer() *ProfilingAnalyzer {
	addr := os.Getenv("PYROSCOPE_SERVER_ADDRESS")
	if addr == "" {
		// 默认 Minikube 内部域名
		addr = "http://twitter-clone-pyroscope:4040"
	}
	return &ProfilingAnalyzer{
		pyroscopeAddr: addr,
	}
}

// GetFlamegraphSummary 拉取指定服务的 CPU 剖析简报，只返回耗时最长的 Top 5 业务热点
func (a *ProfilingAnalyzer) GetFlamegraphSummary(ctx context.Context, appName string) (string, error) {
	// query 参数为 appName.cpu，表示拉取 CPU 样本
	query := fmt.Sprintf("%s.cpu", appName)
	// 使用 collapsed 格式，平铺每一行堆栈，解析成本最低
	url := fmt.Sprintf("%s/render?query=%s&format=collapsed&from=now-2m", a.pyroscopeAddr, query)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create http request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute HTTP GET to Pyroscope: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("pyroscope returned status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return a.parseCollapsedData(resp.Body)
}

func (a *ProfilingAnalyzer) parseCollapsedData(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	
	// 汇总每个最终执行函数（叶子节点）的采样计数
	counts := make(map[string]int)
	totalSamples := 0

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// 格式：stack1;stack2;leaf_func value
		lastSpace := strings.LastIndex(line, " ")
		if lastSpace == -1 {
			continue
		}

		stackStr := line[:lastSpace]
		valStr := line[lastSpace+1:]

		val, err := strconv.Atoi(valStr)
		if err != nil {
			continue
		}

		totalSamples += val

		// 获取叶子函数或包含关键业务路径 "twitter-clone" 的最后一级函数
		stacks := strings.Split(stackStr, ";")
		if len(stacks) == 0 {
			continue
		}

		// 找寻最深处的业务函数作为特征，否则取叶子节点
		targetFunc := stacks[len(stacks)-1]
		for i := len(stacks) - 1; i >= 0; i-- {
			if strings.Contains(stacks[i], "twitter-clone") {
				targetFunc = stacks[i]
				break
			}
		}

		counts[targetFunc] += val
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scanner error while reading collapsed profile: %w", err)
	}

	if totalSamples == 0 {
		return "No active CPU profiling samples recorded in the last 2 minutes.", nil
	}

	// 转化为 slice 排序
	var hotspots []HotspotInfo
	for fn, val := range counts {
		hotspots = append(hotspots, HotspotInfo{
			FunctionName: fn,
			SampleValue:  val,
			Percentage:   (float64(val) / float64(totalSamples)) * 100,
		})
	}

	sort.Slice(hotspots, func(i, j int) bool {
		return hotspots[i].SampleValue > hotspots[j].SampleValue
	})

	// 截取前 5 并拼接为简报
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("=== Continuous Profiling Summary (Total Samples: %d) ===\n", totalSamples))
	limit := 5
	if len(hotspots) < limit {
		limit = len(hotspots)
	}

	for i := 0; i < limit; i++ {
		sb.WriteString(fmt.Sprintf("%d. %s [%.2f%%] (Samples: %d)\n",
			i+1, hotspots[i].FunctionName, hotspots[i].Percentage, hotspots[i].SampleValue))
	}

	return sb.String(), nil
}
