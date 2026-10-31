package bpjs

// Port LZ-String (pieroxy, MIT) — hanya varian EncodedURIComponent yang dipakai BPJS.

import (
	"errors"
	"strings"
	"unicode/utf16"
)

const keyURI = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+-$"

var uriIndex = func() map[byte]int {
	m := map[byte]int{}
	for i := 0; i < len(keyURI); i++ {
		m[keyURI[i]] = i
	}
	return m
}()

// DecompressFromEncodedURIComponent setara LZString.decompressFromEncodedURIComponent.
func DecompressFromEncodedURIComponent(input string) (string, error) {
	if input == "" {
		return "", nil
	}
	input = strings.ReplaceAll(input, " ", "+")
	for i := 0; i < len(input); i++ {
		if _, ok := uriIndex[input[i]]; !ok {
			return "", errors.New("lzstring: karakter tidak valid")
		}
	}
	return decompress(len(input), 32, func(i int) int { return uriIndex[input[i]] })
}

func decompress(length, resetValue int, getNext func(int) int) (string, error) {
	dictionary := map[int][]uint16{0: {0}, 1: {1}, 2: {2}}
	enlargeIn, dictSize, numBits := 4, 4, 3
	var w, result []uint16
	dataVal, dataPos, dataIndex := getNext(0), resetValue, 1

	readBits := func(n int) (int, bool) {
		bits, power, maxpower := 0, 1, 1<<n
		for power != maxpower {
			resb := dataVal & dataPos
			dataPos >>= 1
			if dataPos == 0 {
				dataPos = resetValue
				if dataIndex >= length {
					if power*2 < maxpower { // data habis
						return bits, false
					}
					dataVal = 0
				} else {
					dataVal = getNext(dataIndex)
				}
				dataIndex++
			}
			if resb > 0 {
				bits |= power
			}
			power <<= 1
		}
		return bits, true
	}

	next, _ := readBits(2)
	var c uint16
	switch next {
	case 0:
		v, _ := readBits(8)
		c = uint16(v)
	case 1:
		v, _ := readBits(16)
		c = uint16(v)
	case 2:
		return "", nil
	}
	dictionary[3] = []uint16{c}
	w = []uint16{c}
	result = append(result, c)
	for {
		if dataIndex > length {
			return "", errors.New("lzstring: data terpotong")
		}
		cc, _ := readBits(numBits)
		switch cc {
		case 0:
			v, _ := readBits(8)
			dictionary[dictSize] = []uint16{uint16(v)}
			cc = dictSize
			dictSize++
			enlargeIn--
		case 1:
			v, _ := readBits(16)
			dictionary[dictSize] = []uint16{uint16(v)}
			cc = dictSize
			dictSize++
			enlargeIn--
		case 2:
			return string(utf16.Decode(result)), nil
		}
		if enlargeIn == 0 {
			enlargeIn = 1 << numBits
			numBits++
		}
		var entry []uint16
		if e, ok := dictionary[cc]; ok {
			entry = e
		} else if cc == dictSize {
			entry = append(append([]uint16{}, w...), w[0])
		} else {
			return "", errors.New("lzstring: kode tidak valid")
		}
		result = append(result, entry...)
		ne := append(append([]uint16{}, w...), entry[0])
		dictionary[dictSize] = ne
		dictSize++
		enlargeIn--
		w = entry
		if enlargeIn == 0 {
			enlargeIn = 1 << numBits
			numBits++
		}
	}
}

// CompressToEncodedURIComponent setara LZString.compressToEncodedURIComponent (dipakai server tiruan).
func CompressToEncodedURIComponent(input string) string {
	return compress(utf16.Encode([]rune(input)), 6, func(i int) byte { return keyURI[i] })
}

func compress(un []uint16, bitsPerChar int, getChar func(int) byte) string {
	dictionary := map[string]int{}
	created := map[string]bool{}
	var w string
	enlargeIn, dictSize, numBits := 2, 3, 2
	var data []byte
	dataVal, dataPos := 0, 0

	writeBit := func(bit int) {
		dataVal = (dataVal << 1) | bit
		if dataPos == bitsPerChar-1 {
			dataPos = 0
			data = append(data, getChar(dataVal))
			dataVal = 0
		} else {
			dataPos++
		}
	}
	writeBits := func(n, value int) {
		for i := 0; i < n; i++ {
			writeBit(value & 1)
			value >>= 1
		}
	}
	key := func(s []uint16) string { return string(utf16ToBytes(s)) }
	emitW := func() {
		if created[w] {
			cu := bytesToUTF16([]byte(w))[0]
			if cu < 256 {
				writeBits(numBits, 0)
				writeBits(8, int(cu))
			} else {
				writeBits(numBits, 1)
				writeBits(16, int(cu))
			}
			enlargeIn--
			if enlargeIn == 0 {
				enlargeIn = 1 << numBits
				numBits++
			}
			delete(created, w)
		} else {
			writeBits(numBits, dictionary[w])
		}
		enlargeIn--
		if enlargeIn == 0 {
			enlargeIn = 1 << numBits
			numBits++
		}
	}
	for i := 0; i < len(un); i++ {
		c := key(un[i : i+1])
		if _, ok := dictionary[c]; !ok {
			dictionary[c] = dictSize
			dictSize++
			created[c] = true
		}
		wc := w + c
		if _, ok := dictionary[wc]; ok {
			w = wc
		} else {
			emitW()
			dictionary[wc] = dictSize
			dictSize++
			w = c
		}
	}
	if w != "" {
		emitW()
	}
	writeBits(numBits, 2)
	for {
		dataVal <<= 1
		if dataPos == bitsPerChar-1 {
			data = append(data, getChar(dataVal))
			break
		}
		dataPos++
	}
	return string(data)
}

func utf16ToBytes(s []uint16) []byte {
	b := make([]byte, 0, len(s)*2)
	for _, u := range s {
		b = append(b, byte(u>>8), byte(u))
	}
	return b
}

func bytesToUTF16(b []byte) []uint16 {
	out := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		out = append(out, uint16(b[i])<<8|uint16(b[i+1]))
	}
	return out
}
