package qdrant

import (
	"regexp"
	"testing"
)

func TestConvertSnowflakeToQdrantID(t *testing.T) {
	tests := []struct {
		name        string
		snowflakeID uint64
		expected    string
	}{
		{
			name:        "Min Snowflake ID",
			snowflakeID: 0,
			expected:    "00000000-0000-0000-0000-000000000000",
		},
		{
			name:        "Normal Snowflake ID",
			snowflakeID: 2024791560905822208, // 0x1c19813264001000 -> 0x1c19 0x813264001000
			expected:    "00000000-0000-0000-1c19-813264001000",
		},
		{
			name:        "Max Snowflake ID",
			snowflakeID: 18446744073709551615, // 0xffffffffffffffff
			expected:    "00000000-0000-0000-ffff-ffffffffffff",
		},
	}

	// UUID 格式正则：8-4-4-4-12 hex characters
	uuidRegex := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertSnowflakeToQdrantID(tt.snowflakeID)
			if got != tt.expected {
				t.Errorf("ConvertSnowflakeToQdrantID() = %v, expected %v", got, tt.expected)
			}
			if !uuidRegex.MatchString(got) {
				t.Errorf("ConvertSnowflakeToQdrantID() output %v is not a valid UUID format", got)
			}
		})
	}
}
