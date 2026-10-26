# NeteaseCloudMusic-Audio-Recognize-Reverse

A **pure-Go** reimplementation of the NetEase Cloud Music browser plugin's
`afp.wasm` audio-fingerprint algorithm, plus the reverse-engineering notes.

The repository ships no WebAssembly file and has no WebAssembly runtime
dependency: the fingerprint algorithm is implemented entirely in Go. The
original `afp.wasm` was only used as the reference during reverse engineering.

```go
import ncm "github.com/XiaoMengXinX/NeteaseCloudMusic-Audio-Recognize-Reverse"

c := new(ncm.Client) // Duration 0 = whole recording, adapted to 2-16 s
resp, err := c.RecognizeFile(ctx, "song.ogg") // wav / mp3 / flac / ogg (vorbis, opus)
fmt.Println(string(resp.Raw))                  // raw API JSON; resp.Code, resp.Data too
```

## Install / CLI

```bash
go install github.com/XiaoMengXinX/NeteaseCloudMusic-Audio-Recognize-Reverse/cmd/ncmrecognize@latest

ncmrecognize song.mp3                                # whole recording, adaptive
ncmrecognize -duration 6 song.mp3                    # requested 6-second window
ncmrecognize -offset 30 -duration 7.5 song.mp3       # custom offset/length
```

The CLI prints the chosen fingerprint length and form duration to stderr:

```
fingerprint: 8.000s audio, form duration 9s (pad)
```

## Adaptive clip length (2–16 s)

The NetEase endpoint only accepts certain `duration` values. The client adapts
automatically: it keeps the full clip when possible, otherwise moves it to the
nearest usable length (on a tie it pads with silence rather than truncating) and
sends the matching `duration`.

`Client.Duration` controls the requested window:

- `0` (default) – all available audio, capped/adapted to 2–16 s;
- `6` – requested 6-second window (clamped to the available audio);
- any other value – that many seconds, adapted to a supported length.

`Client.Offset` sets the start offset for `Recognize` / `RecognizeFile`;
`RecognizePCM` takes the offset as its fourth argument. Lower-level APIs:

```go
// Whole recording. 0 = all available.
resp, err := new(ncm.Client).RecognizeAdaptive(ctx, pcm, 48000, 0, 0)

// Explicit length / offset:
resp, err = new(ncm.Client).RecognizeFileAdaptiveAt(ctx, "recording.mp3", 30, 7.5)

// Inspect the chosen duration:
fp, info, err := ncm.FingerprintAdaptive(ctx, pcm, 48000, 0, 7.5)
// info = {Duration: 9, Used: 8.0, Requested: 7.5, Action: "pad"}
resp, err = new(ncm.Client).MatchDuration(ctx, fp, info.Duration)
```

Empirical server rules (see [REVERSE_ENGINEERING.md](REVERSE_ENGINEERING.md)):

- `floor(duration)` must be one of **2, 3, 4, 6, 9, 12, 15**.
- The fingerprint's actual/embedded duration must be within **±1 s** of the
  form `duration`.
- Practical range is about **1.5–16 s**; shorter clips may have too little
  content, and >16 s is truncated to 16 s. 6 s remains a safe default.

The adaptive chooser keeps the full audio whenever a legal form duration is
within ±1 s; only the gaps (7–8, 10–11, 13–14 s) are adjusted, using the
smallest change. On a tie it **pads with silence instead of truncating**, since
truncation can remove the only distinctive part of a short clip.

## Supported input

Pure Go decoders, detected by content: WAV (PCM), MP3, FLAC, Ogg Vorbis,
Ogg Opus (via `pion/opus`). AAC/M4A/WMA are not supported; decode them yourself
and pass PCM to `FingerprintAdaptive` / `Client.RecognizePCM`.

`Fingerprint` / `Match` keep the fixed 6-second behaviour; use
`FingerprintAdaptive` / `MatchDuration` for custom lengths.

## How it works

The algorithm was recovered from the original Emscripten/embind `afp.wasm`
build of the "hyai" fingerprint SDK (`hyai_1.2.0_client_1.0.0`). This
repository contains only the pure-Go pipeline:

1. **Input** – channel 0, resampled to 8 kHz. The web client only accepts
   48 kHz and point-samples every 6th sample; other rates are an extension and
   use linear interpolation. Resampling uses the same non-fused float32
   arithmetic as the wasm pipeline, so fingerprints are identical on arm64 and
   amd64. (This deliberately does not reproduce the old Go wrapper's
   architecture-dependent FMA result for non-48 kHz input.)
2. **Spectrogram** – Hamming window `0.54 - 0.46*cos(2*pi*n/2047)`, FFT length
   2048, frame length 2048, hop 160 (20 ms); KISS FFT radix 4/2 in float32;
   stored value is `|X|²` for bins 0…1024.
3. **Log spectrum** – `ln(sqrt(power))` using musl's `logf`, then subtract the
   global mean.
4. **Peak picking** – 3×3 local maxima on bins 25…1023 (100–4000 Hz); after a
   peak the scan skips one time frame (a quirk of the original).
5. **Interference filter** – keep a peak if its ±10 bin / ±5 frame neighborhood
   mean is > 2, or if `peak - mean > 1`; drop peaks with value ≤ 0.
6. **Rival suppression** – drop a peak if any value in its ±30 bin / ±8 frame
   neighborhood is strictly larger.
7. **Serialization** – header `"hyai_1.2.0_client_1.0.0"`, `u32 0`, `f32 0`,
   `f32 duration`, `"FPVER"` + version, `"Peak"` + count, then 12-byte records
   `{i32 bin, i32 frame, f32 logLevel}` sorted by (frame, bin).
8. **Encryption** – zlib level 6, PKCS#7 padding, AES-128-ECB with the fixed key
   `4B97221F27F02907`, then base64.

Go's `compress/zlib` produces a valid but not byte-identical deflate stream
compared to the wasm's zlib 1.2.11. The decrypted/decompressed fingerprint data
matches the original implementation and the API accepts the Go-produced
stream.

See [REVERSE_ENGINEERING.md](REVERSE_ENGINEERING.md) for the recovered wasm
addresses, function indices, parameter values and validation methodology
(historical analysis; the wasm itself is not shipped here).

## Tests

```bash
go test ./...
```

`testdata/` is git-ignored because it contains optional recorded test clips,
except for the small synthetic `sine.b64` golden fixture. The wasm golden
fingerprint test therefore always runs; the format/smoke tests still skip
without the optional local clips. No WebAssembly runtime is involved.

## Credits

- [akinazuki/NeteaseCloudMusic-Audio-Recognize](https://github.com/akinazuki/NeteaseCloudMusic-Audio-Recognize)
- [MicroCBer/BeatSaverSongData](https://github.com/MicroCBer/BeatSaverSongData)
- The `hyai` audio-fingerprint SDK embedded in the NetEase plugin.

## License

GPL-3.0, see [LICENSE](LICENSE).
