package consumer

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/go-ego/gse"
)

const maxTrendTopicRunes = 16

// TrendsProcessor extracts compact, display-safe hot topics from tweet content.
type TrendsProcessor struct {
	seg        gse.Segmenter
	hashtagReg *regexp.Regexp
	stopwords  map[string]bool
}

func NewTrendsProcessor() (*TrendsProcessor, error) {
	p := &TrendsProcessor{
		hashtagReg: regexp.MustCompile(`#([\p{Han}A-Za-z0-9_][\p{Han}A-Za-z0-9_-]{0,63})`),
		stopwords:  defaultStopwords(),
	}

	if err := p.seg.LoadDictEmbed(); err != nil {
		return nil, err
	}

	return p, nil
}

func (p *TrendsProcessor) ExtractTopics(content string) map[string]int64 {
	topics := make(map[string]int64)
	content = strings.TrimSpace(content)
	if content == "" {
		return topics
	}

	hashtagMatches := p.hashtagReg.FindAllStringSubmatch(content, -1)
	for _, match := range hashtagMatches {
		if len(match) <= 1 {
			continue
		}
		tag := normalizeTrendTopic(match[1])
		if tag != "" && !p.stopwords[strings.ToLower(tag)] {
			topics[strings.ToLower(tag)] = 30
		}
	}

	cleanText := p.hashtagReg.ReplaceAllString(content, " ")
	segs := p.seg.Pos(cleanText)
	for _, seg := range segs {
		word := normalizeTrendTopic(seg.Text)
		if word == "" || utf8.RuneCountInString(word) <= 1 || p.stopwords[strings.ToLower(word)] {
			continue
		}
		if isHighValueEntity(seg.Pos) {
			wordLower := strings.ToLower(word)
			if _, exists := topics[wordLower]; !exists {
				topics[wordLower] = 10
			}
		}
	}

	return topics
}

func normalizeTrendTopic(topic string) string {
	topic = strings.TrimSpace(topic)
	topic = strings.Trim(topic, "# \t\r\n.,，。!！?？:：;；、'\"“”‘’()（）[]【】{}<>《》")
	if topic == "" {
		return ""
	}

	for _, marker := range []string{"话题", "真是", "真的", "网友", "我们", "你们", "他们", "一个", "这次", "这个", "那个"} {
		if idx := strings.Index(topic, marker); idx > 0 {
			topic = topic[:idx]
			break
		}
	}

	topic = strings.TrimSpace(topic)
	if utf8.RuneCountInString(topic) > maxTrendTopicRunes {
		runes := []rune(topic)
		topic = string(runes[:maxTrendTopicRunes])
	}
	return topic
}

func isHighValueEntity(pos string) bool {
	if pos == "n" || pos == "nz" || pos == "x" || pos == "eng" {
		return true
	}
	if strings.HasPrefix(pos, "nr") || strings.HasPrefix(pos, "ns") || strings.HasPrefix(pos, "nt") {
		return true
	}
	return false
}

func defaultStopwords() map[string]bool {
	words := []string{
		"的", "了", "和", "是", "就", "都", "而", "及", "与", "着",
		"我", "你", "他", "我们", "你们", "他们", "自己", "人家", "什么", "怎么",
		"这个", "那个", "这次", "一次", "一个", "一些", "很多", "非常", "特别",
		"今天", "明天", "昨天", "现在", "时候", "可以", "不会", "感觉", "觉得",
		"其实", "确实", "只是", "为了", "因为", "所以", "如果", "但是", "不过",
		"虽然", "不仅", "而且", "也是", "网友", "话题", "推特", "微博", "发推",
		"推文", "回复", "支持", "反对", "同意", "好的", "没问题", "谢谢", "不客气",
	}

	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}
