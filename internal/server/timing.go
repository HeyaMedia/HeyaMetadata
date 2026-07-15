package server

import (
	"fmt"
	"time"
)

func serverTiming(name string, duration time.Duration) string {
	return fmt.Sprintf("%s;dur=%.3f", name, float64(duration.Microseconds())/1000)
}
