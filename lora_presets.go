package main

import (
	"fmt"
	"log"
	"strings"
	"time"
)

type LoRaPreset struct {
	Name            string
	Description     string
	SpreadingFactor int
	Bandwidth       int
	CodingRate      int
	TxPower         int
	ATCommands      []string
}

var presets = map[string]LoRaPreset{
	"short_fast": {
		Name:        "Short Range & Fast (L7)",
		Description: "SF 5, 125kHz, 13020 bit/s",
		ATCommands: []string{
			"AT+LEVEL7",
		},
	},
	"long_slow": {
		Name:        "Long Range & Slow (L0)",
		Description: "SF 12, 125kHz, 244 bit/s",
		ATCommands: []string{
			"AT+LEVEL0",
		},
	},
}

func GetPreset(name string) (LoRaPreset, bool) {
	preset, ok := presets[name]
	return preset, ok
}

func GetPresetNames() []string {
	names := make([]string, 0, len(presets))
	for name := range presets {
		names = append(names, name)
	}
	return names
}

// ResetToDataMode ensures the module is in data mode regardless of its current
// state. It sends +++ and checks the response:
//   - "Entry AT" → module was in data mode, just entered AT mode → send +++ again to exit.
//   - "Exit AT"  → module was already in AT mode and just exited → drain "Power on".
//   - anything else → best-effort drain and continue.
func (sc *SerialConnection) ResetToDataMode() {
	if sc.port == nil || !sc.connected {
		return
	}

	sc.atMode.Store(true)
	defer sc.atMode.Store(false)

	sc.SendMessage("+++\r\n")
	line, err := sc.ReadResponse(2 * time.Second)
	if err != nil {
		// No response — assume already in data mode.
		return
	}

	switch {
	case strings.Contains(line, "Entry AT"):
		// We just entered AT mode; exit it cleanly.
		sc.exitATModeWithResponse()
	case strings.Contains(line, "Exit AT"):
		// Module was in AT mode and just exited; drain the "Power on" line.
		sc.DrainResponses(600 * time.Millisecond)
	default:
		sc.DrainResponses(400 * time.Millisecond)
	}
}

func (sc *SerialConnection) ApplyPreset(preset LoRaPreset) {
	if sc.port == nil || !sc.connected {
		return
	}

	// Enable AT mode: readLoop will route all received lines to responseReady
	// so they are consumed by the synchronous helpers below and never leak
	// into the chat channel.
	sc.atMode.Store(true)

	if !sc.enterATModeWithResponse() {
		sc.atMode.Store(false)
		return
	}

	for _, cmd := range preset.ATCommands {
		if !sc.sendATCommandWithResponse(cmd) {
			break
		}
	}

	sc.exitATModeWithResponse()

	// Clear AT mode only after all expected responses have been consumed.
	sc.atMode.Store(false)
}

// readLines reads successive lines from responseReady until stopFn returns true
// for a line, or until perLineTimeout elapses between lines.
func (sc *SerialConnection) readLines(perLineTimeout time.Duration, stopFn func(line string) bool) ([]string, error) {
	var lines []string
	for {
		line, err := sc.ReadResponse(perLineTimeout)
		if err != nil {
			return lines, err
		}
		lines = append(lines, line)
		if stopFn(line) {
			return lines, nil
		}
	}
}

func (sc *SerialConnection) enterATModeWithResponse() bool {
	if sc.port == nil || !sc.connected {
		return false
	}

	sc.SendMessage("+++\r\n")
	response, err := sc.ReadResponse(2 * time.Second)
	if err != nil {
		log.Printf("Error entering AT mode: %v", err)
		return false
	}
	if strings.Contains(response, "Entry AT") {
		return true
	}
	log.Printf("Unexpected response entering AT mode: %q", response)
	return false
}

// sendATCommandWithResponse sends a single AT command and reads lines until it
// sees "OK" or "ERROR=<code>". This correctly handles both single-line responses
// ("OK") and multi-line responses like AT+HELP which terminates with "OK" or a
// closing delimiter followed eventually by OK/ERROR.
func (sc *SerialConnection) sendATCommandWithResponse(cmd string) bool {
	if sc.port == nil || !sc.connected {
		return false
	}

	sc.SendMessage(cmd + "\r\n")

	lines, err := sc.readLines(1*time.Second, func(line string) bool {
		return line == "OK" || strings.HasPrefix(line, "ERROR=")
	})
	if err != nil {
		log.Printf("Timeout reading response for %s (got %d lines): %v", cmd, len(lines), err)
		return false
	}

	terminal := lines[len(lines)-1]
	if terminal == "OK" {
		return true
	}
	if strings.HasPrefix(terminal, "ERROR=") {
		log.Printf("Module returned error for command %s: %s", cmd, terminal)
		return false
	}
	log.Printf("Unexpected terminal line for command %s: %q", cmd, terminal)
	return false
}

// QueryLevel asks the module for the current LEVEL setting and returns the
// value string (e.g. "7" or "0"). Must be called while atMode is active.
func (sc *SerialConnection) QueryLevel() (string, error) {
	if sc.port == nil || !sc.connected {
		return "", fmt.Errorf("not connected")
	}

	sc.SendMessage("AT+LEVEL\r\n")
	response, err := sc.ReadResponse(1 * time.Second)
	if err != nil {
		return "", fmt.Errorf("AT+LEVEL query timeout: %w", err)
	}
	// Response format: "+LEVEL=<n>"
	if strings.HasPrefix(response, "+LEVEL=") {
		return strings.TrimPrefix(response, "+LEVEL="), nil
	}
	if strings.HasPrefix(response, "ERROR=") {
		return "", fmt.Errorf("module error: %s", response)
	}
	return "", fmt.Errorf("unexpected response to AT+LEVEL: %q", response)
}

// exitATModeWithResponse sends +++ to leave AT mode and drains all lines the
// module emits, while atMode is still true, so nothing leaks into the chat
// channel.
//
// The DX-LR03 has two observed exit responses:
//
//	Normal: "Exit AT\r\n"  (~320 ms)  then  "Power on\r\n"  (~687 ms)
//	Quirk:  "ERROR=102\r\n" — module signals it is already in data mode;
//	        no further lines follow. Treated as a successful exit.
func (sc *SerialConnection) exitATModeWithResponse() {
	if sc.port == nil || !sc.connected {
		return
	}

	sc.SendMessage("+++\r\n")

	// Read up to 4 lines; stop as soon as we hit silence or have consumed all
	// expected lines. Per-line timeouts are generous enough to cover the ~687 ms
	// "Power on" line without blocking longer than necessary on silence.
	sawExit := false
	for i := 0; i < 4; i++ {
		timeout := 900 * time.Millisecond
		if i > 0 {
			timeout = 500 * time.Millisecond
		}
		line, err := sc.ReadResponse(timeout)
		if err != nil {
			break // silence — done
		}
		switch {
		case strings.Contains(line, "Exit AT"), strings.Contains(line, "Power on"):
			sawExit = true
		case line == "ERROR=102":
			// Firmware quirk: module responds with ERROR=102 first, then may
			// still send "Exit AT" and "Power on" — drain them before returning.
			sawExit = true
			sc.DrainResponses(900 * time.Millisecond)
			return
		default:
			log.Printf("exitATMode: unexpected line: %q", line)
		}
	}

	if !sawExit {
		log.Printf("exitATMode: no exit confirmation received")
	}
}
