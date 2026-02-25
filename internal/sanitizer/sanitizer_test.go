package sanitizer

import (
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeUTF8_ValidUTF8(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "simple ASCII",
			input: "hello world",
		},
		{
			name:  "valid UTF-8 with special chars",
			input: `[{"data":"+Hooshnic:V1.06,3556.27680,05003.9190,000,260224,052019,000,000,0,3,1,861826074262144"}]`,
		},
		{
			name:  "UTF-8 with unicode",
			input: "Hello 世界 🌍",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := []byte(tt.input)
			result := SanitizeUTF8(input)

			assert.True(t, utf8.Valid(result), "Result should be valid UTF-8")
			assert.Equal(t, input, result, "Valid UTF-8 should remain unchanged")
		})
	}
}

func TestSanitizeUTF8_InvalidUTF8(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		validate func(t *testing.T, result []byte)
	}{
		{
			name:  "invalid UTF-8 sequence",
			input: []byte{0xFF, 0xFE, 0x48, 0x65, 0x6C, 0x6C, 0x6F}, // Invalid start bytes + "Hello"
			validate: func(t *testing.T, result []byte) {
				assert.True(t, utf8.Valid(result), "Result should be valid UTF-8")
				assert.Contains(t, string(result), "Hello", "Valid portion should be preserved")
			},
		},
		{
			name:  "control characters",
			input: []byte{0x00, 0x48, 0x65, 0x0B, 0x6C, 0x6C, 0x0C, 0x6F, 0x7F}, // Control chars + "Hello"
			validate: func(t *testing.T, result []byte) {
				assert.True(t, utf8.Valid(result), "Result should be valid UTF-8")
				assert.NotContains(t, string(result), "\x00", "Null byte should be removed")
				assert.NotContains(t, string(result), "\x0B", "Vertical tab should be removed")
				assert.NotContains(t, string(result), "\x0C", "Form feed should be removed")
				assert.NotContains(t, string(result), "\x7F", "DEL should be removed")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeUTF8(tt.input)
			tt.validate(t, result)
		})
	}
}

func TestSanitizeUTF8_EmptyInput(t *testing.T) {
	result := SanitizeUTF8([]byte{})
	assert.Empty(t, result, "Empty input should return empty output")
}

func TestSanitizeUTF8_NilInput(t *testing.T) {
	result := SanitizeUTF8(nil)
	assert.Empty(t, result, "Nil input should return empty output")
}

func TestRemoveControlChars(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "remove null bytes",
			input:    []byte("Hello\x00World"),
			expected: "HelloWorld",
		},
		{
			name:     "remove multiple control chars",
			input:    []byte("\x00\x01\x02Hello\x0B\x0C\x0EWorld\x1F\x7F"),
			expected: "HelloWorld",
		},
		{
			name:     "preserve valid characters",
			input:    []byte("Hello\tWorld\nTest"), // Tab and newline are not removed
			expected: "Hello\tWorld\nTest",
		},
		{
			name:     "no control chars",
			input:    []byte("Clean data"),
			expected: "Clean data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeControlChars(tt.input)
			assert.Equal(t, tt.expected, string(result))
		})
	}
}

func BenchmarkSanitizeUTF8_Valid(b *testing.B) {
	data := []byte(`[{"data":"+Hooshnic:V1.06,3556.27680,05003.9190,000,260224,052019,000,000,0,3,1,861826074262144"}]`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SanitizeUTF8(data)
	}
}

func BenchmarkSanitizeUTF8_Invalid(b *testing.B) {
	data := []byte{0xFF, 0xFE, 0x48, 0x65, 0x6C, 0x6C, 0x6F}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SanitizeUTF8(data)
	}
}
