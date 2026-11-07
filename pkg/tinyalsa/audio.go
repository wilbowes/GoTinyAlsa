package tinyalsa

import (
	"bufio"
	"bytes"
	"errors"
	"github.com/Binozo/GoTinyAlsa/internal/tinyapi"
	"github.com/Binozo/GoTinyAlsa/pkg/pcm"
	"io"
	"time"
)

const PCM_IN = tinyapi.PCM_IN
const PCM_OUT = tinyapi.PCM_OUT
const PCM_FORMAT_S16_LE = tinyapi.PCM_FORMAT_S16_LE
const PCM_FORMAT_S16_BE = tinyapi.PCM_FORMAT_S16_BE
const PCM_FORMAT_S24_LE = tinyapi.PCM_FORMAT_S24_LE
const PCM_FORMAT_S24_BE = tinyapi.PCM_FORMAT_S24_BE
const PCM_FORMAT_S24_3BE = tinyapi.PCM_FORMAT_S24_3BE
const PCM_FORMAT_S24_3LE = tinyapi.PCM_FORMAT_S24_3LE
const PCM_FORMAT_S32_LE = tinyapi.PCM_FORMAT_S32_LE
const PCM_FORMAT_S32_BE = tinyapi.PCM_FORMAT_S32_BE
const ErrorTolerance = 10 // defines how many error frames are allowed to be read without stopping reading the next ones

// GetAudioStream listens to the input of the defined device.
// Each delivered slice owns its samples and may be retained by the receiver.
func (d *AlsaDevice) GetAudioStream(config pcm.Config, audioData chan []byte) error {
	pcmDevice, err := tinyapi.PcmOpen(d.Card, d.Device, PCM_IN, config)
	if err != nil {
		return err
	}
	defer pcmDevice.Close()
	size := pcmDevice.FrameBytesSize()
	buffer := make([]byte, size)
	if err != nil {
		return err
	}

	// Recover a send on a closed channel. Registered ONCE, outside the read
	// loop: a defer inside an infinite loop never executes but pushes a new
	// _defer record + closure every iteration — on a stream that runs for the
	// process lifetime that retained ~46 bytes per period forever (~1MB/h at
	// 160ms batches; the EchoMuse fleet heap leak, 2026-07-18).
	defer func() {
		if r := recover(); r != nil {
			// Channel is probably closed!
		}
	}()

	errorCount := 0
	writeTimeout := time.Second * 5
FrameReader:
	for {
		err := pcmDevice.ReadFrames(buffer, size)
		if err != nil {
			if errorCount > ErrorTolerance {
				return err
			}
			errorCount += 1
			continue
		}
		select {
		// Snapshot before sending: the next PCM read reuses buffer.
		case audioData <- bytes.Clone(buffer):
			// Successfully sent audio data back to the api
			continue
		case <-time.After(writeTimeout):
			// Chan got closed, we need to close too
			break FrameReader
		}
	}

	return nil
}

// GetAudioStreamChunked is GetAudioStream reading readFrames per pcm_read
// and delivering deliverFrames per slice (a multiple of readFrames).
//
// GetAudioStream reads the whole ring buffer per call, so a read can only
// complete once the ring is full: there is no headroom, and a reader that is
// a moment late is lapped by the hardware. On MediaTek's memif that showed as
// whole stretches of stale audio repeated with no error (pcm_read restarts on
// EPIPE and carries on), which is harmless for a microphone feeding a wake
// word and fatal for an echo reference that must stay aligned (EchoMuse,
// donut 41C8, 2026-11-07). Reading a period at a time leaves the rest of the
// ring as slack; delivery stays in the caller's batch size.
func (d *AlsaDevice) GetAudioStreamChunked(config pcm.Config, readFrames, deliverFrames int, audioData chan []byte) error {
	pcmDevice, err := tinyapi.PcmOpen(d.Card, d.Device, PCM_IN, config)
	if err != nil {
		return err
	}
	defer pcmDevice.Close()
	bpf := pcmDevice.BytesPerFrame()
	if readFrames <= 0 || deliverFrames < readFrames || deliverFrames%readFrames != 0 {
		return errors.New("deliverFrames must be a positive multiple of readFrames")
	}
	chunk := make([]byte, readFrames*bpf)
	out := make([]byte, 0, deliverFrames*bpf)

	defer func() {
		if r := recover(); r != nil {
			// Channel is probably closed!
		}
	}()

	errorCount := 0
	writeTimeout := time.Second * 5
	for {
		if err := pcmDevice.ReadFrames(chunk, len(chunk)); err != nil {
			if errorCount > ErrorTolerance {
				return err
			}
			errorCount += 1
			continue
		}
		out = append(out, chunk...)
		if len(out) < cap(out) {
			continue
		}
		select {
		case audioData <- out:
			out = make([]byte, 0, deliverFrames*bpf)
		case <-time.After(writeTimeout):
			return nil
		}
	}
}

// SendAudioStream plays the given audio data (usually wav) to the device
func (d *AlsaDevice) SendAudioStream(audioData []byte) error {
	pcmDevice, err := tinyapi.PcmOpen(d.Card, d.Device, PCM_OUT, d.DeviceConfig)
	if err != nil {
		return err
	}
	defer pcmDevice.Close()
	size := pcmDevice.FrameBytesSize()
	buffer := make([]byte, size)

	fullBytes := new(bytes.Buffer)
	fullBytes.Write(audioData)
	reader := bufio.NewReader(fullBytes)

	for {
		n, err := reader.Read(buffer)
		if err != nil {
			return err
		}
		err = pcmDevice.WriteFrames(buffer, n)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil // Completed
			}
			return err
		}
	}
}
