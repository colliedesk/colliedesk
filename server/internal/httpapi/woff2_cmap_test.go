package httpapi_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/andybalholm/brotli"
)

// woff2Runes returns the characters a WOFF2 font maps to a glyph, read from its cmap table (formats 4
// and 12), so a test can prove what a served font draws without a browser.
func woff2Runes(font []byte) (map[rune]bool, error) {
	if len(font) < 48 || string(font[:4]) != "wOF2" {
		return nil, errors.New("not a WOFF2 font")
	}
	numTables := int(binary.BigEndian.Uint16(font[12:]))
	compressed := int(binary.BigEndian.Uint32(font[20:]))
	at := 48
	readBase128 := func() (int, error) {
		value := 0
		for i := 0; i < 5; i++ {
			if at >= len(font) {
				return 0, io.ErrUnexpectedEOF
			}
			b := font[at]
			at++
			value = value<<7 | int(b&0x7f)
			if b&0x80 == 0 {
				return value, nil
			}
		}
		return 0, errors.New("UIntBase128 longer than 5 bytes")
	}
	// The table directory: a known-tag index (0 is cmap, 10 glyf, 11 loca) or 63 and the tag itself,
	// then the original length and, for a transformed table, the transformed length that the stream holds.
	cmapOffset, cmapLength, offset := -1, 0, 0
	for i := 0; i < numTables; i++ {
		if at >= len(font) {
			return nil, io.ErrUnexpectedEOF
		}
		flags := font[at]
		at++
		index, version := int(flags&0x3f), flags>>6
		tag := ""
		if index == 63 {
			tag = string(font[at : at+4])
			at += 4
		}
		length, err := readBase128()
		if err != nil {
			return nil, err
		}
		glyfOrLoca := index == 10 || index == 11 || tag == "glyf" || tag == "loca"
		if glyfOrLoca && version != 3 || !glyfOrLoca && version != 0 {
			if length, err = readBase128(); err != nil {
				return nil, err
			}
		}
		if index == 0 || tag == "cmap" {
			cmapOffset, cmapLength = offset, length
		}
		offset += length
	}
	if cmapOffset < 0 {
		return nil, errors.New("no cmap table")
	}
	if at+compressed > len(font) {
		return nil, io.ErrUnexpectedEOF
	}
	stream, err := io.ReadAll(brotli.NewReader(bytes.NewReader(font[at : at+compressed])))
	if err != nil {
		return nil, err
	}
	if cmapOffset+cmapLength > len(stream) {
		return nil, fmt.Errorf("cmap at %d+%d beyond the %d-byte stream", cmapOffset, cmapLength, len(stream))
	}
	return cmapRunes(stream[cmapOffset : cmapOffset+cmapLength])
}

func cmapRunes(cmap []byte) (map[rune]bool, error) {
	be := binary.BigEndian
	runes := map[rune]bool{}
	for i := 0; i < int(be.Uint16(cmap[2:])); i++ {
		sub := cmap[be.Uint32(cmap[4+8*i+4:]):]
		switch be.Uint16(sub) {
		case 4:
			segments := int(be.Uint16(sub[6:])) / 2
			ends, starts, deltas, offsets := 14, 16+2*segments, 16+4*segments, 16+6*segments
			for s := 0; s < segments; s++ {
				end, start := int(be.Uint16(sub[ends+2*s:])), int(be.Uint16(sub[starts+2*s:]))
				delta, rangeOffset := int(be.Uint16(sub[deltas+2*s:])), int(be.Uint16(sub[offsets+2*s:]))
				for c := start; c <= end && c != 0xffff; c++ {
					glyph := (c + delta) & 0xffff
					if rangeOffset != 0 {
						at := offsets + 2*s + rangeOffset + 2*(c-start)
						if glyph = int(be.Uint16(sub[at:])); glyph != 0 {
							glyph = (glyph + delta) & 0xffff
						}
					}
					if glyph != 0 {
						runes[rune(c)] = true
					}
				}
			}
		case 12:
			for g := 0; g < int(be.Uint32(sub[12:])); g++ {
				group := sub[16+12*g:]
				for c := be.Uint32(group); c <= be.Uint32(group[4:]); c++ {
					runes[rune(c)] = true
				}
			}
		}
	}
	if len(runes) == 0 {
		return nil, errors.New("cmap maps no character")
	}
	return runes, nil
}
