//go:build oracle

package ncmrecognize

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"testing"
)

// wasmPlaintext returns the decompressed wasm fingerprint plaintext for pcm.
func wasmPlaintext(t *testing.T, pcm []float32) []byte {
	t.Helper()
	e, err := newWasmEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := e.ExtractQueryFP(context.Background(), pcm)
	if err != nil {
		t.Fatal(err)
	}
	pt, err := decryptFingerprint(raw)
	if err != nil {
		t.Fatal(err)
	}
	return pt
}

func TestNativePlaintextMatchesWasm(t *testing.T) {
	const n = 48000
	cases := map[string][]float32{}
	sine := func(freq, amp float64) []float32 {
		p := make([]float32, n)
		for i := range p {
			p[i] = float32(amp * math.Sin(2*math.Pi*freq*float64(i)/8000))
		}
		return p
	}
	cases["sine100"] = sine(100, 1)
	cases["sine440"] = sine(440, 1)
	cases["sine1000"] = sine(1000, 1)
	cases["sine3800"] = sine(3800, 1)
	seq := make([]float32, n)
	for i := range seq {
		f := 440.0
		if i >= 8000 {
			f = 880
		}
		if i >= 16000 {
			f = 1760
		}
		if i >= 24000 {
			f = 440
		}
		if i >= 32000 {
			f = 880
		}
		if i >= 40000 {
			f = 1760
		}
		seq[i] = float32(math.Sin(2 * math.Pi * f * float64(i) / 8000))
	}
	cases["seq"] = seq
	// random noise and a music-like mix give broader spectral coverage
	rnd := rand.New(rand.NewSource(42))
	noise := make([]float32, n)
	for i := range noise {
		noise[i] = float32(rnd.Float64()*2 - 1)
	}
	cases["noise"] = noise
	mix := make([]float32, n)
	for i := range mix {
		t := float64(i) / 8000
		env := 0.5 + 0.5*math.Sin(2*math.Pi*0.7*t)
		mix[i] = float32(env * (0.3*math.Sin(2*math.Pi*220*t) + 0.2*math.Sin(2*math.Pi*660*t) + 0.1*math.Sin(2*math.Pi*1870*t)))
	}
	cases["mix"] = mix
	for name, pcm := range cases {
		t.Run(name, func(t *testing.T) {
			want := wasmPlaintext(t, pcm)
			spec, frames, err := spectrogram(pcm)
			if err != nil {
				t.Fatal(err)
			}
			peaks := extractPeaks(spec, frames)
			got := buildPlaintext(peaks, 6)
			if !bytes.Equal(got, want) {
				i := 0
				for i < len(got) && i < len(want) && got[i] == want[i] {
					i++
				}
				t.Fatalf("plaintext mismatch at %d: got len %d want %d\n got %x\nwant %x", i, len(got), len(want), got[max(0, i-8):min(len(got), i+32)], want[max(0, i-8):min(len(want), i+32)])
			}
			fmt.Printf("%s: %d peaks, %d bytes OK\n", name, len(peaks), len(got))
		})
	}
	_ = os.Stdout
}
