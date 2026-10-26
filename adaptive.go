package ncmrecognize

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
)

// Adaptive length support.
//
// The NetEase endpoint does not accept every clip length. Empirically the
// "duration" form field must be one of the buckets below (the server floors the
// value, so 6.9 is treated as 6), and the fingerprint's embedded duration must
// be within adaptiveTolerance seconds of that form value. A recording whose
// length is not directly usable is therefore moved to the nearest usable
// length: earlier audio is kept and, if needed, silence is appended. Inside a
// gap the smallest change wins; on a tie padding is preferred. Truncation is
// also used for recordings beyond the largest bucket's range.
var adaptiveBuckets = []int{2, 3, 4, 6, 9, 12, 15}

const (
	// AdaptiveKeep means all requested audio was used unchanged.
	AdaptiveKeep = "keep"
	// AdaptivePad means the window was extended to a longer usable length.
	// If the source ends before that length, the remainder is silence.
	AdaptivePad = "pad"
	// AdaptiveTruncate means the tail was dropped to reach a usable length.
	AdaptiveTruncate = "truncate"

	adaptiveTolerance  = 1.0  // seconds; |formDuration - actualDuration| <= this
	adaptiveMaxSeconds = 16.0 // form bucket 15 + tolerance
	adaptiveMinSeconds = 1.0  // below this the match API never returns a result
)

// FingerprintInfo describes how an adaptive fingerprint was produced.
type FingerprintInfo struct {
	// Duration is the value to send as the API's "duration" form field.
	Duration int
	// Used is the length of audio actually fingerprinted, in seconds. It is
	// also the duration embedded in the fingerprint itself.
	Used float64
	// Requested is the requested length after clamping to the available audio.
	Requested float64
	// Action is AdaptiveKeep, AdaptivePad or AdaptiveTruncate.
	Action string
}

// chooseAdaptiveLength applies the endpoint's duration rules. It returns the
// audio length to fingerprint, the form duration and the action taken.
func chooseAdaptiveLength(sec float64) (used float64, form int, action string, err error) {
	if math.IsNaN(sec) || math.IsInf(sec, 0) {
		return 0, 0, "", errors.New("invalid duration")
	}
	if sec < adaptiveMinSeconds {
		return 0, 0, "", fmt.Errorf("audio too short: need at least %.1f s (1.5 s or more recommended)", adaptiveMinSeconds)
	}
	// Prefer keeping the full clip when a supported form duration is within
	// the tolerance. On a tie, pick the larger bucket (keeps more of the
	// query window available server-side).
	best, bestDiff := 0, math.Inf(1)
	for _, b := range adaptiveBuckets {
		d := math.Abs(float64(b) - sec)
		if d <= adaptiveTolerance && d <= bestDiff {
			best, bestDiff = b, d
		}
	}
	if best != 0 {
		return sec, best, AdaptiveKeep, nil
	}
	// Beyond the last supported window: truncate to the top of bucket 15's
	// tolerance range.
	if sec > adaptiveMaxSeconds {
		return adaptiveMaxSeconds, adaptiveBuckets[len(adaptiveBuckets)-1], AdaptiveTruncate, nil
	}
	// The clip sits in a gap between two tolerance ranges (e.g. 7..8 s).
	// Find the nearest boundary of the adjacent ranges and move the minimum
	// amount. A tie is resolved by padding, preserving all recorded audio.
	var lowerF, upperF int
	lowerTarget, upperTarget := 0.0, 0.0
	for _, b := range adaptiveBuckets {
		if float64(b)+adaptiveTolerance < sec {
			lowerF, lowerTarget = b, float64(b)+adaptiveTolerance
		}
		if float64(b)-adaptiveTolerance > sec && upperF == 0 {
			upperF, upperTarget = b, float64(b)-adaptiveTolerance
		}
	}
	// For every gap between the buckets, both bounds exist. The checks below
	// are kept as a guard in case the bucket list is changed.
	if lowerF == 0 || upperF == 0 {
		return adaptiveMaxSeconds, adaptiveBuckets[len(adaptiveBuckets)-1], AdaptiveTruncate, nil
	}
	if upperTarget-sec <= sec-lowerTarget {
		return upperTarget, upperF, AdaptivePad, nil
	}
	return lowerTarget, lowerF, AdaptiveTruncate, nil
}

// FingerprintAdaptive fingerprints a clip of the given length (seconds) at
// offsetSec and adapts it to a duration accepted by the match endpoint. Pass
// seconds <= 0 to use all audio available after offsetSec (capped at 16 s).
//
// The returned FingerprintInfo.Duration must be sent as the API "duration"
// field; Client.MatchDuration / Client.RecognizeAdaptive do this
// automatically.
func (e *Engine) FingerprintAdaptive(ctx context.Context, pcm []float32, sampleRate int, offsetSec, seconds float64) (string, FingerprintInfo, error) {
	var info FingerprintInfo
	if sampleRate <= 0 {
		return "", info, errors.New("invalid sample rate")
	}
	if math.IsNaN(offsetSec) || math.IsInf(offsetSec, 0) {
		return "", info, errors.New("invalid offset")
	}
	if offsetSec < 0 {
		return "", info, errors.New("invalid offset")
	}
	start := offsetSec * float64(sampleRate)
	if !(start < float64(len(pcm))) {
		return "", info, errors.New("offset is past the end of the audio")
	}
	available := (float64(len(pcm)) - start) / float64(sampleRate)
	req := seconds
	if req <= 0 || req > available {
		req = available
	}
	used, form, action, err := chooseAdaptiveLength(req)
	if err != nil {
		return "", info, err
	}
	n := int(math.Round(used * FingerprintRate))
	if n < fftLen {
		return "", info, fmt.Errorf("audio too short: %d samples", n)
	}
	in := resampleAt(pcm, sampleRate, offsetSec, n)
	raw, err := fingerprintRawSamples(in)
	if err != nil {
		return "", info, err
	}
	info = FingerprintInfo{
		Duration:  form,
		Used:      float64(n) / FingerprintRate,
		Requested: req,
		Action:    action,
	}
	return base64.StdEncoding.EncodeToString(raw), info, nil
}

// FingerprintAdaptive is the package-level version of
// (*Engine).FingerprintAdaptive.
func FingerprintAdaptive(ctx context.Context, pcm []float32, sampleRate int, offsetSec, seconds float64) (string, FingerprintInfo, error) {
	return getDefaultEngine().FingerprintAdaptive(ctx, pcm, sampleRate, offsetSec, seconds)
}
