package main

import (
	"fmt"
	"github.com/mpetavy/common"
	"hash"
	"io"
	"io/ioutil"
	"netio/endpoint"
	"os"
	"time"
)

var (
	isClient       bool
	device         string
	isSerialDevice bool
)

func readData(reader io.Reader) (hash.Hash, int64, time.Duration, error) {
	var writer io.Writer
	var err error

	hasher, err := startSession()
	if common.Error(err) {
		return nil, 0, 0, err
	}

	if *filename != "" {
		b, _ := common.FileExists(*filename)

		if b {
			return nil, 0, 0, fmt.Errorf("file already exists: %s", *filename)
		}

		file, err := os.OpenFile(*filename, os.O_CREATE|os.O_APPEND|os.O_RDWR, os.ModePerm)
		if common.Error(err) {
			return nil, 0, 0, err
		}

		defer func() {
			common.DebugError(file.Close())
		}()

		writer = file
	} else {
		writer = ioutil.Discard
	}

	common.Info("Reading bytes ...")

	ba := make([]byte, bufferSize)
	start := time.Now()

	reader = common.NewTimeoutReader(reader, common.MillisecondToDuration(*loopTimeout), false)

	n, err := io.CopyBuffer(io.MultiWriter(writer, hasher), reader, ba)
	if common.Error(err) {
		return nil, 0, 0, err
	}

	return hasher, n, time.Since(start), nil
}

func sendData(writer io.Writer) (hash.Hash, int64, time.Duration, error) {
	var reader io.Reader
	var err error

	hasher, err := startSession()
	if common.Error(err) {
		return nil, 0, 0, err
	}

	if *filename != "" {
		file, err := os.Open(*filename)
		if err != nil {
			return nil, 0, 0, err
		}

		defer func() {
			common.DebugError(file.Close())
		}()

		reader = file

		filesize, err := common.FileSize(*filename)
		if err != nil {
			return nil, 0, 0, err
		}

		common.Info("Sending file content: %v %v bytes ...", *filename, filesize)
	} else {
		if *randomBytes {
			reader = common.NewRandomReader()

			common.Info("Sending random bytes ...")
		} else {
			reader = common.NewZeroReader()

			common.Info("Sending zero bytes ...")
		}
	}

	ba := make([]byte, bufferSize)
	start := time.Now()

	if *loopTimeout > 0 {
		reader = common.NewDeadlineReader(reader, common.MillisecondToDuration(*loopTimeout))
	}

	n, err := io.CopyBuffer(io.MultiWriter(writer, hasher), reader, ba)
	if common.Error(err) {
		return nil, 0, 0, err
	}

	return hasher, n, time.Since(start), nil
}

func work(ep endpoint.Endpoint) error {
	connection, err := ep.GetConnection()
	if common.Error(err) {
		return err
	}

	defer func() {
		common.DebugError(connection.Close())
	}()

	if *isDataSender {
		var duration time.Duration
		var hasher hash.Hash
		var n int64

		hasher, n, duration, err = sendData(connection)
		if common.Error(err) {
			return err
		}

		common.Info("Bytes sent: %v/%+v", common.FormatMemory(n), duration)

		endSession(hasher)
	}

	if *isDataReceiver {
		var duration time.Duration
		var hasher hash.Hash
		var n int64

		hasher, n, duration, err = readData(connection)
		if common.Error(err) {
			return err
		}

		common.Info("Bytes read: %v/%+v", common.FormatMemory(n), duration)

		endSession(hasher)
	}

	common.DebugError(connection.Close())

	return nil
}

func run1() error {
	err := initialize()
	if common.Error(err) {
		return err
	}

	isClient = *client != ""

	if isClient {
		device = *client
	} else {
		device = *server
	}

	isSerialDevice = isSerialPortOptions(device)

	var ep endpoint.Endpoint

	if isSerialDevice {
		ep, err = endpoint.NewSerialServer(device)
		if common.Error(err) {
			return err
		}
	} else {
		if isClient {
			ep, err = endpoint.NewNetworkClient(device, *useTls, *useTlsVerify)
			if common.Error(err) {
				return err
			}
		} else {
			ep, err = endpoint.NewNetworkServer(device, *useTls, *useTlsVerify)
			if common.Error(err) {
				return err
			}
		}
	}

	err = ep.Start()
	if common.Error(err) {
		return err
	}

	defer func() {
		common.Error(ep.Stop())
	}()

	for loop := 0; loop < *loopCount; loop++ {
		if *loopCount > 1 {
			common.Info("Loop #%v", loop)
		}

		err = work(ep)
		if common.Error(err) {
			return err
		}

		if *loopCount > 1 && *isDataSender && *loopSleep > 0 {
			common.Info("Loop #%v: sleep timeout: %v", loop, common.MillisecondToDuration(*loopSleep))

			time.Sleep(common.MillisecondToDuration(*loopSleep))
		}
	}

	return nil
}
