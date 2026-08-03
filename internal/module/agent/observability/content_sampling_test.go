package observability

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestSafeContentSamplerIsDisabledByDefault(t *testing.T) {
	sampler, err := NewSafeContentSampler(ContentSamplingConfig{})
	require.NoError(t, err)

	result := sampler.Sample("run:step", "plain content")
	require.Equal(t, ContentSampleStatusDisabled, result.Status)
	require.Equal(t, ContentSamplePolicyDisabled, result.Policy)
	require.Empty(t, result.Value)
}

func TestSafeContentSamplerCapturesBoundedUTF8Preview(t *testing.T) {
	sampler, err := NewSafeContentSampler(ContentSamplingConfig{Enabled: true, Ratio: 1, MaxBytes: 7})
	require.NoError(t, err)

	first := sampler.Sample("stable-key", "你好世界")
	second := sampler.Sample("stable-key", "你好世界")
	require.Equal(t, ContentSampleStatusCaptured, first.Status)
	require.Equal(t, ContentSamplePolicyRedactedPreviewV1, first.Policy)
	require.Equal(t, first, second)
	require.LessOrEqual(t, len(first.Value), 7)
	require.True(t, utf8.ValidString(first.Value))
}

func TestSafeContentSamplerRefusesSensitiveAndOversizedContent(t *testing.T) {
	sampler, err := NewSafeContentSampler(ContentSamplingConfig{
		Enabled: true, Ratio: 1, MaxBytes: 32, MaxScanBytes: 64,
	})
	require.NoError(t, err)

	for _, content := range []string{
		"api_key=must-not-leak", "Authorization: Bearer secret-token",
		"contact user@example.com", "https://example.com/path?token=value",
		"call me at 13800138000", "identity 11010519491231002X",
	} {
		result := sampler.Sample("key", content)
		require.Equal(t, ContentSampleStatusSensitive, result.Status)
		require.Empty(t, result.Value)
	}
	overflow := sampler.Sample("key", strings.Repeat("x", 65))
	require.Equal(t, ContentSampleStatusOversized, overflow.Status)
	require.Empty(t, overflow.Value)
}

func TestSafeContentSamplerValidatesConfiguration(t *testing.T) {
	_, err := NewSafeContentSampler(ContentSamplingConfig{Ratio: 1.1})
	require.Error(t, err)
	_, err = NewSafeContentSampler(ContentSamplingConfig{MaxBytes: 4097})
	require.Error(t, err)
	_, err = NewSafeContentSampler(ContentSamplingConfig{MaxBytes: 512, MaxScanBytes: 128})
	require.Error(t, err)
}
