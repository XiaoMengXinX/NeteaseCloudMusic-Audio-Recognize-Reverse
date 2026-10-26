package ncmrecognize

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"math"
	"os"
	"testing"
)

// testdata/sine.b64 was produced by the original Node.js implementation
// (afp.wasm). Decrypting it and the native output must yield identical
// fingerprint plaintext; only the zlib encoder differs.
func TestFingerprintMatchesNode(t *testing.T) {
	enc, err := os.ReadFile("testdata/sine.b64")
	if err != nil {
		t.Skip(err)
	}
	ct, err := base64.StdEncoding.DecodeString(string(trimSpace(enc)))
	if err != nil {
		t.Fatal(err)
	}
	want, err := decryptFingerprint(ct)
	if err != nil {
		t.Fatal(err)
	}
	pcm := make([]float32, 48000*6)
	for i := range pcm {
		pcm[i] = float32(math.Sin(float64(i)*0.01) * 0.5)
	}
	fp, err := Fingerprint(context.Background(), pcm, 48000, 0)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(fp)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decryptFingerprint(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		i := 0
		for i < len(got) && i < len(want) && got[i] == want[i] {
			i++
		}
		t.Fatalf("plaintext mismatch at %d\n got %x\nwant %x", i, got[max(0, i-16):min(len(got), i+32)], want[max(0, i-16):min(len(want), i+32)])
	}
}

// TestFingerprintOggSmoke exercises the full decode -> resample -> fingerprint
// path on the two recorded test clips. The exact PCM (and therefore the exact
// peak set) depends on the Opus decoder's floating-point rounding, so this
// checks the fingerprint structure and plausibility; the development-time wasm
// oracle verified byte equality with afp.wasm for the same PCM.
func TestFingerprintOggSmoke(t *testing.T) {
	for _, file := range []string{
		"testdata/2026-10-26 16.14.32.ogg",
		"testdata/2026-10-26 16.16.05.ogg",
	} {
		t.Run(file, func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Skip(err)
			}
			a, err := Decode(data)
			if err != nil {
				t.Fatal(err)
			}
			fp, err := Fingerprint(context.Background(), a.PCM, a.SampleRate, 0)
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
			if !bytes.Contains(plain, []byte("hyai_1.2.0_client_1.0.0")) {
				t.Fatalf("missing version header")
			}
			n, err := peakCount(plain)
			if err != nil {
				t.Fatal(err)
			}
			if n < 100 || n > 400 {
				t.Fatalf("implausible peak count %d", n)
			}
			if len(plain) != 79+n*12 {
				t.Fatalf("plaintext length %d for %d peaks", len(plain), n)
			}
		})
	}
}

func peakCount(plain []byte) (int, error) {
	i := bytes.Index(plain, []byte("Peak"))
	if i < 0 || i+8 > len(plain) {
		return 0, errors.New("no Peak section")
	}
	return int(uint32(plain[i+4]) | uint32(plain[i+5])<<8 | uint32(plain[i+6])<<16 | uint32(plain[i+7])<<24), nil
}

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	return b
}
