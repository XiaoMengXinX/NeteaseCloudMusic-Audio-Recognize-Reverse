// Package ncmrecognize recognizes songs through NetEase Cloud Music's
// audio-match API. It is a pure-Go reimplementation of the browser plugin's
// afp.wasm fingerprint algorithm: no cgo, no WebAssembly runtime, no Node.js.
package ncmrecognize

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"sync"
)

const (
	// FingerprintRate is the sample rate expected by the fingerprint algorithm.
	FingerprintRate = 8000
	// FingerprintSeconds is the clip length used by the NetEase web client.
	FingerprintSeconds = 6
)

var (
	defaultOnce   sync.Once
	defaultEngine *Engine
)

// Engine computes NetEase audio fingerprints. It is stateless and safe for
// concurrent use.
type Engine struct{}

// NewEngine returns a fingerprint engine. The context is accepted for
// compatibility; no resources are allocated.
func NewEngine(ctx context.Context) (*Engine, error) { return &Engine{}, nil }

// Close is a no-op kept for API compatibility.
func (e *Engine) Close(ctx context.Context) error { return nil }

func getDefaultEngine() *Engine {
	defaultOnce.Do(func() { defaultEngine = &Engine{} })
	return defaultEngine
}

// Fingerprint returns the base64 fingerprint ("rawdata") for the given mono
// PCM samples, starting offsetSec seconds in. It takes FingerprintSeconds
// seconds of audio, resampled to 8 kHz like the web client: 48 kHz input is
// point-sampled every 6th sample; other rates use linear interpolation.
func Fingerprint(ctx context.Context, pcm []float32, sampleRate int, offsetSec float64) (string, error) {
	return getDefaultEngine().Fingerprint(ctx, pcm, sampleRate, offsetSec)
}

// Fingerprint is like the package-level Fingerprint but uses this engine.
func (e *Engine) Fingerprint(ctx context.Context, pcm []float32, sampleRate int, offsetSec float64) (string, error) {
	if sampleRate <= 0 {
		return "", errors.New("invalid sample rate")
	}
	if math.IsNaN(offsetSec) || math.IsInf(offsetSec, 0) {
		return "", errors.New("invalid offset")
	}
	n := FingerprintRate * FingerprintSeconds
	step := float64(sampleRate) / FingerprintRate
	start := offsetSec * float64(sampleRate)
	last := f64add(start, f64mul(float64(n-1), step))
	if start < 0 || !(last < float64(len(pcm))) {
		return "", fmt.Errorf("audio too short: need %d seconds from offset %.1fs", FingerprintSeconds, offsetSec)
	}
	in := resampleAt(pcm, sampleRate, offsetSec, n)
	raw, err := e.ExtractQueryFP(ctx, in)
	if err != nil {
		return "", err
	}
	if len(raw) <= 64 {
		return "", fmt.Errorf("fingerprint too short (%d bytes)", len(raw))
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

// ExtractQueryFP computes the raw (encrypted) fingerprint for exactly
// FingerprintRate*FingerprintSeconds samples of 8 kHz mono PCM.
func (e *Engine) ExtractQueryFP(ctx context.Context, pcm []float32) ([]byte, error) {
	return fingerprintRaw(pcm)
}
