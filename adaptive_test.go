package ncmrecognize

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"math"
	"testing"
)

func TestChooseAdaptiveLength(t *testing.T) {
	cases := []struct {
		sec    float64
		used   float64
		form   int
		action string
	}{
		{1.0, 1.0, 2, AdaptiveKeep},
		{1.5, 1.5, 2, AdaptiveKeep},
		{1.9, 1.9, 2, AdaptiveKeep},
		{2.0, 2.0, 2, AdaptiveKeep},
		{2.5, 2.5, 3, AdaptiveKeep},
		{3.0, 3.0, 3, AdaptiveKeep},
		{4.0, 4.0, 4, AdaptiveKeep},
		{4.5, 4.5, 4, AdaptiveKeep},
		{5.0, 5.0, 6, AdaptiveKeep},
		{5.5, 5.5, 6, AdaptiveKeep},
		{6.0, 6.0, 6, AdaptiveKeep},
		{6.5, 6.5, 6, AdaptiveKeep},
		{6.9, 6.9, 6, AdaptiveKeep},
		{7.0, 7.0, 6, AdaptiveKeep},
		{7.2, 7.0, 6, AdaptiveTruncate},
		{7.4, 7.0, 6, AdaptiveTruncate},
		{7.5, 8.0, 9, AdaptivePad},
		{7.6, 8.0, 9, AdaptivePad},
		{7.8, 8.0, 9, AdaptivePad},
		{8.0, 8.0, 9, AdaptiveKeep},
		{8.5, 8.5, 9, AdaptiveKeep},
		{9.0, 9.0, 9, AdaptiveKeep},
		{9.9, 9.9, 9, AdaptiveKeep},
		{10.0, 10.0, 9, AdaptiveKeep},
		{10.4, 10.0, 9, AdaptiveTruncate},
		{10.5, 11.0, 12, AdaptivePad},
		{10.6, 11.0, 12, AdaptivePad},
		{11.0, 11.0, 12, AdaptiveKeep},
		{12.9, 12.9, 12, AdaptiveKeep},
		{13.0, 13.0, 12, AdaptiveKeep},
		{13.4, 13.0, 12, AdaptiveTruncate},
		{13.5, 14.0, 15, AdaptivePad},
		{13.6, 14.0, 15, AdaptivePad},
		{14.0, 14.0, 15, AdaptiveKeep},
		{15.9, 15.9, 15, AdaptiveKeep},
		{16.0, 16.0, 15, AdaptiveKeep},
		{16.1, 16.0, 15, AdaptiveTruncate},
		{16.5, 16.0, 15, AdaptiveTruncate},
		{20.0, 16.0, 15, AdaptiveTruncate},
	}
	for _, c := range cases {
		used, form, action, err := chooseAdaptiveLength(c.sec)
		if err != nil {
			t.Errorf("%gs: unexpected error %v", c.sec, err)
			continue
		}
		if math.Abs(used-c.used) > 1e-9 || form != c.form || action != c.action {
			t.Errorf("%gs: got used=%g form=%d action=%s, want %g/%d/%s", c.sec, used, form, action, c.used, c.form, c.action)
		}
	}
	if _, _, _, err := chooseAdaptiveLength(0.9); err == nil {
		t.Error("0.9s: expected error")
	}
}

func TestFingerprintAdaptive(t *testing.T) {
	const rate = 48000
	const seconds = 20
	pcm := make([]float32, rate*seconds)
	for i := range pcm {
		pcm[i] = float32(0.5 * math.Sin(2*math.Pi*440*float64(i)/rate))
	}
	cases := []struct {
		seconds float64
		used    float64
		form    int
		action  string
	}{
		{2.5, 2.5, 3, AdaptiveKeep},
		{5.0, 5.0, 6, AdaptiveKeep},
		{7.5, 8.0, 9, AdaptivePad},
		{10.5, 11.0, 12, AdaptivePad},
		{16.5, 16.0, 15, AdaptiveTruncate},
		{0, 16.0, 15, AdaptiveTruncate}, // all available (20s), capped at 16s
	}
	for _, c := range cases {
		fp, info, err := FingerprintAdaptive(context.Background(), pcm, rate, 0, c.seconds)
		if err != nil {
			t.Fatalf("%gs: %v", c.seconds, err)
		}
		if info.Duration != c.form || math.Abs(info.Used-c.used) > 1e-9 || info.Action != c.action {
			t.Fatalf("%gs: got info %+v, want used=%g form=%d action=%s", c.seconds, info, c.used, c.form, c.action)
		}
		raw, err := base64.StdEncoding.DecodeString(fp)
		if err != nil {
			t.Fatal(err)
		}
		plain, err := decryptFingerprint(raw)
		if err != nil {
			t.Fatal(err)
		}
		gotDur := math.Float32frombits(binary.LittleEndian.Uint32(plain[35:]))
		if float64(gotDur) != info.Used {
			t.Fatalf("%gs: embedded duration %g, want %g", c.seconds, gotDur, info.Used)
		}
		if n, err := peakCount(plain); err != nil || n == 0 {
			t.Fatalf("%gs: bad peaks: n=%d err=%v", c.seconds, n, err)
		}
	}
}

func TestFingerprintAdaptiveClamp(t *testing.T) {
	const rate = 48000
	pcm := make([]float32, rate*20)
	for i := range pcm {
		pcm[i] = float32(0.5 * math.Sin(2*math.Pi*440*float64(i)/rate))
	}
	// offset 18s leaves 2s, even though 5s were requested.
	_, info, err := FingerprintAdaptive(context.Background(), pcm, rate, 18, 5)
	if err != nil {
		t.Fatal(err)
	}
	if info.Requested != 2 || info.Used != 2 || info.Duration != 2 || info.Action != AdaptiveKeep {
		t.Fatalf("clamped info = %+v", info)
	}
	// requesting more than the available 20s caps at the 16s maximum.
	_, info, err = FingerprintAdaptive(context.Background(), pcm, rate, 0, 30)
	if err != nil {
		t.Fatal(err)
	}
	if info.Requested != 20 || info.Used != 16 || info.Duration != 15 || info.Action != AdaptiveTruncate {
		t.Fatalf("long info = %+v", info)
	}
	// an offset past the end is an error.
	if _, _, err = FingerprintAdaptive(context.Background(), pcm, rate, 30, 5); err == nil {
		t.Fatal("expected offset-out-of-range error")
	}
}

func TestFingerprintAdaptiveTooShort(t *testing.T) {
	pcm := make([]float32, 48000/2) // 0.5 s
	if _, _, err := FingerprintAdaptive(context.Background(), pcm, 48000, 0, 0); err == nil {
		t.Fatal("expected error for 0.5 s")
	}
}
