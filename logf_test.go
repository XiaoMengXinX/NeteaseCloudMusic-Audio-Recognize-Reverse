package ncmrecognize

import (
	"math"
	"testing"
)

// logf32 is the float32 natural log used by the fingerprint pipeline. It is
// checked against Go's float64 math.Log with a float32-sized tolerance.
func TestLogf32(t *testing.T) {
	for _, x := range []float32{1, 2, 0.5, 10, 0.001, 1e-6, 1e6, 12345.678} {
		got := logf32(x)
		want := float32(math.Log(float64(x)))
		tol := float32(1e-5) * float32(math.Max(1, math.Abs(float64(want))))
		if diff := float32(math.Abs(float64(got - want))); diff > tol {
			t.Errorf("logf32(%g) = %g, want ~%g (diff %g)", x, got, want, diff)
		}
	}
	if x := logf32(1); x != 0 {
		t.Errorf("logf32(1) = %g, want 0", x)
	}
	if x := logf32(0); !math.IsInf(float64(x), -1) {
		t.Errorf("logf32(0) = %g, want -Inf", x)
	}
	if x := logf32(-1); !math.IsNaN(float64(x)) {
		t.Errorf("logf32(-1) = %g, want NaN", x)
	}
}
