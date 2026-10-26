package ncmrecognize

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"math"
	"testing"
)

// deterministicPCM44100 builds a reproducible 44.1 kHz signal without using
// libm, so a golden hash only depends on the resampler and fingerprint code.
func deterministicPCM44100(n int) []float32 {
	pcm := make([]float32, n)
	x := uint32(0x12345678)
	for i := range pcm {
		x = x*1664525 + 1013904223
		pcm[i] = float32(int32(x)) / 2147483648.0 * 0.5
	}
	return pcm
}

func hashFloat32s(pcm []float32) string {
	h := sha256.New()
	var b [4]byte
	for _, v := range pcm {
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
		h.Write(b[:])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// TestResample44100Stable locks the 44.1 kHz resampler output. Before the
// float barriers were added, arm64 (FMA) and amd64 (no FMA) produced different
// fingerprints for the same input.
func TestResample44100Stable(t *testing.T) {
	const rate = 44100
	pcm := deterministicPCM44100(rate * 8)
	in := resampleAt(pcm, rate, 0.5, FingerprintRate*FingerprintSeconds)
	const want = "1f35a01feae949e136078949556ed442347347a666a6360a604ba16d1421cc18"
	if got := hashFloat32s(in); got != want {
		t.Fatalf("resampled window hash = %s, want %s", got, want)
	}
}

// TestFingerprint44100Stable checks the end-to-end fixed and adaptive paths at
// a rate that requires interpolation. The hash is of the decrypted plaintext,
// so it is independent of the zlib encoder.
func TestFingerprint44100Stable(t *testing.T) {
	const rate = 44100
	pcm := deterministicPCM44100(rate * 8)
	const want = "67cc732d4d152e7e13d8f7673a126f582f517a589b40fcb307b503307b1a904c"

	checkHash := func(name string, fp string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		raw, err := base64.StdEncoding.DecodeString(fp)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		plain, err := decryptFingerprint(raw)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		sum := sha256.Sum256(plain)
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Fatalf("%s: plaintext hash = %s, want %s", name, got, want)
		}
	}

	fp, err := Fingerprint(context.Background(), pcm, rate, 0.5)
	checkHash("fixed", fp, err)

	afp, info, err := FingerprintAdaptive(context.Background(), pcm, rate, 0.5, 6)
	if err != nil {
		t.Fatalf("adaptive: %v", err)
	}
	if info.Duration != 6 || info.Used != 6 || info.Action != AdaptiveKeep {
		t.Fatalf("adaptive info = %+v", info)
	}
	checkHash("adaptive", afp, nil)
}

// TestFingerprintRejectsInvalidOffset covers the inputs that used to panic or
// silently produce an empty fingerprint (NaN/Inf/huge offsets).
func TestFingerprintRejectsInvalidOffset(t *testing.T) {
	pcm := make([]float32, 48000*FingerprintSeconds)
	for _, tc := range []struct {
		name string
		off  float64
	}{
		{"nan", math.NaN()},
		{"+inf", math.Inf(1)},
		{"-inf", math.Inf(-1)},
		{"huge", 1e300},
		{"negative", -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Fingerprint panicked: %v", r)
				}
			}()
			if _, err := Fingerprint(context.Background(), pcm, 48000, tc.off); err == nil {
				t.Fatal("Fingerprint: expected error")
			}
			if _, _, err := FingerprintAdaptive(context.Background(), pcm, 48000, tc.off, 6); err == nil {
				t.Fatal("FingerprintAdaptive: expected error")
			}
		})
	}
}

// TestAdaptivePadsShortSource exercises the actual zero-fill branch: the
// source is 7.5 s, the adaptive chooser extends the window to 8 s.
func TestAdaptivePadsShortSource(t *testing.T) {
	const rate = 48000
	pcm := make([]float32, rate*15/2)
	for i := range pcm {
		pcm[i] = float32(0.5 * math.Sin(2*math.Pi*440*float64(i)/rate))
	}

	in := resampleAt(pcm, rate, 0, 8*FingerprintRate)
	if len(in) != 8*FingerprintRate {
		t.Fatalf("len = %d", len(in))
	}
	realSamples := len(pcm) * FingerprintRate / rate
	for i := realSamples; i < len(in); i++ {
		if in[i] != 0 {
			t.Fatalf("sample %d = %g, want silence", i, in[i])
		}
	}

	fp, info, err := FingerprintAdaptive(context.Background(), pcm, rate, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if info.Duration != 9 || info.Used != 8 || info.Requested != 7.5 || info.Action != AdaptivePad {
		t.Fatalf("info = %+v", info)
	}
	raw, err := base64.StdEncoding.DecodeString(fp)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := decryptFingerprint(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := math.Float32frombits(binary.LittleEndian.Uint32(plain[35:])); got != 8 {
		t.Fatalf("embedded duration = %g, want 8", got)
	}
}

func lcgPCM(n int) []float32 {
	pcm := make([]float32, n)
	x := uint32(0x9e3779b9)
	for i := range pcm {
		x = x*1664525 + 1013904223
		pcm[i] = float32(int32(x)) / 2147483648.0 * 0.75
	}
	return pcm
}

func squarePCM(n, period int) []float32 {
	pcm := make([]float32, n)
	for i := range pcm {
		if (i/period)%2 == 0 {
			pcm[i] = 0.5
		} else {
			pcm[i] = -0.5
		}
	}
	return pcm
}

// TestFingerprintWasmGolden locks the fingerprint plaintext against the
// original afp.wasm for signals that are not part of the single committed
// sine.b64 fixture. The hashes were produced by running afp.wasm through the
// wasm-backed implementation.
func TestFingerprintWasmGolden(t *testing.T) {
	cases := []struct {
		name string
		pcm  []float32
		rate int
		off  float64
		want string
	}{
		{"8k-lcg", lcgPCM(8000 * 6), 8000, 0, "9180ade8ae74717e0c20a36af76112eb210478cca0dcc3eceb48ac2b6b1f1984"},
		{"8k-square", squarePCM(8000*6, 40), 8000, 0, "9d326e315f6a88c62fcbf332138a2ca5f9459a64379ec83ed366a918eb8f16f0"},
		{"48k-lcg", lcgPCM(48000 * 8), 48000, 1, "78ce87c23e2b0471ac8ec44152283692406d3c873726e65c630e572170f2ffa0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fp, err := Fingerprint(context.Background(), tc.pcm, tc.rate, tc.off)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := base64.StdEncoding.DecodeString(fp)
			if err != nil {
				t.Fatal(err)
			}
			plain, err := decryptFingerprint(raw)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(plain)
			if got := hex.EncodeToString(sum[:]); got != tc.want {
				t.Fatalf("plaintext hash = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestFixedAndAdaptiveAgree ensures the fixed 6 s path and the adaptive path
// share exactly the same resampling and fingerprint code for valid windows.
func TestFixedAndAdaptiveAgree(t *testing.T) {
	for _, rate := range []int{8000, 44100, 48000, 96000} {
		pcm := lcgPCM(rate * 10)
		for _, off := range []float64{0, 0.5, 1.25} {
			fp, err := Fingerprint(context.Background(), pcm, rate, off)
			if err != nil {
				t.Fatalf("rate=%d off=%g fixed: %v", rate, off, err)
			}
			afp, info, err := FingerprintAdaptive(context.Background(), pcm, rate, off, FingerprintSeconds)
			if err != nil {
				t.Fatalf("rate=%d off=%g adaptive: %v", rate, off, err)
			}
			if info.Duration != FingerprintSeconds || info.Used != FingerprintSeconds || info.Action != AdaptiveKeep {
				t.Fatalf("rate=%d off=%g adaptive info = %+v", rate, off, info)
			}
			fraw, err := base64.StdEncoding.DecodeString(fp)
			if err != nil {
				t.Fatal(err)
			}
			araw, err := base64.StdEncoding.DecodeString(afp)
			if err != nil {
				t.Fatal(err)
			}
			fplain, err := decryptFingerprint(fraw)
			if err != nil {
				t.Fatal(err)
			}
			aplain, err := decryptFingerprint(araw)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(fplain, aplain) {
				t.Fatalf("rate=%d off=%g fixed and adaptive differ", rate, off)
			}
		}
	}
}

func TestAdaptiveRejectsNaNDuration(t *testing.T) {
	pcm := make([]float32, 48000*FingerprintSeconds)
	if _, _, err := FingerprintAdaptive(context.Background(), pcm, 48000, 0, math.NaN()); err == nil {
		t.Fatal("expected error for NaN duration")
	}
}
