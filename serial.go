package main

import (
	"fmt"
	"io"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/tarm/serial"
)

type SerialConnection struct {
	port          *serial.Port
	config        *serial.Config
	connected     bool
	atMode        atomic.Bool   // true while an AT command sequence is in progress
	incoming      chan string   // chat messages from the module (data mode)
	outgoing      chan string   // data to send to the module
	responseReady chan string   // synchronous AT responses
	done          chan struct{} // closed by Disconnect to stop goroutines
}

func NewSerialConnection(portName string, baudRate int) *SerialConnection {
	return &SerialConnection{
		config: &serial.Config{
			Name:        portName,
			Baud:        baudRate,
			ReadTimeout: time.Millisecond * 500,
		},
		incoming:      make(chan string, 100),
		outgoing:      make(chan string, 100),
		responseReady: make(chan string, 10), // larger buffer: AT sequences can produce several lines
		done:          make(chan struct{}),
		connected:     false,
	}
}

func (sc *SerialConnection) Connect() error {
	port, err := serial.OpenPort(sc.config)
	if err != nil {
		return fmt.Errorf("serial.OpenPort failed: %v", err)
	}

	sc.port = port
	sc.connected = true

	go sc.readLoop()
	go sc.writeLoop()

	return nil
}

func (sc *SerialConnection) Disconnect() error {
	if sc.port != nil {
		sc.connected = false
		close(sc.done) // signal goroutines to stop
		return sc.port.Close()
	}
	return nil
}

func (sc *SerialConnection) readLoop() {
	buf := make([]byte, 1024)
	var messageBuffer []byte

	for sc.connected {
		n, err := sc.port.Read(buf)
		if err != nil {
			if err != io.EOF && !strings.Contains(err.Error(), "timeout") {
				log.Printf("Serial read error: %v", err)
			}
			select {
			case <-sc.done:
				return
			default:
				time.Sleep(50 * time.Millisecond)
				continue
			}
		}

		if n > 0 {
			received := buf[:n]

			for i := 0; i < n; i++ {
				if received[i] == '\n' || received[i] == '\r' {
					if len(messageBuffer) > 0 {
						message := string(messageBuffer)
						sc.routeMessage(message)
						messageBuffer = nil
					}
				} else {
					messageBuffer = append(messageBuffer, received[i])
				}
			}

			// If the buffer grows very large without a line terminator, flush it.
			if len(messageBuffer) > 1000 {
				sc.routeMessage(string(messageBuffer))
				messageBuffer = nil
			}
		}
	}
}

// routeMessage decides where a received line goes:
//   - During an AT command sequence (atMode == true) → responseReady channel
//     so the caller's ReadResponse() gets it.
//   - In data (chat) mode                            → incoming channel
//     so the TUI can display it.
//
// In AT mode we block until the message is consumed; this prevents responses
// from racing into the chat channel when no reader is on responseReady yet.
func (sc *SerialConnection) routeMessage(message string) {
	if sc.atMode.Load() {
		select {
		case sc.responseReady <- message:
		case <-sc.done:
		}
	} else {
		select {
		case sc.incoming <- message:
		case <-sc.done:
		default:
			log.Println("Incoming channel full, message dropped")
		}
	}
}

// ReadResponse waits for the next line from the module within timeout.
// Must only be called while atMode is true (i.e. inside an AT command sequence).
func (sc *SerialConnection) ReadResponse(timeout time.Duration) (string, error) {
	select {
	case response := <-sc.responseReady:
		return response, nil
	case <-time.After(timeout):
		return "", fmt.Errorf("response timeout after %v", timeout)
	}
}

// DrainResponses discards all lines that arrive on responseReady within deadline.
// Use this to consume trailing module output (e.g. "Power on" after "Exit AT")
// so nothing leaks into the chat channel once atMode is cleared.
func (sc *SerialConnection) DrainResponses(deadline time.Duration) {
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	for {
		select {
		case <-sc.responseReady:
			// discard
		case <-timer.C:
			return
		}
	}
}

func (sc *SerialConnection) writeLoop() {
	for {
		select {
		case msg, ok := <-sc.outgoing:
			if !ok {
				return
			}
			if sc.port != nil {
				if _, err := sc.port.Write([]byte(msg)); err != nil {
					log.Printf("Serial write error: %v", err)
				}
			}
		case <-sc.done:
			return
		}
	}
}

func (sc *SerialConnection) SendMessage(message string) {
	select {
	case sc.outgoing <- message:
	case <-sc.done:
	default:
		log.Println("Outgoing channel full, message dropped")
	}
}

func (sc *SerialConnection) GetIncoming() <-chan string {
	return sc.incoming
}

func (sc *SerialConnection) IsConnected() bool {
	return sc.connected
}

func (sc *SerialConnection) UpdateConfig(portName string, baudRate int) {
	sc.config.Name = portName
	sc.config.Baud = baudRate
}

func (sc *SerialConnection) SendChatMessage(message string) {
	if sc.port == nil || !sc.connected {
		return
	}
	sc.SendMessage(message + "\r\n")
}
