package observability

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	ContentSamplePolicyDisabled          = "disabled"
	ContentSamplePolicyRedactedPreviewV1 = "redacted_preview_v1"

	ContentSampleStatusDisabled    = "disabled"
	ContentSampleStatusEmpty       = "empty"
	ContentSampleStatusNotSelected = "not_selected"
	ContentSampleStatusSensitive   = "sensitive"
	ContentSampleStatusOversized   = "oversized"
	ContentSampleStatusCaptured    = "captured"

	defaultContentSampleMaxBytes     = 512
	maximumContentSampleMaxBytes     = 4096
	defaultContentSampleMaxScanBytes = 64 * 1024
)

var (
	sensitiveContentPattern = regexp.MustCompile(`(?i)(api[\s_-]*key|authorization|bearer\s+|password|client[\s_-]*secret|access[\s_-]*token|refresh[\s_-]*token|resume[\s_-]*token|set-cookie|cookie\s*[:=]|\bsk-[a-z0-9_-]{12,}|\beyj[a-z0-9_-]{16,}\.)`)
	personalEmailPattern    = regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)
	personalIDPattern       = regexp.MustCompile(`(?:\b1[3-9]\d{9}\b|\b\d{17}[0-9xX]\b)`)
	credentialURLPattern    = regexp.MustCompile(`(?i)https?://[^\s]+(?:\?|@)[^\s]*`)
)

type ContentSample struct {
	Value  string
	Status string
	Policy string
}

type ContentSampler interface {
	Sample(key, content string) ContentSample
}

type ContentSamplingConfig struct {
	Enabled      bool
	Ratio        float64
	MaxBytes     int
	MaxScanBytes int
}

// SafeContentSampler is deliberately conservative. Sampling is deterministic
// across replicas, bounded, opt-in, and refuses content that resembles secrets
// or direct personal identifiers instead of trying to partially redact it.
type SafeContentSampler struct {
	enabled      bool
	ratio        float64
	maxBytes     int
	maxScanBytes int
}

func NewSafeContentSampler(config ContentSamplingConfig) (*SafeContentSampler, error) {
	if config.Ratio < 0 || config.Ratio > 1 {
		return nil, errors.New("content sample ratio must be between 0 and 1")
	}
	if config.MaxBytes == 0 {
		config.MaxBytes = defaultContentSampleMaxBytes
	}
	if config.MaxBytes < 1 || config.MaxBytes > maximumContentSampleMaxBytes {
		return nil, errors.New("content sample max bytes must be between 1 and 4096")
	}
	if config.MaxScanBytes == 0 {
		config.MaxScanBytes = defaultContentSampleMaxScanBytes
	}
	if config.MaxScanBytes < config.MaxBytes {
		return nil, errors.New("content sample scan limit must not be smaller than preview limit")
	}
	return &SafeContentSampler{
		enabled: config.Enabled, ratio: config.Ratio,
		maxBytes: config.MaxBytes, maxScanBytes: config.MaxScanBytes,
	}, nil
}

func (s *SafeContentSampler) Sample(key, content string) ContentSample {
	if s == nil || !s.enabled {
		return ContentSample{Status: ContentSampleStatusDisabled, Policy: ContentSamplePolicyDisabled}
	}
	result := ContentSample{Status: ContentSampleStatusNotSelected, Policy: ContentSamplePolicyRedactedPreviewV1}
	if content == "" {
		result.Status = ContentSampleStatusEmpty
		return result
	}
	if len(content) > s.maxScanBytes {
		result.Status = ContentSampleStatusOversized
		return result
	}
	if contentIsSensitive(content) {
		result.Status = ContentSampleStatusSensitive
		return result
	}
	if !deterministicallySelected(key, content, s.ratio) {
		return result
	}
	result.Value = truncateUTF8(strings.ToValidUTF8(content, ""), s.maxBytes)
	result.Status = ContentSampleStatusCaptured
	return result
}

func contentIsSensitive(content string) bool {
	return sensitiveContentPattern.MatchString(content) ||
		personalEmailPattern.MatchString(content) ||
		personalIDPattern.MatchString(content) ||
		credentialURLPattern.MatchString(content)
}

func deterministicallySelected(key, content string, ratio float64) bool {
	if ratio <= 0 {
		return false
	}
	if ratio >= 1 {
		return true
	}
	if key == "" {
		key = content
	}
	sum := sha256.Sum256([]byte(key))
	bucket := binary.BigEndian.Uint64(sum[:8])
	return float64(bucket)/float64(math.MaxUint64) < ratio
}

func truncateUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	end := maxBytes
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}
	return value[:end]
}
