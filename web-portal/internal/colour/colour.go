package colour

import (
	"crypto/sha1"
	"fmt"
	"math"
	"sync"
)

const (
	sat = 0.55
	lit = 0.55
)

var (
	mu    sync.Mutex
	cache = map[string]string{}
)

func TextToCSS(text string) string {
	mu.Lock()
	if c, ok := cache[text]; ok {
		mu.Unlock()
		return c
	}
	mu.Unlock()

	sum := sha1.Sum([]byte(text))
	hue := float64(uint16(sum[0])|uint16(sum[1])<<8) / 65535.0
	r, g, b := hslToRGB(hue, sat, lit)
	css := fmt.Sprintf("#%02x%02x%02x", round255(r), round255(g), round255(b))

	mu.Lock()
	if len(cache) > 128 {
		cache = map[string]string{}
	}
	cache[text] = css
	mu.Unlock()
	return css
}

func round255(v float64) int {
	n := int(math.Round(v * 255))
	if n < 0 {
		return 0
	}
	if n > 255 {
		return 255
	}
	return n
}

func hslToRGB(h, s, l float64) (float64, float64, float64) {
	if s == 0 {
		return l, l, l
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	return hue2rgb(p, q, h+1.0/3.0), hue2rgb(p, q, h), hue2rgb(p, q, h-1.0/3.0)
}

func hue2rgb(p, q, t float64) float64 {
	if t < 0 {
		t++
	}
	if t > 1 {
		t--
	}
	switch {
	case t < 1.0/6.0:
		return p + (q-p)*6*t
	case t < 0.5:
		return q
	case t < 2.0/3.0:
		return p + (q-p)*(2.0/3.0-t)*6
	default:
		return p
	}
}
