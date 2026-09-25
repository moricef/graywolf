package beacon

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

const (
	maxWxNowBytes        = 64 * 1024
	maxWxNowWeatherBytes = 236 // 20-byte positioned-weather prefix + AX.25's 256-byte info field
)

// ReadWxNow reads the conventional two-line WxNow.txt interchange format.
// The timestamp is checked for shape but deliberately not used as the APRS
// timestamp: a positioned weather report carries the station position and the
// complete weather appendix from line two.
func ReadWxNow(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open WxNow.txt: %w", err)
	}
	defer f.Close()

	raw, err := io.ReadAll(io.LimitReader(f, maxWxNowBytes+1))
	if err != nil {
		return "", fmt.Errorf("read WxNow.txt: %w", err)
	}
	if len(raw) > maxWxNowBytes {
		return "", fmt.Errorf("WxNow.txt exceeds %d bytes", maxWxNowBytes)
	}

	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	var lines []string
	for _, line := range strings.Split(strings.TrimPrefix(text, "\ufeff"), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) < 2 {
		return "", fmt.Errorf("WxNow.txt must contain a timestamp line and an APRS weather line")
	}
	timestamp := strings.TrimSpace(lines[0])
	if _, err := time.ParseInLocation("Jan 02 2006 15:04", timestamp, time.Local); err != nil {
		return "", fmt.Errorf("invalid WxNow.txt timestamp %q: %w", lines[0], err)
	}

	weather := lines[1]
	if len(weather) > maxWxNowWeatherBytes {
		return "", fmt.Errorf("WxNow.txt weather line exceeds %d bytes", maxWxNowWeatherBytes)
	}
	for _, c := range []byte(weather) {
		if c < 0x20 || c > 0x7e {
			return "", fmt.Errorf("WxNow.txt weather line contains a non-printable byte")
		}
	}
	decoded, err := aprs.ParseInfo([]byte("_01010000" + weather))
	if err != nil || decoded.Type != aprs.PacketWeather || decoded.Weather == nil {
		return "", fmt.Errorf("WxNow.txt line 2 is not a complete APRS weather report")
	}
	return weather, nil
}
