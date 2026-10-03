package env

import (
	"testing"
	"time"
)

func TestGetInt(t *testing.T) {
	t.Run("fallback_when_unset", func(t *testing.T) {
		if got := GetInt("TEST_GET_INT", 7); got != 7 {
			t.Fatalf("GetInt() = %d, want %d", got, 7)
		}
	})

	t.Run("parsed_value", func(t *testing.T) {
		t.Setenv("TEST_GET_INT", "42")
		if got := GetInt("TEST_GET_INT", 7); got != 42 {
			t.Fatalf("GetInt() = %d, want %d", got, 42)
		}
	})

	t.Run("fallback_when_invalid", func(t *testing.T) {
		t.Setenv("TEST_GET_INT", "x")
		if got := GetInt("TEST_GET_INT", 7); got != 7 {
			t.Fatalf("GetInt() fallback = %d, want %d", got, 7)
		}
	})
}

func TestGetInt64(t *testing.T) {
	t.Run("parsed_value", func(t *testing.T) {
		t.Setenv("TEST_GET_INT64", "168")
		if got := GetInt64("TEST_GET_INT64", 10); got != 168 {
			t.Fatalf("GetInt64() = %d, want %d", got, 168)
		}
	})

	t.Run("fallback_when_invalid", func(t *testing.T) {
		t.Setenv("TEST_GET_INT64", "invalid")
		if got := GetInt64("TEST_GET_INT64", 10); got != 10 {
			t.Fatalf("GetInt64() fallback = %d, want %d", got, 10)
		}
	})
}

func TestGetDuration(t *testing.T) {
	tests := []struct {
		name string
		key  string
		def  time.Duration
		val  string
		set  bool
		want time.Duration
	}{
		{
			name: "unset_uses_default",
			key:  "TEST_GET_DURATION",
			def:  2 * time.Second,
			set:  false,
			want: 2 * time.Second,
		},
		{
			name: "numeric_ms",
			key:  "TEST_GET_DURATION",
			def:  0,
			val:  "1500",
			set:  true,
			want: 1500 * time.Millisecond,
		},
		{
			name: "zero_ms",
			key:  "TEST_GET_DURATION",
			def:  time.Second,
			val:  "0",
			set:  true,
			want: 0,
		},
		{
			name: "with_unit_seconds",
			key:  "TEST_GET_DURATION",
			def:  0,
			val:  "2s",
			set:  true,
			want: 2 * time.Second,
		},
		{
			name: "with_unit_milliseconds",
			key:  "TEST_GET_DURATION",
			def:  0,
			val:  "750ms",
			set:  true,
			want: 750 * time.Millisecond,
		},
		{
			name: "with_spaces_trimmed",
			key:  "TEST_GET_DURATION",
			def:  0,
			val:  "  3s  ",
			set:  true,
			want: 3 * time.Second,
		},
		{
			name: "invalid_falls_back_to_default",
			key:  "TEST_GET_DURATION",
			def:  60 * time.Second,
			val:  "abc",
			set:  true,
			want: 60 * time.Second,
		},
		{
			name: "negative_ms_allowed",
			key:  "TEST_GET_DURATION",
			def:  0,
			val:  "-500",
			set:  true,
			want: -500 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		if tt.set {
			t.Setenv(tt.key, tt.val)
		} else {
			t.Setenv(tt.key, "")
		}
		got := GetDuration(tt.key, tt.def)
		if got != tt.want {
			t.Fatalf("%s: GetDuration(%q) = %v, want %v", tt.name, tt.val, got, tt.want)
		}
	}
}

func TestGetString(t *testing.T) {
	t.Run("fallback_when_unset", func(t *testing.T) {
		if got := GetString("TEST_GET_STRING", "default"); got != "default" {
			t.Fatalf("GetString() = %q, want %q", got, "default")
		}
	})

	t.Run("trims_value", func(t *testing.T) {
		t.Setenv("TEST_GET_STRING", "  chat-session  ")
		if got := GetString("TEST_GET_STRING", "default"); got != "chat-session" {
			t.Fatalf("GetString() = %q, want %q", got, "chat-session")
		}
	})

	t.Run("fallback_when_blank", func(t *testing.T) {
		t.Setenv("TEST_GET_STRING", "   ")
		if got := GetString("TEST_GET_STRING", "default"); got != "default" {
			t.Fatalf("GetString() fallback = %q, want %q", got, "default")
		}
	})
}

func TestGetBool(t *testing.T) {
	t.Run("fallback_when_unset", func(t *testing.T) {
		if got := GetBool("TEST_GET_BOOL", true); !got {
			t.Fatal("GetBool() = false, want true")
		}
	})

	t.Run("parsed_value", func(t *testing.T) {
		t.Setenv("TEST_GET_BOOL", "true")
		if got := GetBool("TEST_GET_BOOL", false); !got {
			t.Fatal("GetBool() = false, want true")
		}
	})

	t.Run("fallback_when_invalid", func(t *testing.T) {
		t.Setenv("TEST_GET_BOOL", "nope")
		if got := GetBool("TEST_GET_BOOL", true); !got {
			t.Fatal("GetBool() fallback = false, want true")
		}
	})
}

func TestGetFloat(t *testing.T) {
	t.Run("fallback_when_unset", func(t *testing.T) {
		if got := GetFloat("TEST_GET_FLOAT", 1.5); got != 1.5 {
			t.Fatalf("GetFloat() = %v, want %v", got, 1.5)
		}
	})

	t.Run("parsed_value", func(t *testing.T) {
		t.Setenv("TEST_GET_FLOAT", "3.1415")
		if got := GetFloat("TEST_GET_FLOAT", 1.5); got != 3.1415 {
			t.Fatalf("GetFloat() = %v, want %v", got, 3.1415)
		}
	})

	t.Run("fallback_when_invalid", func(t *testing.T) {
		t.Setenv("TEST_GET_FLOAT", "abc")
		if got := GetFloat("TEST_GET_FLOAT", 1.5); got != 1.5 {
			t.Fatalf("GetFloat() fallback = %v, want %v", got, 1.5)
		}
	})
}
