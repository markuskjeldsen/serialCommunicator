# LoRa Serial Communicator

A TUI application built with Go and BubbleTea for communicating with LoRa modules via serial port (FTDI adapter).

The application is designed for simple LoRa module communication and automatically attempts to connect to the serial port on startup.

## Features

- **Automatic Connection**: Connects to the default serial port (`/dev/ttyUSB0`) on startup
- **Serial Communication**: Real-time communication via FTDI adapters
- **LoRa Presets**: Quickly configure LoRa parameters with optimized presets
  - **Long Range & Slow (Default)**: SF12, 125kHz, CR8, 20dBm (optimized for range)
  - **Short Range & Fast**: SF7, 125kHz, CR5, 14dBm (optimized for speed)
- **Chat Interface**: Interactive chat-like display for messages
- **TUI Navigation**: Seamless navigation using keyboard shortcuts

## Installation

1. Install Go (version 1.24+)
2. Clone this repository
3. Build the application:

```bash
# Host build
go build -o serialcom .

# ARM64 build (Android/Linux)
GOOS=linux GOARCH=arm64 go build -o serialcom-arm64 .
```

## Usage

Run the application:
```bash
./run.sh
```

The application will automatically attempt to connect. Use **Ctrl+S** to manually reconnect if needed.

## Keyboard Shortcuts

- **Tab/Shift+Tab**: Cycle through input fields
- **Space**: Toggle preset (when preset field is focused)
- **Enter**: Send message (when message field is focused)
- **Backspace**: Delete character (when message field is focused)
- **Ctrl+S**: Reconnect to serial port
- **Ctrl+D**: Disconnect from serial port
- **Ctrl+H**: Toggle help
- **q**: Quit application

## Default Settings

- **Serial Port**: `/dev/ttyUSB0`
- **Baud Rate**: `9600`
- **Default Preset**: Long Range & Slow (SF12)

## LoRa Configuration

When switching presets, the application enters AT mode and sends the appropriate commands to configure your module. Ensure your module is compatible with standard AT commands.

## Requirements

- Go 1.24 or higher
- LoRa module configured for AT commands
- FTDI USB-to-serial adapter
- Appropriate permissions for serial port access (`/dev/ttyUSB0` or similar)

## Notes

- Messages are sent with `\r\n` line endings as complete packets.
- If the default port is not `/dev/ttyUSB0`, you can modify it in `model.go` or using the on-screen configuration (if available).
- The baud rate is fixed at 9600 to match common LoRa module defaults.
