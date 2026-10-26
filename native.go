package ncmrecognize

import (
	"bytes"
	"compress/zlib"
	"crypto/aes"
	"errors"
	"fmt"
	"math"
)

const (
	fftLen   = 2048
	hopLen   = 160
	verStr   = "hyai_1.2.0_client_1.0.0"
	aesKey   = "4B97221F27F02907"
	blockLen = aes.BlockSize
)

var (
	fft2048 = newKissFFT(fftLen)
	hamming = windowHamming(fftLen)
)

// powerSpectrum computes the 1025-bin power spectrum of one windowed frame.
func powerSpectrum(frame []float32) []float32 {
	buf := make([]complex64, fftLen)
	out := make([]complex64, fftLen)
	for i := 0; i < fftLen; i++ {
		buf[i] = complex(f32mul(frame[i], hamming[i]), 0)
	}
	fft2048.exec(out, buf)
	pow := make([]float32, specBins)
	for b := 0; b < specBins; b++ {
		re, im := real(out[b]), imag(out[b])
		pow[b] = f32add(f32mul(re, re), f32mul(im, im))
	}
	return pow
}

// spectrogram computes the frame-major power spectrogram exactly like the
// wasm SpecCalculator (2048-sample Hamming-windowed frames, 160-sample hop).
func spectrogram(pcm []float32) ([]float32, int, error) {
	if len(pcm) < fftLen {
		return nil, 0, errors.New("audio too short")
	}
	frames := 1 + (len(pcm)-fftLen)/hopLen
	spec := make([]float32, frames*specBins)
	for f := 0; f < frames; f++ {
		pow := powerSpectrum(pcm[f*hopLen : f*hopLen+fftLen])
		copy(spec[f*specBins:], pow)
	}
	return spec, frames, nil
}

func appendU32(b []byte, v uint32) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

func appendF32(b []byte, v float32) []byte {
	return appendU32(b, math.Float32bits(v))
}

// buildPlaintext serializes the fingerprint exactly like afp.wasm's AFPClient.
func buildPlaintext(peaks []peak, duration float32) []byte {
	b := make([]byte, 0, 128+len(peaks)*12)
	b = appendU32(b, uint32(len(verStr)))
	b = append(b, verStr...)
	b = appendU32(b, 0) // client+12 flag
	b = appendF32(b, 0) // client+20
	b = appendF32(b, duration)
	b = append(b, "FPVER"...)
	b = appendU32(b, uint32(len(verStr)))
	b = append(b, verStr...)
	b = append(b, "Peak"...)
	b = appendU32(b, uint32(len(peaks)))
	for _, p := range peaks {
		b = appendU32(b, uint32(p.bin))
		b = appendU32(b, uint32(p.frame))
		b = appendF32(b, p.val)
	}
	return b
}

// encryptFingerprint compresses with zlib (level 6), applies PKCS#7 padding
// and encrypts with AES-128-ECB, mirroring util/Encryption.cpp.
func encryptFingerprint(plain []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw, err := zlib.NewWriterLevel(&buf, 6)
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(plain); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	data := buf.Bytes()
	pad := blockLen - len(data)%blockLen
	for i := 0; i < pad; i++ {
		data = append(data, byte(pad))
	}
	block, err := aes.NewCipher([]byte(aesKey))
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += blockLen {
		block.Encrypt(out[i:i+blockLen], data[i:i+blockLen])
	}
	return out, nil
}

// fingerprintRaw computes the raw (encrypted) fingerprint for exactly
// 6 seconds of 8 kHz mono PCM.
func fingerprintRaw(pcm []float32) ([]byte, error) {
	if len(pcm) != FingerprintRate*FingerprintSeconds {
		return nil, fmt.Errorf("expected %d samples, got %d", FingerprintRate*FingerprintSeconds, len(pcm))
	}
	return fingerprintRawSamples(pcm)
}

// fingerprintRawSamples computes the raw (encrypted) fingerprint for an
// arbitrary length of 8 kHz mono PCM (at least one 2048-sample FFT frame).
// The embedded duration is len(pcm)/8000.
func fingerprintRawSamples(pcm []float32) ([]byte, error) {
	spec, frames, err := spectrogram(pcm)
	if err != nil {
		return nil, err
	}
	peaks := extractPeaks(spec, frames)
	return encryptFingerprint(buildPlaintext(peaks, float32(len(pcm))/8000))
}

// decryptFingerprint is the inverse of encryptFingerprint, used by tests.
func decryptFingerprint(ct []byte) ([]byte, error) {
	if len(ct) == 0 || len(ct)%blockLen != 0 {
		return nil, errors.New("bad ciphertext length")
	}
	block, err := aes.NewCipher([]byte(aesKey))
	if err != nil {
		return nil, err
	}
	pt := make([]byte, len(ct))
	for i := 0; i < len(ct); i += blockLen {
		block.Decrypt(pt[i:i+blockLen], ct[i:i+blockLen])
	}
	pad := int(pt[len(pt)-1])
	if pad < 1 || pad > blockLen || pad > len(pt) {
		return nil, errors.New("bad padding")
	}
	pt = pt[:len(pt)-pad]
	r, err := zlib.NewReader(bytes.NewReader(pt))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var out bytes.Buffer
	if _, err := out.ReadFrom(r); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
