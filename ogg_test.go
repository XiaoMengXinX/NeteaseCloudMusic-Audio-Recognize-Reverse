//go:build oracle

package ncmrecognize

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"
)

func resample8k(pcm []float32, sampleRate int) []float32 {
	n := FingerprintRate * FingerprintSeconds
	step := float64(sampleRate) / FingerprintRate
	in := make([]float32, n)
	for i := range in {
		pos := float64(i) * step
		k := int(pos)
		frac := float32(pos - float64(k))
		if frac == 0 || k+1 >= len(pcm) {
			in[i] = pcm[k]
		} else {
			in[i] = pcm[k]*(1-frac) + pcm[k+1]*frac
		}
	}
	return in
}

func TestOggPlaintextMatchesWasm(t *testing.T) {
	for _, f := range []string{"testdata/2026-10-26 16.14.32.ogg", "testdata/2026-10-26 16.16.05.ogg"} {
		t.Run(f, func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Skip(err)
			}
			a, err := Decode(data)
			if err != nil {
				t.Fatal(err)
			}
			in := resample8k(a.PCM, a.SampleRate)
			want := wasmPlaintext(t, in)
			spec, frames, err := spectrogram(in)
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
				t.Fatalf("mismatch at %d got len %d want %d\n got %x\nwant %x", i, len(got), len(want), got[max(0, i-16):min(len(got), i+32)], want[max(0, i-16):min(len(want), i+32)])
			}
			fmt.Printf("rate=%d pcm=%d peaks=%d plain=%d OK\n", a.SampleRate, len(a.PCM), len(peaks), len(got))
			// native encrypted output must decrypt to the same plaintext
			raw, err := fingerprintRaw(in)
			if err != nil {
				t.Fatal(err)
			}
			back, err := decryptFingerprint(raw)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(back, want) {
				t.Fatalf("encryption roundtrip mismatch")
			}
			_ = context.Background()
		})
	}
}
