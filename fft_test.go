package ncmrecognize

import (
	"math"
	"testing"
)

func TestHammingWindow(t *testing.T) {
	w := windowHamming(2048)
	if math.Abs(float64(w[0])-0.08) > 1e-7 {
		t.Fatalf("w[0]=%g", w[0])
	}
	if math.Abs(float64(w[1024])-1) > 1e-6 {
		t.Fatalf("w[1024]=%g", w[1024])
	}
	for i := 0; i < 2048; i++ {
		if w[i] != w[2047-i] {
			t.Fatalf("window not symmetric at %d", i)
		}
	}
}

func TestFFTImpulse(t *testing.T) {
	f := newKissFFT(2048)
	in := make([]complex64, 2048)
	in[0] = 1
	out := make([]complex64, 2048)
	f.exec(out, in)
	for i, v := range out {
		if math.Abs(float64(real(v))-1) > 1e-5 || math.Abs(float64(imag(v))) > 1e-5 {
			t.Fatalf("bin %d = %v", i, v)
		}
	}
}

func TestFFTSine(t *testing.T) {
	f := newKissFFT(2048)
	const bin = 64
	in := make([]complex64, 2048)
	for i := range in {
		in[i] = complex(float32(math.Cos(2*math.Pi*bin*float64(i)/2048)), 0)
	}
	out := make([]complex64, 2048)
	f.exec(out, in)
	if math.Abs(float64(real(out[bin]))-1024) > 1 {
		t.Fatalf("bin %d = %v, want 1024", bin, out[bin])
	}
	if math.Abs(float64(real(out[2048-bin]))-1024) > 1 {
		t.Fatalf("bin %d = %v, want 1024", 2048-bin, out[2048-bin])
	}
}
