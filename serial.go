package main

import (
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/tarm/serial"
)

type SerialConnection struct {
	port          *serial.Port
	config        *serial.Config
	connected     bool
	incoming      chan string
	outgoing      chan string
	responseReady chan string // For synchronous responses
}

func NewSerialConnection(portName string, baudRate int) *SerialConnection {
	return &SerialConnection{
		config: &serial.Config{
			Name:        portName,
			Baud:        baudRate,
			ReadTimeout: time.Millisecond * 500, // Shorter timeout for faster response checks
		},
		incoming:      make(chan string, 100),
		outgoing:      make(chan string, 100),
		responseReady: make(chan string, 1),
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
		close(sc.incoming)
		close(sc.outgoing)
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
			time.Sleep(50 * time.Millisecond)
			continue
		}

		if n > 0 {
			received := buf[:n]

			for i := 0; i < n; i++ {
				if received[i] == '\n' || received[i] == '\r' {
					if len(messageBuffer) > 0 {
						message := string(messageBuffer)
						// Try to send to responseReady first for synchronous calls
						select {
						case sc.responseReady <- message:
						case sc.incoming <- message:
						default:
							log.Println("Incoming/response channel full, message dropped")
						}
						messageBuffer = nil
					}
				} else {
					messageBuffer = append(messageBuffer, received[i])
				}
			}

			// If buffer grows too large, consider it a message
			if len(messageBuffer) > 1000 {
				message := string(messageBuffer)
				select {
				case sc.incoming <- message:
				default:
					log.Println("Incoming channel full, message dropped")
				}
				messageBuffer = nil
			}
		}
	}
}

// ReadResponse attempts to read a single line response from the serial port
// within the given timeout. It should only be called for synchronous AT commands.
func (sc *SerialConnection) ReadResponse(timeout time.Duration) (string, error) {
	select {
	case response := <-sc.responseReady:
		return response, nil
	case <-time.After(timeout):
		return "", fmt.Errorf("response timeout after %v", timeout)
	}
}

func (sc *SerialConnection) writeLoop() {
	for sc.connected {
		select {
		case msg, ok := <-sc.outgoing:
			if !ok {
				return
			}
			if sc.port != nil {
				_, err := sc.port.Write([]byte(msg))
				if err != nil {
					log.Printf("Serial write error: %v", err)
				}
			}
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func (sc *SerialConnection) SendMessage(message string) {
	select {
	case sc.outgoing <- message:
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
