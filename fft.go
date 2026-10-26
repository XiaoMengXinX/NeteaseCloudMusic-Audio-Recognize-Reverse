package ncmrecognize

import "math"

// kissFFT is a float32 radix-2/3/4/5 Cooley-Tukey FFT identical to the
// KISS FFT implementation embedded in afp.wasm (forward transform only).
type kissFFT struct {
	n       int
	factors []int // (p, m) pairs, as produced by kiss_fft's kf_factor
	tw      []complex64
}

func newKissFFT(n int) *kissFFT {
	k := &kissFFT{n: n}
	k.factors = k.factor(n)
	k.tw = make([]complex64, n)
	s := 2 * math.Pi / float64(n)
	for i := 0; i < n; i++ {
		p := -s * float64(i)
		k.tw[i] = complex(float32(math.Cos(p)), float32(math.Sin(p)))
	}
	return k
}

// factor reproduces kiss_fft's kf_factor: prefer radix 4, then 2, then odd.
func (k *kissFFT) factor(n int) []int {
	var out []int
	p := 4
	floorSqrt := int(math.Floor(math.Sqrt(float64(n))))
	for {
		for n%p != 0 {
			switch p {
			case 4:
				p = 2
			case 2:
				p = 3
			default:
				p += 2
			}
			if p > floorSqrt {
				p = n
			}
		}
		n /= p
		out = append(out, p, n)
		if n <= 1 {
			return out
		}
	}
}

func f32mul(a, b float32) float32 { return math.Float32frombits(math.Float32bits(a * b)) }
func f32add(a, b float32) float32 { return math.Float32frombits(math.Float32bits(a + b)) }
func f32sub(a, b float32) float32 { return math.Float32frombits(math.Float32bits(a - b)) }

func cmul(a, b complex64) complex64 {
	ar, ai, br, bi := real(a), imag(a), real(b), imag(b)
	rr := f32sub(f32mul(ar, br), f32mul(ai, bi))
	ri := f32add(f32mul(ar, bi), f32mul(ai, br))
	return complex(rr, ri)
}

// exec runs a forward transform of in into out (natural order, out may alias in).
func (k *kissFFT) exec(out, in []complex64) {
	k.work(out, in, 1, 1, k.factors)
}

func (k *kissFFT) work(fout, f []complex64, fstride, inStride int, factors []int) {
	p, m := factors[0], factors[1]
	if m == 1 {
		fi := 0
		for j := 0; j < p; j++ {
			fout[j] = f[fi]
			fi += fstride * inStride
		}
	} else {
		fi := 0
		for j := 0; j < p; j++ {
			k.work(fout[j*m:(j+1)*m], f[fi:], fstride*p, inStride, factors[2:])
			fi += fstride * inStride
		}
	}
	switch p {
	case 2:
		k.bfly2(fout, fstride, m)
	case 4:
		k.bfly4(fout, fstride, m)
	default:
		panic("kissFFT: only radix 2 and 4 are implemented (n must factor as 2^a*4^b)")
	}
}

func (k *kissFFT) bfly2(fout []complex64, fstride, m int) {
	tw := 0
	for i := 0; i < m; i++ {
		t := cmul(fout[i+m], k.tw[tw])
		tw += fstride
		fout[i+m] = fout[i] - t
		fout[i] += t
	}
}

func (k *kissFFT) bfly4(fout []complex64, fstride, m int) {
	tw1, tw2, tw3 := 0, 0, 0
	m2, m3 := 2*m, 3*m
	for i := 0; i < m; i++ {
		s0 := cmul(fout[i+m], k.tw[tw1])
		s1 := cmul(fout[i+m2], k.tw[tw2])
		s2 := cmul(fout[i+m3], k.tw[tw3])
		s5 := fout[i] - s1
		fout[i] += s1
		s3 := s0 + s2
		s4 := s0 - s2
		fout[i+m2] = fout[i] - s3
		tw1 += fstride
		tw2 += fstride * 2
		tw3 += fstride * 3
		fout[i] += s3
		fout[i+m] = complex(real(s5)+imag(s4), imag(s5)-real(s4))
		fout[i+m3] = complex(real(s5)-imag(s4), imag(s5)+real(s4))
	}
}

// windowHamming corresponds to the window table built by afp.wasm.
func windowHamming(n int) []float32 {
	w := make([]float32, n)
	for i := range w {
		w[i] = float32(0.54 - 0.46*math.Cos(2*math.Pi*float64(i)/float64(n-1)))
	}
	return w
}
