#!/bin/bash

echo "Running LoRa Serial Communicator..."
echo "Default serial port: /dev/ttyUSB0 (FTDI adapter)"
echo "Baud rate: 9600"
echo ""
echo "Make sure your LoRa module is configured to accept AT commands:"
echo "  AT+SF=7/12    - Spreading Factor (7 for fast, 12 for long range)"
echo "  AT+BW=125000  - Bandwidth in Hz"
echo "  AT+CR=5/8     - Coding Rate"
echo "  AT+TP=14/20   - Transmit Power in dBm"
echo ""
echo "Press Ctrl+H for in-application help"
echo ""

./serialcom