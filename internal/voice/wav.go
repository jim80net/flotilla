package voice

import (
	"bytes"
	"encoding/binary"
)

// encodeWAV wraps mono 16-bit PCM in a canonical 44-byte RIFF/WAVE header so the STT
// provider receives a self-describing audio clip (sample rate + format inline) rather than
// a headerless PCM blob it would have to guess at. The design's STT contract is "PCM/wav";
// this is the wav. Little-endian throughout (the WAVE format and Discord/libopus PCM are
// both LE).
func encodeWAV(pcm []int16, sampleRate, channels int) []byte {
	const (
		bitsPerSample = 16
		headerSize    = 44
	)
	dataBytes := len(pcm) * 2 // 16-bit samples
	byteRate := sampleRate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8

	buf := bytes.NewBuffer(make([]byte, 0, headerSize+dataBytes))
	// RIFF chunk descriptor.
	buf.WriteString("RIFF")
	writeU32(buf, uint32(headerSize-8+dataBytes)) // ChunkSize = 36 + Subchunk2Size
	buf.WriteString("WAVE")
	// "fmt " subchunk (PCM).
	buf.WriteString("fmt ")
	writeU32(buf, 16)                 // Subchunk1Size (16 for PCM)
	writeU16(buf, 1)                  // AudioFormat = 1 (PCM, uncompressed)
	writeU16(buf, uint16(channels))   // NumChannels
	writeU32(buf, uint32(sampleRate)) // SampleRate
	writeU32(buf, uint32(byteRate))   // ByteRate
	writeU16(buf, uint16(blockAlign)) // BlockAlign
	writeU16(buf, bitsPerSample)      // BitsPerSample
	// "data" subchunk.
	buf.WriteString("data")
	writeU32(buf, uint32(dataBytes)) // Subchunk2Size
	for _, s := range pcm {
		writeU16(buf, uint16(s)) // two's-complement int16 → identical LE bytes
	}
	return buf.Bytes()
}

func writeU16(buf *bytes.Buffer, v uint16) { _ = binary.Write(buf, binary.LittleEndian, v) }
func writeU32(buf *bytes.Buffer, v uint32) { _ = binary.Write(buf, binary.LittleEndian, v) }
