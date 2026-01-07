package cache

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// encodeFlag Tests
// =============================================================================

func TestEncodeFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		version  int64
		jsonData []byte
		expected string
	}{
		{
			name:     "happy path",
			version:  42,
			jsonData: []byte(`{"enabled":true}`),
			expected: `42|{"enabled":true}`,
		},
		{
			name:     "large version (max int64)",
			version:  9223372036854775807,
			jsonData: []byte(`{"key":"value"}`),
			expected: `9223372036854775807|{"key":"value"}`,
		},
		{
			name:     "empty JSON",
			version:  1,
			jsonData: []byte(""),
			expected: "1|",
		},
		{
			name:     "zero version",
			version:  0,
			jsonData: []byte(`{"test":"data"}`),
			expected: `0|{"test":"data"}`,
		},
		{
			name:     "complex JSON with nested objects",
			version:  123,
			jsonData: []byte(`{"enabled":true,"rules":[{"id":"r1","type":"percentage","value":{"percentage":50}}]}`),
			expected: `123|{"enabled":true,"rules":[{"id":"r1","type":"percentage","value":{"percentage":50}}]}`,
		},
		{
			name:     "JSON with pipe characters",
			version:  5,
			jsonData: []byte(`{"description":"value|with|pipes"}`),
			expected: `5|{"description":"value|with|pipes"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Act
			result := encodeFlag(tt.jsonData, tt.version)

			// Assert
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// decodeFlag Tests
// =============================================================================

func TestDecodeFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		encoded  string
		expected string
	}{
		{
			name:     "happy path",
			encoded:  `42|{"enabled":true}`,
			expected: `{"enabled":true}`,
		},
		{
			name:     "large version (max int64)",
			encoded:  `9223372036854775807|{"key":"value"}`,
			expected: `{"key":"value"}`,
		},
		{
			name:     "empty JSON",
			encoded:  "1|",
			expected: "",
		},
		{
			name:     "corrupted data - no pipe (fallback)",
			encoded:  `{"enabled":true}`,
			expected: `{"enabled":true}`,
		},
		{
			name:     "legacy format without version prefix",
			encoded:  `some.legacy.data.format`,
			expected: `some.legacy.data.format`,
		},
		{
			name:     "complex JSON with nested objects",
			encoded:  `123|{"enabled":true,"rules":[{"id":"r1","type":"percentage","value":{"percentage":50}}]}`,
			expected: `{"enabled":true,"rules":[{"id":"r1","type":"percentage","value":{"percentage":50}}]}`,
		},
		{
			name:     "JSON with pipe characters in value",
			encoded:  `5|{"description":"value|with|pipes"}`,
			expected: `{"description":"value|with|pipes"}`,
		},
		{
			name:     "search optimization - pipe at position 20",
			encoded:  "1234567890123456789|" + strings.Repeat("x", 1000),
			expected: strings.Repeat("x", 1000),
		},
		{
			name:     "pipe after search limit (corrupted)",
			encoded:  strings.Repeat("0", 21) + "|data",
			expected: strings.Repeat("0", 21) + "|data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Act
			result := decodeFlag(tt.encoded)

			// Assert
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// EncodeQueueMessage Tests
// =============================================================================

func TestEncodeQueueMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		flagKey  string
		version  int64
		expected string
	}{
		{
			name:     "happy path",
			flagKey:  "feature-flag-123",
			version:  42,
			expected: "feature-flag-123:42",
		},
		{
			name:     "large version (max int64)",
			flagKey:  "my-flag",
			version:  9223372036854775807,
			expected: "my-flag:9223372036854775807",
		},
		{
			name:     "version zero",
			flagKey:  "test-flag",
			version:  0,
			expected: "test-flag:0",
		},
		{
			name:     "empty flag key",
			flagKey:  "",
			version:  1,
			expected: ":1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Act
			result := EncodeQueueMessage(tt.flagKey, tt.version)

			// Assert
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// DecodeQueueMessage Tests
// =============================================================================

func TestDecodeQueueMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		message         string
		expectedKey     string
		expectedVersion int64
	}{
		{
			name:            "happy path",
			message:         "feature-flag-123:42",
			expectedKey:     "feature-flag-123",
			expectedVersion: 42,
		},
		{
			name:            "large version (max int64)",
			message:         "my-flag:9223372036854775807",
			expectedKey:     "my-flag",
			expectedVersion: 9223372036854775807,
		},
		{
			name:            "version zero",
			message:         "test-flag:0",
			expectedKey:     "test-flag",
			expectedVersion: 0,
		},
		{
			name:            "legacy format without version",
			message:         "feature-flag-old",
			expectedKey:     "feature-flag-old",
			expectedVersion: 0,
		},
		{
			name:            "invalid version format (fallback)",
			message:         "flag:not-a-number",
			expectedKey:     "flag:not-a-number",
			expectedVersion: 0,
		},
		{
			name:            "empty message",
			message:         "",
			expectedKey:     "",
			expectedVersion: 0,
		},
		{
			name:            "only colon",
			message:         ":",
			expectedKey:     ":",
			expectedVersion: 0,
		},
		{
			name:            "empty key with valid version",
			message:         ":123",
			expectedKey:     "",
			expectedVersion: 123,
		},
		{
			name:            "version overflow (fallback)",
			message:         "flag:99999999999999999999999",
			expectedKey:     "flag:99999999999999999999999",
			expectedVersion: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Act
			flagKey, version := DecodeQueueMessage(tt.message)

			// Assert
			assert.Equal(t, tt.expectedKey, flagKey)
			assert.Equal(t, tt.expectedVersion, version)
		})
	}
}

// =============================================================================
// Round-trip Tests (Encode -> Decode)
// =============================================================================

func TestRoundTrip_FlagEncoding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		version  int64
		jsonData []byte
	}{
		{
			name:     "simple JSON",
			version:  1,
			jsonData: []byte(`{"enabled":true}`),
		},
		{
			name:     "complex JSON with nested objects",
			version:  42,
			jsonData: []byte(`{"enabled":true,"rules":[{"id":"r1","type":"percentage"}],"meta":{"owner":"team-a"}}`),
		},
		{
			name:     "max int64 version",
			version:  9223372036854775807,
			jsonData: []byte(`{"test":"data"}`),
		},
		{
			name:     "zero version",
			version:  0,
			jsonData: []byte(`{}`),
		},
		{
			name:     "JSON with pipe characters",
			version:  5,
			jsonData: []byte(`{"desc":"a|b|c"}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Act
			encoded := encodeFlag(tt.jsonData, tt.version)
			decoded := decodeFlag(encoded)

			// Assert
			assert.Equal(t, string(tt.jsonData), decoded, "decoded JSON should match original")
		})
	}
}

func TestRoundTrip_QueueMessageEncoding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		flagKey string
		version int64
	}{
		{
			name:    "simple flag key",
			flagKey: "feature-flag",
			version: 1,
		},
		{
			name:    "max int64 version",
			flagKey: "my-flag",
			version: 9223372036854775807,
		},
		{
			name:    "zero version",
			flagKey: "test",
			version: 0,
		},
		{
			name:    "empty flag key",
			flagKey: "",
			version: 42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Act
			encoded := EncodeQueueMessage(tt.flagKey, tt.version)
			decodedKey, decodedVersion := DecodeQueueMessage(encoded)

			// Assert
			assert.Equal(t, tt.flagKey, decodedKey, "decoded flag key should match original")
			assert.Equal(t, tt.version, decodedVersion, "decoded version should match original")
		})
	}
}

// =============================================================================
// Property-based Tests
// =============================================================================

func TestEncodeDecodeFlag_PropertyAlwaysRecoverable(t *testing.T) {
	t.Parallel()

	// Property: For any valid version and JSON bytes, encoding then decoding
	// should return the original JSON bytes

	testCases := []struct {
		version  int64
		jsonData string
	}{
		{1, `{}`},
		{100, `{"a":"b"}`},
		{9223372036854775807, `{"complex":{"nested":{"data":"here"}}}`},
		{0, `[]`},
		{-1, `{"negative":"version"}`},
		{42, `{"pipe":"a|b","colon":"x:y"}`},
	}

	for _, tc := range testCases {
		encoded := encodeFlag([]byte(tc.jsonData), tc.version)
		decoded := decodeFlag(encoded)

		require.Equal(t, tc.jsonData, decoded,
			"Round-trip failed for version=%d, json=%s", tc.version, tc.jsonData)
	}
}

func TestEncodeDecodeQueueMessage_PropertyAlwaysRecoverable(t *testing.T) {
	t.Parallel()

	// Property: For any valid flag key and version, encoding then decoding
	// should return the original values

	testCases := []struct {
		flagKey string
		version int64
	}{
		{"simple", 1},
		{"with-dashes", 100},
		{"with_underscores", 42},
		{"namespace:feature:flag", 5},
		{"MixedCase123", 9223372036854775807},
		{"", 1}, // Edge case: empty key
		{"flag", 0},
		{"flag", -1},
	}

	for _, tc := range testCases {
		encoded := EncodeQueueMessage(tc.flagKey, tc.version)
		decodedKey, decodedVersion := DecodeQueueMessage(encoded)

		require.Equal(t, tc.flagKey, decodedKey,
			"Flag key mismatch for key=%s, version=%d", tc.flagKey, tc.version)
		require.Equal(t, tc.version, decodedVersion,
			"Version mismatch for key=%s, version=%d", tc.flagKey, tc.version)
	}
}
