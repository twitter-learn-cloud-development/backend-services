package consumer

import (
	"regexp"
	"strings"
	"github.com/go-ego/gse"
)

// TrendsProcessor 大厂级趋势话题挖掘与权重算分处理器
type TrendsProcessor struct {
	seg        gse.Segmenter
	hashtagReg *regexp.Regexp
	stopwords  map[string]bool
}

// NewTrendsProcessor 创建并初始化趋势话题处理器
func NewTrendsProcessor() (*TrendsProcessor, error) {
	p := &TrendsProcessor{
		hashtagReg: regexp.MustCompile(`#([^#\s]+)`),
		stopwords:  defaultStopwords(),
	}

	// 使用 gse 嵌入式字典，避免部署在容器中时找不到字典文件，极其友好
	if err := p.seg.LoadDictEmbed(); err != nil {
		return nil, err
	}

	return p, nil
}

// ExtractTopics 从推文内容中自动挖掘 Hashtags 与高价值命名实体 (NER)，并计算权重分值
func (p *TrendsProcessor) ExtractTopics(content string) map[string]int64 {
	topics := make(map[string]int64)
	if content == "" {
		return topics
	}

	// 1. 优先提取用户手动打标的 #Hashtag (UGC 显式主题，权重最高)
	hashtagMatches := p.hashtagReg.FindAllStringSubmatch(content, -1)
	for _, match := range hashtagMatches {
		if len(match) > 1 {
			tag := strings.TrimSpace(match[1])
			if tag != "" {
				// 转为小写归一化
				tagLower := strings.ToLower(tag)
				topics[tagLower] = 30 // UGC Hashtag 基础分 30 分
			}
		}
	}

	// 2. 利用 gse 分词及词性标注进行命名实体识别 (NER) 提取热词
	// 剔除已匹配为 Hashtag 的内容，避免重复计分
	cleanText := p.hashtagReg.ReplaceAllString(content, "")
	segs := p.seg.Pos(cleanText)

	for _, seg := range segs {
		word := strings.TrimSpace(seg.Text)
		pos := seg.Pos

		// 字符长度必须大于 1 字节且不能是停用词
		if len(word) <= 1 || p.stopwords[strings.ToLower(word)] {
			continue
		}

		// 仅保留高价值命名实体词性：
		// n: 名词, nz: 专有名词, x: 英文, nr: 人名, ns: 地名, nt: 机构团体
		if isHighValueEntity(pos) {
			wordLower := strings.ToLower(word)
			// 如果该词尚未被 Hashtag 覆盖，则赋予命名实体基础分
			if _, exists := topics[wordLower]; !exists {
				topics[wordLower] = 10 // 智能提取的实体词基础分 10 分
			}
		}
	}

	return topics
}

// isHighValueEntity 判断词性是否为高价值命名实体
func isHighValueEntity(pos string) bool {
	// n (名词), nz (其他专名), x (非汉字串/英文)
	// nr/nr1/nrf (人名), ns/nsf (地名), nt/ntc/nto (机构团体)
	if pos == "n" || pos == "nz" || pos == "x" {
		return true
	}
	if strings.HasPrefix(pos, "nr") || strings.HasPrefix(pos, "ns") || strings.HasPrefix(pos, "nt") {
		return true
	}
	return false
}

// defaultStopwords 返回常用的中文停用词列表，防范无意义常用词污染趋势榜
func defaultStopwords() map[string]bool {
	words := []string{
		"的", "了", "和", "是", "就", "都", "而", "及", "与", "着",
		"我", "你", "他", "我们", "你们", "他们", "自己", "人家", "什么", "怎么",
		"这个", "那个", "今天", "明天", "昨天", "现在", "时候", "可以", "不会", "感觉",
		"觉得", "哈哈", "呵呵", "哈哈哈哈", "帖子", "推特", "微博", "发送", "回复", "楼主",
		"楼主", "楼上", "楼下", "支持", "反对", "同意", "好的", "没问题", "谢谢", "不客气",
		"因为", "所以", "如果", "但是", "然而", "不过", "虽然", "不仅", "而且", "也是",
		"一个", "一些", "很多", "非常", "特别", "真的", "确实", "其实", "只是", "为了",
	}

	m := make(map[string]bool)
	for _, w := range words {
		m[w] = true
	}
	return m
}
