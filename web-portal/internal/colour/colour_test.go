package colour_test

import (
	"testing"
	"testing/quick"

	"github.com/sudo-ivan/snikketx/web-portal/internal/colour"
)

func TestDeterministic(t *testing.T) {
	a := colour.TextToCSS("alice@example.test")
	b := colour.TextToCSS("alice@example.test")
	if a != b || len(a) != 7 || a[0] != '#' {
		t.Fatalf("got %q", a)
	}
}

func TestPropertyFormat(t *testing.T) {
	f := func(s string) bool {
		css := colour.TextToCSS(s)
		if len(css) != 7 || css[0] != '#' {
			return false
		}
		for _, c := range css[1:] {
			switch {
			case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
			default:
				return false
			}
		}
		return true
	}
	if err := quick.Check(f, nil); err != nil {
		t.Fatal(err)
	}
}

func FuzzTextToCSS(f *testing.F) {
	f.Add("alice@example.test")
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) {
		_ = colour.TextToCSS(s)
	})
}

func BenchmarkTextToCSS(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = colour.TextToCSS("alice@example.test")
	}
}
