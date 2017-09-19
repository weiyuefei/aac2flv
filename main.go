package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

func main() {
	aac := NewAACReader("JustOneLastDance.aac")
	defer aac.Cleanup()

	flv := NewFLV("JustOneLastDance.flv")
	defer flv.Cleanup()

	// scriptdata tag is not must
	flv.WriteHeader(Audio)
	flv.WriteTag(AudioTag, aac.AudioSpecificConfig(), 0)

	for {
		ts := aac.GetTimestampOfFrame()
		audioData, err := aac.RawDataOfFrame()
		if err != nil {
			if err == io.EOF {
				fmt.Println("Read file END. AAC transfer to FLV success")
				break
			}
			fmt.Println("Read file ERROR")
			return
		}
		flv.WriteTag(AudioTag, audioData, ts)
	}
}

type FLV struct {
	fp *os.File
	w  *bufio.Writer
}

func NewFLV(filename string) *FLV {
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil
	}

	bw := bufio.NewWriter(f)
	if bw == nil {
		return nil
	}

	return &FLV{fp: f, w: bw}
}

type WhatMedia int

const (
	Audio      WhatMedia = 1 << iota
	Video      WhatMedia = 1 << iota
	AudioVideo WhatMedia = 1 << iota
)

func (flv *FLV) WriteHeader(has WhatMedia) {
	flv.w.WriteString("FLV")
	flv.w.WriteByte(0x01)
	if has|Audio == 1 {
		flv.w.WriteByte(0x04)
	} else if has|Video == 1 {
		flv.w.WriteByte(0x01)
	} else {
		flv.w.WriteByte(0x05)
	}

	u32 := make([]byte, 4)
	binary.BigEndian.PutUint32(u32, 9)
	flv.w.Write(u32)

	binary.BigEndian.PutUint32(u32, 0)
	flv.w.Write(u32)
}

type TagType int

const (
	AudioTag  TagType = 0x08
	VideoTag  TagType = 0x09
	ScriptTag TagType = 0x12
)

func (flv *FLV) writeTagHeader(t TagType, dataSize int, ts uint32) {
	flv.w.WriteByte(byte(t))
	flv.w.WriteByte(byte((dataSize >> 16) & 0xff))
	flv.w.WriteByte(byte((dataSize >> 8) & 0xff))
	flv.w.WriteByte(byte(dataSize & 0xff))

	tsLow24 := ts & 0xffffff
	tsHigh8 := (ts >> 24) & 0xff
	flv.w.WriteByte(byte((tsLow24 >> 16) & 0xff))
	flv.w.WriteByte(byte((tsLow24 >> 8) & 0xff))
	flv.w.WriteByte(byte(tsLow24 & 0xff))
	flv.w.WriteByte(byte(tsHigh8))

	u24 := make([]byte, 3)
	flv.w.Write(u24)
}

func (flv *FLV) WriteTag(t TagType, d []byte, ts uint32) {
	flv.writeTagHeader(t, len(d), ts)
	flv.w.Write(d)
	u32 := make([]byte, 4)
	binary.BigEndian.PutUint32(u32, 11+uint32(len(d)))
	flv.w.Write(u32)
}

func (flv *FLV) Cleanup() {
	flv.w.Flush()
	flv.fp.Close()
}

type AACReader struct {
	fp *os.File
	r  *bufio.Reader
	ts uint32
}

func NewAACReader(filename string) *AACReader {
	f, err := os.OpenFile(filename, os.O_RDONLY, 0644)
	if err != nil {
		return nil
	}

	br := bufio.NewReader(f)
	if br == nil {
		return nil
	}

	return &AACReader{fp: f, r: br, ts: 0}
}

type AudioASC struct {
	audioObjectType        byte // 5bits
	samplingFrequencyIndex byte // 4bits
	channelConfiguration   byte // 4 bits
	frameLengthFlag        byte // 1bit
	dependsOnCoreCoder     byte // 1bit
	extensionFlag          byte // 1bit
}

const (
	LinearPCMPlatformEndian = 0
	ADPCM                   = 1
	MP3                     = 2
	LinearPCMLittleEndian   = 3
	Nellymoser16KHzMono     = 4
	Nellymoser8KHzMono      = 5
	Nellymoser              = 6
	G711ALawLogarithmicPCM  = 7
	G711MuLawLogarithmicPCM = 8
	Reserved                = 9
	AAC                     = 10
	Speex                   = 11
	MP38KHz                 = 14
	DeviceSpecificSound     = 15
)

const (
	SoundRate5500Hz  = 0
	SoundRate11000Hz = 1
	SoundRate22000Hz = 2
	SoundRate44100Hz = 3
)

const (
	SoundSize8BitSamples  = 0
	SoundSize16BitSamples = 1
)

const (
	SoundTypeMono   = 0
	SoundTypeStereo = 1
)

func (aac *AACReader) audioTagHeader() byte {
	return (byte)(AAC<<4&0xf0) |
		(byte)(SoundRate44100Hz<<2&0x0c) |
		(byte)(SoundSize16BitSamples<<1&0x02) |
		(byte)(SoundTypeStereo&0x01)
}

func (aac *AACReader) AudioSpecificConfig() []byte {
	adts, _ := aac.r.Peek(7)

	asc := bytes.NewBuffer(make([]byte, 0))

	asc.WriteByte(aac.audioTagHeader())
	asc.WriteByte(0x00)

	var part1, part2, part3 uint16

	part1 = (uint16)(adts[2] >> 6 & 0x03)
	part1 <<= 11
	part1 &= 0xf800

	part2 = (uint16)(adts[2] >> 2 & 0x0f)
	part2 <<= 7
	part2 &= 0x0780

	part3 = (uint16)(adts[2]&0x01) | (uint16)(adts[3]>>6&0x03)
	part3 <<= 3
	part3 &= 0x0078

	u16 := part1 | part2 | part3

	asc.WriteByte(byte(u16 >> 8 & 0xff))
	asc.WriteByte(byte(u16 & 0xff))

	return asc.Bytes()
}

func (aac *AACReader) parseAdtsHeader() (int, error) {
	adts := make([]byte, 0)
	// Read reads data into p.
	// It returns the number of bytes read into p.
	// It calls Read at most once on the underlying Reader,
	// hence n may be less than len(p).
	// At EOF, the count will be zero and err will be io.EOF.
	remain := 7
	for remain > 0 {
		buf := make([]byte, remain)
		n, err := aac.r.Read(buf)

		if err != nil {
			return 0, err
		}

		adts = append(adts, buf[:n]...)
		remain -= n
	}

	// FIXME Is there any gracefull coding?
	var frameLength uint16
	frameLength = uint16(adts[3] & 0x03)
	frameLength <<= 8
	frameLength |= uint16(adts[4])
	frameLength <<= 3
	frameLength |= uint16(adts[5] >> 5 & 0x07)

	numOfRawDataBlocksInFrame := uint32(adts[6] & 0x03)
	var sampleRateArray = []int{
		96000, 88200, 64000,
		48000, 44100, 32000,
		24000, 22050, 16000,
		12000, 11025, 8000, 7350,
	}

	samplingFrequencyIndex := (adts[2] >> 2 & 0x0f)
	sampleRate := sampleRateArray[samplingFrequencyIndex]

	numOfRawDataBlocksInFrame += 1
	aac.ts += (numOfRawDataBlocksInFrame * 1024 * 1000) / uint32(sampleRate)

	return int(frameLength - 7), nil
}

func (aac *AACReader) GetTimestampOfFrame() uint32 {
	return aac.ts
}

func (aac *AACReader) RawDataOfFrame() ([]byte, error) {
	RawLength, err := aac.parseAdtsHeader()
	if err != nil {
		return make([]byte, 0), err
	}

	audioData := bytes.NewBuffer(make([]byte, 0))
	audioData.WriteByte(aac.audioTagHeader())
	audioData.WriteByte(0x01)

	rawData := make([]byte, 0)
	// Read reads data into p.
	// It returns the number of bytes read into p.
	// It calls Read at most once on the underlying Reader,
	// hence n may be less than len(p).
	// At EOF, the count will be zero and err will be io.EOF.
	remain := RawLength
	for remain > 0 {
		buf := make([]byte, remain)
		n, err := aac.r.Read(buf)

		if err != nil {
			return rawData, err
		}

		rawData = append(rawData, buf[:n]...)
		remain -= n
	}

	audioData.Write(rawData)
	return audioData.Bytes(), nil
}

func (aac *AACReader) Cleanup() {
	aac.fp.Close()
}

