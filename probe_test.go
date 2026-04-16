//go:build probe

// Run with:   go test -tags probe -v -run TestDXLR03
//
// Requires the DX-LR03 module physically connected at /dev/ttyUSB0 at 9600 baud.
// Gated behind the "probe" build tag so it never runs during normal `go test ./...`.
package main

import (
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/tarm/serial"
)

const (
	probePort     = "/dev/ttyUSB0"
	probeBaudRate = 9600
)

// ─────────────────────────────────────────────────────────────────────────────
// low-level helpers
// ─────────────────────────────────────────────────────────────────────────────

func probeOpenPort(t *testing.T) *serial.Port {
	t.Helper()
	c := &serial.Config{
		Name:        probePort,
		Baud:        probeBaudRate,
		ReadTimeout: 200 * time.Millisecond,
	}
	p, err := serial.OpenPort(c)
	if err != nil {
		t.Fatalf("cannot open serial port %s: %v", probePort, err)
	}
	return p
}

func readAllRaw(p *serial.Port, deadline time.Duration) []byte {
	var raw []byte
	buf := make([]byte, 256)
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		n, err := p.Read(buf)
		if n > 0 {
			raw = append(raw, buf[:n]...)
		}
		if err != nil && err != io.EOF && !strings.Contains(err.Error(), "timeout") {
			break
		}
	}
	return raw
}

func hexDump(raw []byte) string {
	var sb strings.Builder
	for i, b := range raw {
		if i > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(fmt.Sprintf("%02X", b))
	}
	return sb.String()
}

func sendExpect(t *testing.T, p *serial.Port, send string, wantSubstr string, wait time.Duration) []byte {
	t.Helper()
	if _, err := p.Write([]byte(send)); err != nil {
		t.Fatalf("write %q: %v", send, err)
	}
	raw := readAllRaw(p, wait)
	resp := string(raw)
	t.Logf("  send %-20q  recv %q  (hex: %s)",
		send,
		strings.NewReplacer("\r", "\\r", "\n", "\\n").Replace(resp),
		hexDump(raw))
	if !strings.Contains(resp, wantSubstr) {
		t.Errorf("expected response to contain %q, got %q", wantSubstr, resp)
	}
	return raw
}

// ─────────────────────────────────────────────────────────────────────────────
// Pass 1: raw protocol — every byte verified against the real hardware
// ─────────────────────────────────────────────────────────────────────────────

func TestDXLR03_RawProtocol(t *testing.T) {
	p := probeOpenPort(t)
	defer p.Close()

	readAllRaw(p, 400*time.Millisecond) // drain residual

	t.Run("EnterATMode", func(t *testing.T) {
		sendExpect(t, p, "+++\r\n", "Entry AT", 2*time.Second)
	})

	t.Run("BarePing", func(t *testing.T) {
		sendExpect(t, p, "AT\r\n", "OK", 1*time.Second)
	})

	t.Run("HelpReturnsMultilineBlock", func(t *testing.T) {
		// AT+HELP returns ~16 lines terminated by a "===" line; verify key fields.
		raw := sendExpect(t, p, "AT+HELP\r\n", "LoRa Parameter", 3*time.Second)
		resp := string(raw)
		for _, want := range []string{"+VERSION=", "LEVEL:", "Frequency:", "Bandwidth:", "Spreading Factor:"} {
			if !strings.Contains(resp, want) {
				t.Errorf("AT+HELP response missing %q", want)
			}
		}
		t.Logf("  AT+HELP full response:\n%s", resp)
	})

	t.Run("QueryLevelFormat", func(t *testing.T) {
		// AT+LEVEL (no digit) returns "+LEVEL=<n>"
		raw := sendExpect(t, p, "AT+LEVEL\r\n", "+LEVEL=", 1*time.Second)
		t.Logf("  AT+LEVEL raw: %q  hex: %s", string(raw), hexDump(raw))
	})

	t.Run("SetLevel7AndConfirm", func(t *testing.T) {
		sendExpect(t, p, "AT+LEVEL7\r\n", "OK", 1*time.Second)
		sendExpect(t, p, "AT+LEVEL\r\n", "+LEVEL=7", 1*time.Second)
	})

	t.Run("SetLevel0AndConfirm", func(t *testing.T) {
		sendExpect(t, p, "AT+LEVEL0\r\n", "OK", 1*time.Second)
		sendExpect(t, p, "AT+LEVEL\r\n", "+LEVEL=0", 1*time.Second)
	})

	t.Run("UnknownCommandReturnsError", func(t *testing.T) {
		if _, err := p.Write([]byte("AT+VER\r\n")); err != nil {
			t.Fatal(err)
		}
		resp := string(readAllRaw(p, 1*time.Second))
		t.Logf("  send AT+VER  recv %q", resp)
		if !strings.HasPrefix(resp, "ERROR=") {
			t.Errorf("expected ERROR=<code> for unsupported command, got %q", resp)
		}
	})

	t.Run("ExitATMode", func(t *testing.T) {
		raw := sendExpect(t, p, "+++\r\n", "Exit AT", 1*time.Second)
		combined := string(raw)
		if !strings.Contains(combined, "Power on") {
			extra := string(readAllRaw(p, 800*time.Millisecond))
			t.Logf("  extra after exit: %q", extra)
			combined += extra
		}
		if !strings.Contains(combined, "Power on") {
			t.Errorf(`expected "Power on" after "Exit AT", got %q`, combined)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Pass 2: application stack — SerialConnection + ApplyPreset + QueryLevel
// ─────────────────────────────────────────────────────────────────────────────

func TestDXLR03_ApplicationStack(t *testing.T) {
	sc := NewSerialConnection(probePort, probeBaudRate)
	if err := sc.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer sc.Disconnect() //nolint:errcheck

	// Ensure the module is in data mode before any test, regardless of what
	// previous test runs may have left behind.
	sc.ResetToDataMode()
	time.Sleep(200 * time.Millisecond)

	// applyChecked mirrors ApplyPreset but asserts each step individually.
	applyChecked := func(t *testing.T, key string) {
		t.Helper()
		preset, ok := GetPreset(key)
		if !ok {
			t.Fatalf("preset %q not defined", key)
		}
		sc.atMode.Store(true)
		if !sc.enterATModeWithResponse() {
			sc.atMode.Store(false)
			t.Fatal("enterATModeWithResponse returned false")
		}
		for _, cmd := range preset.ATCommands {
			if !sc.sendATCommandWithResponse(cmd) {
				sc.exitATModeWithResponse()
				sc.atMode.Store(false)
				t.Fatalf("sendATCommandWithResponse(%q) returned false", cmd)
			}
		}
		sc.exitATModeWithResponse()
		sc.atMode.Store(false)
	}

	t.Run("ApplyPreset_ShortFast", func(t *testing.T) {
		applyChecked(t, "short_fast")
	})

	time.Sleep(300 * time.Millisecond)

	t.Run("ApplyPreset_LongSlow", func(t *testing.T) {
		applyChecked(t, "long_slow")
	})

	time.Sleep(300 * time.Millisecond)

	t.Run("QueryLevel_ConfirmsLongSlow", func(t *testing.T) {
		sc.atMode.Store(true)
		if !sc.enterATModeWithResponse() {
			sc.atMode.Store(false)
			t.Fatal("enterATModeWithResponse returned false")
		}
		level, err := sc.QueryLevel()
		if err != nil {
			sc.exitATModeWithResponse()
			sc.atMode.Store(false)
			t.Fatalf("QueryLevel: %v", err)
		}
		t.Logf("  current LEVEL = %q", level)
		if level != "0" {
			t.Errorf("expected LEVEL=0 after applying long_slow, got %q", level)
		}
		sc.exitATModeWithResponse()
		sc.atMode.Store(false)
	})

	time.Sleep(300 * time.Millisecond)

	t.Run("ApplyPreset_ViaPublicAPI", func(t *testing.T) {
		// Exercise the public ApplyPreset path and verify the result with QueryLevel.
		preset, _ := GetPreset("short_fast")
		sc.ApplyPreset(preset)

		sc.atMode.Store(true)
		if !sc.enterATModeWithResponse() {
			sc.atMode.Store(false)
			t.Fatal("enterATModeWithResponse returned false")
		}
		level, err := sc.QueryLevel()
		sc.exitATModeWithResponse()
		sc.atMode.Store(false)
		if err != nil {
			t.Fatalf("QueryLevel after ApplyPreset: %v", err)
		}
		t.Logf("  LEVEL after ApplyPreset(short_fast) = %q", level)
		if level != "7" {
			t.Errorf("expected LEVEL=7 after short_fast, got %q", level)
		}
	})

	t.Run("NoChatLeakage", func(t *testing.T) {
		// After all the above AT sequences the chat channel must be empty.
		time.Sleep(200 * time.Millisecond)
		var leaked []string
	drain:
		for {
			select {
			case msg := <-sc.GetIncoming():
				leaked = append(leaked, msg)
			default:
				break drain
			}
		}
		if len(leaked) > 0 {
			t.Errorf("AT responses leaked into chat channel: %v", leaked)
		}
	})
}
