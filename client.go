package ncmrecognize

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const matchURL = "https://interface.music.163.com/api/music/audio/match"

// Client calls the NetEase audio match API.
type Client struct {
	HTTP   *http.Client // defaults to http.DefaultClient
	Engine *Engine      // defaults to a shared engine

	// Duration is the clip length in seconds used by Recognize, RecognizeFile
	// and RecognizePCM. Zero (the default) means "all available audio",
	// adapted to the endpoint's supported lengths (2-16 s). Set it to 6 for
	// the fixed 6-second window, or to any other value for a custom length.
	Duration float64
	// Offset is the start offset in seconds used by Recognize and
	// RecognizeFile. RecognizePCM takes its offset as an argument.
	Offset float64
}

// Response is the API response. Raw always holds the unmodified JSON.
type Response struct {
	Code int             `json:"code"`
	Data json.RawMessage `json:"data"`
	Raw  json.RawMessage `json:"-"`
}

// Recognize fingerprints the audio (wav/mp3 bytes) and queries the API.
func (c *Client) Recognize(ctx context.Context, audio []byte) (*Response, error) {
	a, err := Decode(audio)
	if err != nil {
		return nil, err
	}
	return c.RecognizePCM(ctx, a.PCM, a.SampleRate, c.Offset)
}

// RecognizeFile is Recognize for a file path.
func (c *Client) RecognizeFile(ctx context.Context, path string) (*Response, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return c.Recognize(ctx, b)
}

// RecognizePCM queries the API with mono PCM samples (first channel) starting
// at offsetSec. The clip length is c.Duration (0 = all available audio, 6 = the
// fixed 6-second window).
func (c *Client) RecognizePCM(ctx context.Context, pcm []float32, sampleRate int, offsetSec float64) (*Response, error) {
	return c.RecognizeAdaptive(ctx, pcm, sampleRate, offsetSec, c.Duration)
}

// Match sends an already computed 6-second fingerprint to the API.
func (c *Client) Match(ctx context.Context, fingerprint string) (*Response, error) {
	return c.MatchDuration(ctx, fingerprint, FingerprintSeconds)
}

// RecognizeAdaptive decodes nothing: it fingerprints the given PCM with an
// adaptive clip length and sends the matching duration to the API. seconds is
// the clip length starting at offsetSec; <= 0 means all available audio.
func (c *Client) RecognizeAdaptive(ctx context.Context, pcm []float32, sampleRate int, offsetSec, seconds float64) (*Response, error) {
	var fp string
	var info FingerprintInfo
	var err error
	if c.Engine != nil {
		fp, info, err = c.Engine.FingerprintAdaptive(ctx, pcm, sampleRate, offsetSec, seconds)
	} else {
		fp, info, err = FingerprintAdaptive(ctx, pcm, sampleRate, offsetSec, seconds)
	}
	if err != nil {
		return nil, err
	}
	return c.MatchDuration(ctx, fp, info.Duration)
}

// RecognizeFileAdaptive is RecognizeAdaptive for a file path. seconds <= 0
// uses the whole file (bounded by the endpoint's maximum query length).
func (c *Client) RecognizeFileAdaptive(ctx context.Context, path string, seconds float64) (*Response, error) {
	return c.RecognizeFileAdaptiveAt(ctx, path, 0, seconds)
}

// RecognizeFileAdaptiveAt is RecognizeFileAdaptive with a start offset.
func (c *Client) RecognizeFileAdaptiveAt(ctx context.Context, path string, offsetSec, seconds float64) (*Response, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	a, err := Decode(b)
	if err != nil {
		return nil, err
	}
	return c.RecognizeAdaptive(ctx, a.PCM, a.SampleRate, offsetSec, seconds)
}

// MatchDuration sends an already computed fingerprint with an explicit
// "duration" form value. Use FingerprintAdaptive to obtain a compatible
// duration for clips that are not 6 seconds long.
func (c *Client) MatchDuration(ctx context.Context, fingerprint string, duration int) (*Response, error) {
	if duration <= 0 {
		duration = FingerprintSeconds
	}
	form := url.Values{
		"sessionId":     {newUUID()},
		"algorithmCode": {"shazam_v2"},
		"duration":      {strconv.Itoa(duration)},
		"rawdata":       {fingerprint},
		"times":         {"2"},
		"decrypt":       {"1"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, matchURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	for k, v := range map[string]string{
		"accept":             "*/*",
		"accept-language":    "zh-CN,zh-TW;q=0.9,zh;q=0.8",
		"cache-control":      "no-cache",
		"content-type":       "application/x-www-form-urlencoded; charset=UTF-8",
		"pragma":             "no-cache",
		"sec-ch-ua-platform": `"Windows"`,
		"sec-fetch-dest":     "empty",
		"sec-fetch-mode":     "cors",
		"sec-fetch-site":     "none",
	} {
		req.Header.Set(k, v)
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	r := &Response{Raw: body}
	if err := json.Unmarshal(body, r); err != nil {
		return nil, fmt.Errorf("bad response (HTTP %d): %w", resp.StatusCode, err)
	}
	return r, nil
}

func newUUID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
