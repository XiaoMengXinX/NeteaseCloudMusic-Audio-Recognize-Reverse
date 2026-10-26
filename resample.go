package ncmrecognize

// resampleAt resamples n samples starting at offsetSec to FingerprintRate.
//
// Samples past the end of pcm are left as zero, which is how adaptive padding
// works. Callers that require a complete window must check the bounds first.
//
// The float32 and float64 barriers are required for deterministic output:
// without them Go fuses a*b+c into an FMA on arm64 (and on amd64 with
// GOAMD64>=v3), so the same 44.1 kHz input would produce a different
// fingerprint on different machines.
func resampleAt(pcm []float32, sampleRate int, offsetSec float64, n int) []float32 {
	if n <= 0 {
		return nil
	}
	out := make([]float32, n)
	if sampleRate <= 0 {
		return out
	}
	start := f64mul(offsetSec, float64(sampleRate))
	step := float64(sampleRate) / FingerprintRate
	for i := 0; i < n; i++ {
		pos := f64add(start, f64mul(float64(i), step))
		k := int(pos)
		if k < 0 || k >= len(pcm) {
			break
		}
		frac := float32(f64sub(pos, float64(k)))
		if frac == 0 || k+1 >= len(pcm) {
			out[i] = pcm[k]
		} else {
			out[i] = f32add(f32mul(pcm[k], 1-frac), f32mul(pcm[k+1], frac))
		}
	}
	return out
}
