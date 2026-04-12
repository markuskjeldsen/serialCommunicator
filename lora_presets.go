package main

import (
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

func (sc *SerialConnection) ApplyPreset(preset LoRaPreset) {
	if sc.port == nil || !sc.connected {
		return
	}

	if !sc.enterATModeWithResponse() {
		return // Failed to enter AT mode
	}

	for _, cmd := range preset.ATCommands {
		if !sc.sendATCommandWithResponse(cmd) {
			break // Stop if a command fails
		}
	}

	sc.exitATModeWithResponse()
}

func (sc *SerialConnection) enterATModeWithResponse() bool {
	if sc.port == nil || !sc.connected {
		return false
	}

	sc.SendMessage("+++" + "\r\n") // Send with CR/LF
	response, err := sc.ReadResponse(2 * time.Second)
	if err != nil {
		log.Printf("Error entering AT mode: %v", err)
		return false
	}
	if strings.Contains(response, "Entry AT") {
		return true
	}
	log.Printf("Unexpected response entering AT mode: %s", response)
	return false
}

func (sc *SerialConnection) sendATCommandWithResponse(cmd string) bool {
	if sc.port == nil || !sc.connected {
		return false
	}

	sc.SendMessage(cmd + "\r\n")
	response, err := sc.ReadResponse(1 * time.Second)
	if err != nil {
		log.Printf("Error sending AT command %s: %v", cmd, err)
		return false
	}
	if strings.Contains(response, "OK") {
		return true
	}
	log.Printf("Unexpected response for command %s: %s", cmd, response)
	return false
}

func (sc *SerialConnection) exitATModeWithResponse() {
	if sc.port == nil || !sc.connected {
		return
	}

	sc.SendMessage("+++" + "\r\n") // Send with CR/LF
	// No specific response expected for exit, just wait a bit
	time.Sleep(500 * time.Millisecond)
}
