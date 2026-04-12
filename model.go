package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type model struct {
	serialPort   string
	baudRate     int
	connected    bool
	preset       string
	messageInput string
	chatHistory  []string
	width        int
	height       int
	focusedInput string
	serialError  string
	helpMode     bool
	serialConn   *SerialConnection
}

func initialModel() model {
	return model{
		serialPort:   "/dev/ttyUSB0",
		baudRate:     9600,
		connected:    false,
		preset:       "short_fast",
		messageInput: "",
		chatHistory:  []string{},
		focusedInput: "serial",
		helpMode:     false,
		serialConn:   nil,
	}
}

type serialMsg string
type errorMsg error

func (m model) Init() tea.Cmd {
	return tea.Tick(time.Second/10, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

type tickMsg time.Time

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.serialConn != nil {
				m.serialConn.Disconnect()
			}
			return m, tea.Quit
		case "ctrl+h":
			m.helpMode = !m.helpMode
			return m, nil
		case "ctrl+s":
			return m, m.connectSerial()
		case "ctrl+d":
			if m.serialConn != nil {
				m.serialConn.Disconnect()
				m.serialConn = nil
				m.connected = false
			}
			return m, nil
		case "tab":
			if m.focusedInput == "serial" {
				m.focusedInput = "message"
			} else if m.focusedInput == "message" {
				m.focusedInput = "preset"
			} else {
				m.focusedInput = "serial"
			}
			return m, nil
		case "shift+tab":
			if m.focusedInput == "serial" {
				m.focusedInput = "preset"
			} else if m.focusedInput == "preset" {
				m.focusedInput = "message"
			} else {
				m.focusedInput = "serial"
			}
			return m, nil
		case "enter":
			if m.focusedInput == "message" && m.messageInput != "" {
				m.chatHistory = append(m.chatHistory, "You: "+m.messageInput)
				if m.serialConn != nil && m.serialConn.IsConnected() {
					m.serialConn.SendChatMessage(m.messageInput)
				}
				m.messageInput = ""
			}
			return m, nil
		case "backspace":
			if m.focusedInput == "message" && len(m.messageInput) > 0 {
				m.messageInput = m.messageInput[:len(m.messageInput)-1]
			}
			return m, nil
		case " ":
			if m.focusedInput == "preset" {
				if m.preset == "short_fast" {
					m.preset = "long_slow"
				} else {
					m.preset = "short_fast"
				}
				if m.serialConn != nil && m.serialConn.IsConnected() {
					if preset, ok := GetPreset(m.preset); ok {
						m.serialConn.ApplyPreset(preset)
					}
				}
				return m, nil
			} else if m.focusedInput == "message" {
				m.messageInput += " "
				return m, nil
			}
		default:
			if m.focusedInput == "message" && len(msg.String()) == 1 {
				m.messageInput += msg.String()
				return m, nil
			}
		}

	case errorMsg:
		m.serialError = fmt.Sprintf("Connection error: %v", msg.Error())
		m.connected = false
		fmt.Printf("Connection failed: %v\n", msg.Error())
		return m, nil

	case connectedMsg:
		fmt.Println("Connection successful!")
		m.serialConn = msg.conn
		m.connected = true
		m.serialError = ""
		return m, nil

	case tickMsg:
		if m.serialConn != nil && m.serialConn.IsConnected() {
			select {
			case incoming := <-m.serialConn.GetIncoming():
				m.chatHistory = append(m.chatHistory, "LoRa: "+incoming)
			default:
			}
		}
		return m, tea.Tick(time.Second/10, func(t time.Time) tea.Msg {
			return tickMsg(t)
		})
	}

	return m, nil
}

func (m model) View() string {
	if m.helpMode {
		return m.helpView()
	}

	var b strings.Builder

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("62")).
		Padding(0, 1).
		Render("LoRa Serial Communicator")

	status := "Disconnected"
	if m.connected {
		status = lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Render("Connected")
	} else {
		status = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("Disconnected")
	}

	header := lipgloss.JoinHorizontal(lipgloss.Left,
		title,
		lipgloss.NewStyle().Padding(0, 2).Render("|"),
		status,
	)

	b.WriteString(header + "\n\n")

	configSection := lipgloss.NewStyle().Width(40).Render(
		lipgloss.JoinVertical(lipgloss.Left,
			"Configuration:",
			"",
			m.renderInputField("Serial Port:", m.serialPort, "serial"),
			m.renderInputField("Baud Rate:", fmt.Sprintf("%d", m.baudRate), "serial"),
			m.renderPresetField(),
			"",
			lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("[Space] Toggle preset  [Tab] Navigate  [Ctrl+H] Help"),
		),
	)

	chatSection := lipgloss.NewStyle().
		Width(m.width-45).
		Height(m.height-10).
		BorderStyle(lipgloss.RoundedBorder()).
		Padding(0, 1).
		Render(
			lipgloss.JoinVertical(lipgloss.Left,
				"Chat Messages:",
				"",
				m.renderChatHistory(),
				"",
				m.renderMessageInput(),
			),
		)

	mainContent := lipgloss.JoinHorizontal(lipgloss.Top, configSection, "  ", chatSection)

	b.WriteString(mainContent + "\n\n")

	if m.serialError != "" {
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Render("Error: "+m.serialError) + "\n")
	}

	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("Press q to quit"))

	return b.String()
}

func (m model) renderInputField(label, value string, field string) string {
	style := lipgloss.NewStyle()
	if m.focusedInput == field {
		style = style.BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(0, 1)
	}
	return style.Render(label + " " + value)
}

func (m model) renderPresetField() string {
	preset, ok := GetPreset(m.preset)
	presetName := "Unknown Preset"
	if ok {
		presetName = preset.Name
	}

	style := lipgloss.NewStyle()
	if m.focusedInput == "preset" {
		style = style.BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(0, 1)
	}

	indicator := "◉"
	if m.focusedInput == "preset" {
		indicator = "▶"
	}

	return style.Render(fmt.Sprintf("%s Preset: %s", indicator, presetName))
}

func (m model) renderMessageInput() string {
	style := lipgloss.NewStyle()
	if m.focusedInput == "message" {
		style = style.BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(0, 1)
	}
	return style.Render("Message: " + m.messageInput + "▌")
}

func (m model) renderChatHistory() string {
	if len(m.chatHistory) == 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("No messages yet...")
	}

	var lines []string
	start := 0
	if len(m.chatHistory) > 10 {
		start = len(m.chatHistory) - 10
	}
	for i := start; i < len(m.chatHistory); i++ {
		lines = append(lines, m.chatHistory[i])
	}
	return strings.Join(lines, "\n")
}

func (m model) helpView() string {
	helpText := `LoRa Serial Communicator - Help

Navigation:
  Tab/Shift+Tab    Cycle through input fields
  Space            Toggle preset in preset field
  Enter            Send message in message field
  Backspace        Delete character in message field
  Ctrl+H           Toggle help
  Ctrl+S           Connect to serial port
  Ctrl+D           Disconnect from serial port
  q                Quit application

Presets (AT Commands):
  Short Range & Fast (L7)  SF 5, 125kHz, 13020 bit/s
  Long Range & Slow (L0)   SF 12, 125kHz, 244 bit/s

Serial Settings:
  Default port: /dev/ttyUSB0 (common for FTDI adapters)
  Baud rate: Fixed at 9600 (configure your LoRa module accordingly)

AT Command Support:
  The application sends "+++" to enter AT command mode (no \\r\\n),
  then sends the selected preset command (e.g., "AT+LEVEL7\\r\\n").
  The module should automatically return to data mode.

Message Sending:
  Type messages in the message field and press Enter to send.
  All messages are sent with a "\\r\\n" line ending.

Press any key to return...`

	return lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Padding(1, 2).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Render(helpText)
}

func sendSerialMessageCmd(message string) tea.Cmd {
	return func() tea.Msg {
		return serialMsg(message)
	}
}

func (m *model) connectSerial() tea.Cmd {
	return func() tea.Msg {
		if m.serialConn != nil {
			m.serialConn.Disconnect()
		}

		conn := NewSerialConnection(m.serialPort, m.baudRate)
		err := conn.Connect()
		if err != nil {
			return errorMsg(err)
		}

		if preset, ok := GetPreset(m.preset); ok {
			conn.ApplyPreset(preset)
		}

		return connectedMsg{conn: conn}
	}
}

type connectedMsg struct {
	conn *SerialConnection
}
