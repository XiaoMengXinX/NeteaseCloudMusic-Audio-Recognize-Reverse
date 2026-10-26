package ncmrecognize

import (
	"math"
	"sort"
)

// Peak extraction parameters, as recovered from afp.wasm's AFPClient setup.
const (
	specBins   = 1025                  // fftLen/2 + 1
	freqLoBin  = 25                    // freqLB 100 Hz / freqResolution 3.90625 Hz
	freqHiBin  = 1024                  // freqUB 4000 Hz (exclusive upper bin)
	bandBins   = freqHiBin - freqLoBin // 999
	nmsFreqRad = 30
	nmsTimeRad = 8
	filterFreq = 10
	filterTime = 5
)

type peak struct {
	bin, frame int32
	val        float32
}

// extractPeaks runs the exact peak-picking pipeline of afp.wasm on a
// frame-major spectrogram of power values (frames * 1025).
func extractPeaks(spec []float32, frames int) []peak {
	// Transpose the 25..1023 band into a bin-major matrix, then take
	// sqrt -> log -> subtract the global mean (all float32 like the wasm).
	mat := make([]float32, bandBins*frames)
	idx := 0
	for b := 0; b < bandBins; b++ {
		off := (b + freqLoBin)
		for f := 0; f < frames; f++ {
			mat[idx] = float32(math.Sqrt(float64(spec[f*specBins+off])))
			idx++
		}
	}
	const eps = float32(1.119209287026024e-06)
	var sum float64
	for i, v := range mat {
		if v < eps {
			v = eps
		}
		l := logf32(v)
		mat[i] = l
		sum += float64(l)
	}
	mean := float32(sum / float64(len(mat)))
	for i := range mat {
		mat[i] = f32sub(mat[i], mean)
	}

	// 3x3 local maxima.
	var peaks []peak
	for b := 0; b < bandBins; b++ {
		b0, b1 := b-1, b+2
		if b0 < 0 {
			b0 = 0
		}
		if b1 > bandBins {
			b1 = bandBins
		}
		row := b * frames
		for f := 0; f < frames; {
			c := mat[row+f]
			f0, f1 := f-1, f+2
			if f0 < 0 {
				f0 = 0
			}
			if f1 > frames {
				f1 = frames
			}
			max := false
			for bb := b0; bb < b1 && !max; bb++ {
				r := bb * frames
				for ff := f0; ff < f1; ff++ {
					if mat[r+ff] > c {
						max = true
						break
					}
				}
			}
			if !max {
				peaks = append(peaks, peak{int32(b), int32(f), c})
				f += 2 // afp.wasm skips the next frame after a peak
			} else {
				f++
			}
		}
	}

	// Interference filter: reject peaks whose (log) level is not clearly
	// above the mean of a +-10 bin / +-5 frame neighborhood.
	filtered := peaks[:0]
	for _, p := range peaks {
		c := p.val
		if c <= 0 {
			continue
		}
		b0, b1 := int(p.bin)-filterFreq, int(p.bin)+filterFreq+1
		if b0 < 0 {
			b0 = 0
		}
		if b1 > bandBins {
			b1 = bandBins
		}
		f0, f1 := int(p.frame)-filterTime, int(p.frame)+filterTime+1
		if f0 < 0 {
			f0 = 0
		}
		if f1 > frames {
			f1 = frames
		}
		var sum float32
		count := 0
		for bb := b0; bb < b1; bb++ {
			r := bb * frames
			for ff := f0; ff < f1; ff++ {
				sum = f32add(sum, mat[r+ff])
				count++
			}
		}
		m := f32div(sum, float32(count))
		if m > 2 || f32sub(c, m) > 1 {
			filtered = append(filtered, p)
		}
	}
	peaks = filtered

	// Rival-maximum suppression over a wider +-30 bin / +-8 frame window.
	survived := peaks[:0]
	for _, p := range peaks {
		c := p.val
		b0, b1 := int(p.bin)-nmsFreqRad, int(p.bin)+nmsFreqRad+1
		if b0 < 0 {
			b0 = 0
		}
		if b1 > bandBins {
			b1 = bandBins
		}
		f0, f1 := int(p.frame)-nmsTimeRad, int(p.frame)+nmsTimeRad+1
		if f0 < 0 {
			f0 = 0
		}
		if f1 > frames {
			f1 = frames
		}
		shadowed := false
		for bb := b0; bb < b1 && !shadowed; bb++ {
			r := bb * frames
			for ff := f0; ff < f1; ff++ {
				if mat[r+ff] > c {
					shadowed = true
					break
				}
			}
		}
		if !shadowed {
			survived = append(survived, peak{int32(int(p.bin) + freqLoBin), p.frame, c})
		}
	}
	sort.SliceStable(survived, func(i, j int) bool {
		if survived[i].frame != survived[j].frame {
			return survived[i].frame < survived[j].frame
		}
		return survived[i].bin < survived[j].bin
	})
	return survived
}
