package ui

import (
	"reflect"
	"testing"
)

func TestWrapText(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		width    int
		expected []string
	}{
		{
			name:     "short string",
			input:    "hello world",
			width:    20,
			expected: []string{"hello world"},
		},
		{
			name:     "exact width",
			input:    "1234567890",
			width:    10,
			expected: []string{"1234567890"},
		},
		{
			name:     "simple wrap",
			input:    "hello world",
			width:    8,
			expected: []string{"hello", "  world"},
		},
		{
			name:     "wrap multiple lines",
			input:    "this is a very long string that should wrap multiple times",
			width:    10,
			expected: []string{"this is a", "  very", "  long", "  string", "  that", "  should", "  wrap", "  multiple", "  times"},
		},
		{
			name:     "long word exceeds width",
			input:    "supercalifragilisticexpialidocious",
			width:    10,
			expected: []string{"supercalif", "  ragilist", "  icexpial", "  idocious"},
		},
		{
			name:     "width too small for indentation",
			input:    "hello world",
			width:    2,
			expected: []string{"he", "ll", "o", "wo", "rl", "d"},
		},
		{
			name:     "empty string",
			input:    "",
			width:    10,
			expected: []string{""},
		},
		{
			name:     "trailing spaces",
			input:    "hello    ",
			width:    5,
			expected: []string{"hello"},
		},
		{
			name:     "multiple spaces",
			input:    "hello     world",
			width:    8,
			expected: []string{"hello", "  world"},
		},
		{
			name:     "emoji wrap",
			input:    "hello 🇺🇸 world",
			width:    8,
			expected: []string{"hello", "  🇺🇸", "  world"},
		},
		{
			name:     "long emoji sequence",
			input:    "🇺🇸🇪🇺🇯🇵",
			width:    4,
			expected: []string{"🇺🇸🇪🇺", "  🇯🇵"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrapText(tt.input, tt.width)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("wrapText() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		input    string
		limit    int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hell…"},
		{"abc", 2, "a…"},
		{"abc", 1, "…"},
		{"", 5, ""},
		{"🇺🇸🇪🇺🇯🇵", 4, "🇺🇸…"},
		{"🇺🇸", 1, "…"},
		{"🇺🇸", 2, "🇺🇸"},
	}

	for _, tt := range tests {
		got := truncateString(tt.input, tt.limit)
		if got != tt.expected {
			t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.limit, got, tt.expected)
		}
	}
}

func TestIsAlphanumeric(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"abcABC123", true},
		{"abc-123", false},
		{"abc 123", false},
		{"", true},
		{"_", false},
		{"!@#", false},
		{"1234567890", true},
		{"UserName", true},
	}

	for _, tt := range tests {
		got := IsAlphanumeric(tt.input)
		if got != tt.expected {
			t.Errorf("IsAlphanumeric(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}
