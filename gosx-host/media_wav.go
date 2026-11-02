package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

func probePCM(path string) (ProjectDocument, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	head := make([]byte, 12)
	if _, err = io.ReadFull(f, head); err != nil || string(head[:4]) != "RIFF" || string(head[8:]) != "WAVE" {
		return nil, fmt.Errorf("use a readable PCM WAV soundtrack")
	}
	channels, rate, alignment, bits := uint16(0), uint32(0), uint16(0), uint16(0)
	var dataSize int64
	for offset := int64(12); offset+8 <= info.Size(); {
		h := make([]byte, 8)
		if _, err = f.ReadAt(h, offset); err != nil {
			return nil, err
		}
		size := int64(binary.LittleEndian.Uint32(h[4:]))
		offset += 8
		if size > info.Size()-offset {
			return nil, fmt.Errorf("WAV upload is truncated")
		}
		switch string(h[:4]) {
		case "fmt ":
			if size < 16 || size > 4096 {
				return nil, fmt.Errorf("invalid PCM format")
			}
			b := make([]byte, size)
			if _, err = f.ReadAt(b, offset); err != nil {
				return nil, err
			}
			format := binary.LittleEndian.Uint16(b)
			if format == 0xfffe && size >= 40 && binary.LittleEndian.Uint16(b[24:]) == 1 {
				format = 1
			}
			if format != 1 {
				return nil, fmt.Errorf("soundtrack must be PCM WAV")
			}
			channels = binary.LittleEndian.Uint16(b[2:])
			rate = binary.LittleEndian.Uint32(b[4:])
			alignment = binary.LittleEndian.Uint16(b[12:])
			bits = binary.LittleEndian.Uint16(b[14:])
		case "data":
			dataSize += size
		}
		offset += size + (size % 2)
	}
	if channels < 1 || channels > 8 || rate == 0 || bits < 8 || bits > 32 || bits%8 != 0 || alignment != channels*(bits/8) || dataSize <= 0 || dataSize%int64(alignment) != 0 {
		return nil, fmt.Errorf("invalid PCM WAV channels, sample rate or sample data")
	}
	duration := float64(dataSize) / float64(alignment) / float64(rate)
	if duration <= 0 || duration > 7200 {
		return nil, fmt.Errorf("use a soundtrack up to two hours long")
	}
	return ProjectDocument{"duration_seconds": rawValue(duration), "sample_rate": rawValue(rate), "channels": rawValue(channels)}, nil
}
