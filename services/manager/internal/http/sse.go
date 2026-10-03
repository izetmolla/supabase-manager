package httpapi

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v3"
)

func sseHeaders(c fiber.Ctx) {
	c.Set(fiber.HeaderContentType, "text/event-stream")
	c.Set(fiber.HeaderCacheControl, "no-cache")
	c.Set(fiber.HeaderConnection, "keep-alive")
	c.Set("X-Accel-Buffering", "no")
}

// writeEvent writes one SSE event and flushes. A flush error means the client left.
func writeEvent(w *bufio.Writer, event string, data any) error {
	var payload string
	switch v := data.(type) {
	case string:
		payload = v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		payload = string(b)
	}
	if event != "" {
		_, _ = fmt.Fprintf(w, "event: %s\n", event)
	}
	for line := range strings.SplitSeq(payload, "\n") {
		_, _ = fmt.Fprintf(w, "data: %s\n", line)
	}
	_, _ = w.WriteString("\n")
	return w.Flush()
}

func writePing(w *bufio.Writer) error {
	_, _ = w.WriteString(": ping\n\n")
	return w.Flush()
}
