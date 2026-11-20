package pcm

import "github.com/Binozo/GoTinyAlsa/internal/tinypcm"

type Config struct {
	// The number of audio channels
	Channels int
	// Sample rate (the higher, the better)
	SampleRate int
	// Number of frames in a period
	PeriodSize int
	// Number of periods
	PeriodCount int
	// Audiocodec (e.g. tinyalsa.PCM_FORMAT_S24_LE)
	Format           tinypcm.Format
	StartThreshold   int
	StopThreshold    int
	SilenceThreshold int
	// AvailMin is GetAudioStreamChunked's read margin: each read waits until
	// this many frames are ready, so the reader stays behind the hardware
	// pointer. Not passed to pcm_config: a blocking read ignores the kernel's
	// avail_min, and the field's offset depends on the device's tinyalsa.
	AvailMin int
}
