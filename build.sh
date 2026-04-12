#!/bin/bash

echo "Building LoRa Serial Communicator..."
go build -o serialcom .

if [ $? -eq 0 ]; then
    echo "Build successful! Executable: serialcom"
    echo ""
    echo "Usage: ./serialcom"
    echo ""
    echo "Keyboard shortcuts:"
    echo "  Ctrl+S: Connect to serial port"
    echo "  Ctrl+D: Disconnect from serial port"
    echo "  Ctrl+H: Toggle help"
    echo "  Tab: Navigate between fields"
    echo "  Space: Toggle preset (in preset field)"
    echo "  Enter: Send message (in message field)"
    echo "  q: Quit"
else
    echo "Build failed!"
    exit 1
fi