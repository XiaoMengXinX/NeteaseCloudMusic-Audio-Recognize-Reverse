package ncmrecognize

import "math"

// logf32 reproduces the float32 natural-log implementation compiled into
// afp.wasm (musl logf). Explicit rounding barriers keep the Go compiler from
// fusing multiply-adds, matching the wasm's non-fused arithmetic.
func logf32(x float32) float32 {
	const (
		ln2Hi = float32(0.6931381225585938)
		ln2Lo = float32(9.05800061445916e-06)
		lg1   = float32(0.6666666269302368)
		lg2   = float32(0.40000972151756287)
		lg3   = float32(0.2849878668785095)
		lg4   = float32(0.24279078841209412)
	)
	ix := math.Float32bits(x)
	k := int32(-127)
	switch {
	case ix < 0x00800000 || int32(ix) < 0:
		if ix<<1 == 0 {
			return float32(math.Inf(-1))
		}
		if int32(ix) < 0 {
			return float32(math.NaN())
		}
		// subnormal: scale up by 2^25
		k = -152
		x = f32mul(x, 33554432)
		ix = math.Float32bits(x)
	case ix > 0x7f7fffff:
		return x
	case ix == 0x3f800000:
		return 0
	}
	ix += 4913933
	dk := float32(k + int32(ix>>23))
	ix = (ix & 0x007fffff) + 0x3f3504f3
	f := f32sub(math.Float32frombits(ix), 1)
	s := f32div(f, f32add(f, 2))
	z := f32mul(s, s)
	w := f32mul(z, z)
	t1 := f32mul(w, f32add(f32mul(w, lg4), lg2))
	t2 := f32mul(z, f32add(f32mul(w, lg3), lg1))
	r := f32add(t2, t1)
	hfsq := f32mul(f, f32mul(f, 0.5))
	inner := f32mul(s, f32add(hfsq, r))
	inner = f32add(f32mul(dk, ln2Lo), inner)
	inner = f32sub(inner, hfsq)
	inner = f32add(inner, f)
	return f32add(f32mul(dk, ln2Hi), inner)
}

func f32div(a, b float32) float32 { return math.Float32frombits(math.Float32bits(a / b)) }
