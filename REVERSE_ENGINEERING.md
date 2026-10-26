# Reverse-engineering notes: `afp.wasm`

This document records how the fingerprint algorithm in `afp.wasm` was recovered
and how the pure-Go implementation in this repository was validated against it.
The wasm itself is **not** included here: this repository ships only the pure-Go
port.

## Module overview

`afp.wasm` is an Emscripten/embind build (MVP, 231 KB) of the "hyai" audio
fingerprint SDK:

- version strings: `hyai_1.2.0_client_1.0.0`, `FPVER`
- classes visible in RTTI: `AFPClient`, `SpecCalculator`, `FFT_KISSFFT`,
  `FFT_Base`, `PeakExtractorCoreBaseline/Itf/RtgMax`, `AES_Encryption`
- source paths in assertions: `client/AFPClient.cpp`, `fft/SpecCalculator.cpp`,
  `util/Encryption.cpp`
- only public function: `ExtractQueryFP(std::string) -> vector<char>`
  (embind; the plugin calls it with a `Float32Array`)

The module has 27 imports (`a` … `A`, embind), 551 functions and a 16 MB memory.
Function indices in the text format start at 27 (the imports), so wasm function
index = `$N + 27`. During analysis the export section was patched at load time
to expose the Embind invokers and internal functions; that development-only
oracle is not included in this repository.

## Runtime tracing

`AFPClient` is a 48-byte object. `ExtractQueryFP` constructs it, feeds the PCM
and extracts:

```
$551(std::string& in, vector<char>* out):
    AFPClient* c = ctor()
    c->Feed(in.data, in.size/4)     // $280
    c->Extract(out)                 // $278
    dtor(c)
```

After `Feed`, the `SpecCalculator` object exposes:

| offset | value | meaning |
|---|---|---|
| +0 | 8000 | sample rate |
| +4 | 2048 | FFT length |
| +8 | 2048 | frame length |
| +12 | 160 | hop |
| +40 | `Param*` | configuration |
| +44 | `PeakExtractorCore*` | peak picker |
| +64/+68 | float vector | power spectrogram, 1025 values/frame |

The peak extractor's vtable is `{29,30,31,32,33}` (table indices);
table[32] = `$360` is `Extract()`. Calling `$360` directly with the spectrogram
reproduces `ExtractQueryFP`'s peak list, which made the algorithm tractable.

## Configuration

Recovered from the `AFPClient` constructor (`$293`) and the peak-extractor
object after construction:

| name | value |
|---|---|
| `freqResolution` | 3.90625 Hz (8000/2048) |
| `freqLB` / `freqUB` | 100 Hz / 4000 Hz |
| `intervalT` | 8 (frames) |
| `intervalF` | 10 (bins) |
| `maxPksPerFrm` | 30 |
| `removeSil` | true |
| bin range | 25 … 1023 (999 bins) |
| filter window | ±10 bins, ±5 frames |
| rival window | ±30 bins, ±8 frames |

The window table at runtime is a **symmetric Hamming** window:
`w[n] = 0.54 - 0.46*cos(2*pi*n/(N-1))`, `N = 2048`; it matches the wasm table
bit for bit.

The FFT is KISS FFT with radix-4/2 factors
`(4,512)(4,128)(4,32)(4,8)(4,2)(2,1)`; twiddles are `exp(-2*pi*i*k/N)` in
float32. The wasm does not fuse multiply-adds, so the Go port uses explicit
float32 rounding barriers (`f32mul`, `f32add`, …) to stay bit-exact on arm64.
The input resampler uses the same barriers (and float64 barriers for the
sample position), which also keeps non-48 kHz fingerprints identical across
architectures; 48 kHz input still matches the web client's point sampling.

## Peak pipeline (`$360`)

1. Transpose bins 25…1023 of the frame-major power spectrogram into a
   `999 × frames` bin-major matrix.
2. `x = logf(sqrt(x))` with `max(x, 1.119209287026024e-06)`; `logf` is musl's
   float32 log (`$55`), reproduced in `logf.go`.
3. Subtract the mean of the whole matrix (float64 accumulation, float32
   result).
4. 3×3 local maximum scan. **Quirk:** after a peak, the time scan advances by 2
   frames instead of 1; otherwise by 1.
5. Interference filter: for each peak with value > 0, take the mean of its
   ±10 bin / ±5 frame neighborhood; keep if `mean > 2` or
   `peak - mean > 1`.
6. Rival suppression: drop a peak if any value in its ±30 bin / ±8 frame
   neighborhood is strictly greater.
7. Add 25 to the bin index; sort by (frame, bin).

The result is a list of `{i32 bin, i32 frame, f32 logLevel}` records.

## Serialized plaintext

```
u32 23, "hyai_1.2.0_client_1.0.0"
u32 0, f32 0, f32 duration
"FPVER", u32 23, "hyai_1.2.0_client_1.0.0"
"Peak", u32 count, count * {i32 bin, i32 frame, f32 level}
```

Header layout was confirmed by disassembling the serializer at the start of
`$278` (references to the globals holding the version and the `"Peak"` tag).

## Encryption

The serializer output is compressed with zlib level 6 (`deflateInit2` arguments
recovered from the inlined zlib state: level 6, method 8, windowBits 15,
memLevel 8, strategy 0), padded with PKCS#7 and encrypted with AES-128-ECB.

The 16-byte AES key was recovered by scanning the wasm heap after an extraction
for a valid AES-128 key schedule:

```
key = "4B97221F27F02907"   (ASCII, row-major AES key schedule at 0x509690)
```

Decrypting the output with that key and decompressing yields the plaintext
above; the trailing bytes are PKCS#7 padding (`0x10 * 16`, `0x02 * 2`, …).

Go's `compress/zlib` is used instead of porting zlib's deflate; its stream is
valid and decodes to the identical plaintext, but is not byte-identical to
zlib 1.2.11. The NetEase endpoint accepts it (verified end to end).

## Server-side duration constraints (empirical)

The wasm itself processes arbitrary lengths (up to its 16 MB heap limit, around
20–21 s). The match endpoint is stricter. The following was measured against
`interface.music.163.com` with the recorded `MyGO!!!!! - 壱雫空.mp3` at many
offsets:

- The `duration` form value is floored. `floor(duration)` must be one of
  **2, 3, 4, 6, 9, 12, 15**; form values such as 5, 7, 8, 10, 11, 13, 14 and
  >=16 are rejected even when the fingerprint is valid.
- The fingerprint's embedded duration (serialized at header offset 35) must be
  within **±1 s** of the form value. `5 s` fingerprint + form `4` or `6` works;
  + form `3.9` (difference 1.1 s) fails.
- Fractions inside a valid bucket are accepted: 6.9 is treated as 6, 9.99 as 9,
  15.9 as 15.
- The practical actual-length range is about **1.5–16 s**. 1.2 s is too short
  even with form 2; 16 s works with form 15; 16.5 s fails unless truncated to
  16 s.
- 6 s is the official plugin value and remains the safest choice.

The adaptive Go API encodes these rules:

1. Keep the full clip if some supported bucket is within ±1 s; choose the
   nearest bucket (ties prefer the larger one).
2. In a gap (e.g. 7.5 s), move to the nearest boundary (7 s or 8 s) by the
   smallest amount; on a tie pad with silence (8 s, form 9) instead of
   truncating, because truncation can remove the only distinctive audio.
3. Above 16 s, truncate to 16 s and use form 15.

Example: for `L=2.5 s` on the second live ogg clip, truncating to 2 s failed at
2 of 4 offsets, while keeping 2.5 s (form 2/3) or padding to 3 s matched 4/4.
For gaps 7.5/10.5/13.5 s all of truncate-minimal, keep, and pad-minimal matched
in our sample; padding is preferred because it never discards content.

## Validation

The native implementation was compared against the original wasm during
development, before the wasm oracle harness was removed from this repository:

- Both implementations' fingerprints were decrypted and decompressed; the
  serialized plaintext was byte-identical.
- Covered inputs: 100/440/1000/3800 Hz tones, a frequency sequence, white
  noise, a music-like mix, and the two recorded `2026-10-26 *.ogg` clips.
- End-to-end: both clips are recognized as MyGO!!!!! songs through
  `interface.music.163.com` with the pure-Go pipeline.

## Reproducing the analysis

The original `afp.wasm` is not shipped in this repository. To repeat the
analysis, obtain it from the NetEase plugin or the upstream source, then:

- disassemble it with binaryen: `wasm-dis afp.wasm -o afp.wat`
- the development-only oracle (not included here) loaded it with wazero,
  patched the export section to expose internal functions, and compared the
  power spectrogram and `logf32` output against the Go port; the native
  spectrogram matched all 295 200 float32 values for a 6 s test signal, and
  `logf32` matched on 200 000 random values.
