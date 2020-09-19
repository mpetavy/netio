package endpoint

import (
	"fmt"
	"github.com/mpetavy/common"
	"go.bug.st/serial"
	"io"
	"strconv"
	"strings"
)

type SerialConnection struct {
	port io.ReadWriteCloser
}

func (serialConnection *SerialConnection) Read(p []byte) (n int, err error) {
	return serialConnection.port.Read(p)
}

func (serialConnection *SerialConnection) Write(p []byte) (n int, err error) {
	return serialConnection.port.Write(p)
}

func (serialConnection *SerialConnection) Close() error {
	defer func() {
		serialConnection.port = nil
	}()

	if serialConnection.port != nil {
		err := serialConnection.port.Close()
		if common.Error(err) {
			return err
		}
	}

	return nil
}

type SerialServer struct {
	device string
}

func NewSerialServer(device string) (*SerialServer, error) {
	serialServer := &SerialServer{
		device: device,
	}

	serialServer.device = device

	return serialServer, nil
}

func (serialServer *SerialServer) Start() error {
	return nil
}

func (serialServer *SerialServer) Stop() error {
	return nil
}

func (serialServer *SerialServer) GetConnection() (Connection, error) {
	common.Info("Connected: %s", serialServer.device)

	serialPort, mode, err := evaluateSerialPortOptions(serialServer.device)
	if common.Error(err) {
		return nil, err
	}

	port, err := serial.Open(serialPort, mode)
	if common.Error(err) {
		return nil, err
	}

	return &SerialConnection{
		port: port,
	}, nil
}

func evaluateSerialPortOptions(device string) (string, *serial.Mode, error) {
	ss := strings.Split(device, ",")

	baudrate := 9600
	databits := 8
	stopbits := serial.OneStopBit
	paritymode := serial.NoParity
	pm := "N"
	sb := "1"

	var portname string
	var err error

	portname = ss[0]
	if len(ss) > 1 {
		baudrate, err = strconv.Atoi(ss[1])
		if err != nil {
			return "", nil, fmt.Errorf("invalid baudrate: %s", ss[1])
		}
	}
	if len(ss) > 2 {
		databits, err = strconv.Atoi(ss[2])
		if err != nil {
			return "", nil, fmt.Errorf("invalid databits: %s", ss[2])
		}
	}
	if len(ss) > 3 {
		pm = strings.ToUpper(ss[3][:1])

		switch pm {
		case "N":
			paritymode = serial.NoParity
		case "O":
			paritymode = serial.OddParity
		case "E":
			paritymode = serial.EvenParity
		default:
			return "", nil, fmt.Errorf("invalid partitymode: %s", pm)
		}
	}

	if len(ss) > 4 {
		sb = strings.ToUpper(ss[4][:1])

		switch sb {
		case "1":
			stopbits = serial.OneStopBit
		case "1.5":
			stopbits = serial.OnePointFiveStopBits
		case "2":
			stopbits = serial.TwoStopBits
		default:
			return "", nil, fmt.Errorf("invalid stopbits: %s", sb)
		}
	}

	common.Info("Use serial port: %s Baudrate: %d %d %s %s", portname, baudrate, databits, pm, sb)

	return portname, &serial.Mode{
		BaudRate: baudrate,
		DataBits: databits,
		Parity:   paritymode,
		StopBits: stopbits,
	}, nil
}
