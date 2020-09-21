package main

import (
	"crypto/md5"
	"crypto/sha256"
	"flag"
	"fmt"
	"hash"
	"io"
	"io/ioutil"
	"netio/endpoint"
	"os"
	"strings"
	"time"

	"github.com/mpetavy/common"
)

// -s /dev/ttyUSB0,115200 -lc 0 -e fc7fa95c5659ccdf5aad8c883916a854
// -c /dev/ttyUSB1,115200 -f README.md -lc 5 -ls 2000

// -s /dev/ttyUSB0,115200 -ds -f README.md -lc 5 -ls 2000
// -c /dev/ttyUSB1,115200 -dr -lc 0 -rs 3000 -e fc7fa95c5659ccdf5aad8c883916a854

var (
	LDFLAG_DEVELOPER = "mpetavy"                          // will be replaced with ldflag
	LDFLAG_HOMEPAGE  = "https://github.com/mpetavy/netio" // will be replaced with ldflag
	LDFLAG_LICENSE   = common.APACHE                      // will be replaced with ldflag
	LDFLAG_VERSION   = "1.0.0"                            // will be replaced with ldflag
	LDFLAG_EXPIRE    = ""                                 // will be replaced with ldflag
	LDFLAG_GIT       = ""                                 // will be replaced with ldflag
	LDFLAG_COUNTER   = "9999"                             // will be replaced with ldflag

	client              *string
	server              *string
	filename            *string
	useTls              *bool
	useTlsVerify        *bool
	isDataSender        *bool
	isDataReceiver      *bool
	bufferSizeString    *string
	readThrottleString  *string
	writeThrottleString *string
	loopCount           *int
	loopTimeout         *int
	readySleep          *int
	hashAlg             *string
	hashExpected        *string
	randomBytes         *bool
	loopSleep           *int
	bufferSize          int64
	readThrottle        int64
	writeThrottle       int64

	isClient       bool
	device         string
	isSerialDevice bool
	hashError      int
)

const (
	READY = "###-READY-###"
)

func init() {
	common.Init(false, LDFLAG_VERSION, "2019", "network/serial performance testing tool", LDFLAG_DEVELOPER, LDFLAG_HOMEPAGE, LDFLAG_LICENSE, nil, nil, run1, 0)

	client = flag.String("c", "", "Client address/serial port")
	server = flag.String("s", "", "Server address/serial port")
	filename = flag.String("f", "", "Filename to write to (server) or read from (client)")
	useTls = flag.Bool("tls", false, "Use TLS")
	useTlsVerify = flag.Bool("tls.verify", false, "TLS verification verification")
	isDataSender = flag.Bool("ds", false, "Act as data sender")
	isDataReceiver = flag.Bool("dr", false, "Act as data receiver")
	hashAlg = flag.String("h", "md5", "Hash algorithm (md5, sha224, sha256)")
	hashExpected = flag.String("e", "", "Expected hash")
	randomBytes = flag.Bool("r", false, "Send random bytes (or '0' bytes)")
	bufferSizeString = flag.String("bs", "32K", "Buffer size in bytes")
	readThrottleString = flag.String("rt", "0", "Read throttled bytes/sec")
	writeThrottleString = flag.String("wt", "0", "Write throttled bytes/sec")
	loopCount = flag.Int("lc", 1, "Loop count")
	loopTimeout = flag.Int("lt", common.DurationToMillisecond(time.Second), "Loop timeout")
	loopSleep = flag.Int("ls", common.DurationToMillisecond(time.Second), "Loop sleep between loop steps")
	readySleep = flag.Int("rs", common.DurationToMillisecond(time.Second), "Sender sleep time before send READY")
}

func initialize() error {
	var err error

	bufferSize, err = common.ParseMemory(*bufferSizeString)
	if common.Error(err) {
		return err
	}

	if isSerialPortOptions(*server) || isSerialPortOptions(*client) {
		bufferSize = int64(common.Min(1024, int(bufferSize)))
	}

	common.Info("Buffer size: %s", common.FormatMemory(bufferSize))

	readThrottle, err = common.ParseMemory(*readThrottleString)
	if common.Error(err) {
		return err
	}

	writeThrottle, err = common.ParseMemory(*writeThrottleString)
	if common.Error(err) {
		return err
	}

	if readThrottle > 0 {
		common.Info("READ throttle bytes/sec: %s = %d Bytes", *readThrottleString, readThrottle)
	}
	if writeThrottle > 0 {
		common.Info("WRITE throttle bytes/sec: %s = %d Bytes", *writeThrottleString, writeThrottle)
	}

	if *filename != "" {
		if mustSendData() {
			b, _ := common.FileExists(*filename)

			if !b {
				return &common.ErrFileNotFound{FileName: *filename}
			}
		}

		if mustReceiveData() {
			b, _ := common.FileExists(*filename)

			if b {
				return fmt.Errorf("file already exists: %s", *filename)
			}
		}
	}

	return nil
}

func mustSendData() bool {
	return *isDataSender || (*client != "" && !*isDataReceiver)
}

func mustReceiveData() bool {
	return *isDataReceiver || (*server != "" && !*isDataSender)
}

func openHasher() (hash.Hash, error) {
	var hasher hash.Hash

	switch *hashAlg {
	case "md5":
		hasher = md5.New()
	case "sha224":
		hasher = sha256.New224()
	case "sha256":
		hasher = sha256.New()
	default:
		return nil, fmt.Errorf("unknown hash algorithm: %s", *hashAlg)
	}

	return hasher, nil
}

func closeHasher(hasher hash.Hash) {
	if hasher != nil {
		hashCalculated := fmt.Sprintf("%x", hasher.Sum(nil))

		common.Info("%s hash: %s", strings.ToUpper(*hashAlg), hashCalculated)

		if *hashExpected != "" {
			common.Info("%s expect: %s", strings.ToUpper(*hashAlg), *hashExpected)

			if *hashExpected != hashCalculated {
				hashError++

				common.Info("%s hash error #%d", strings.ToUpper(*hashAlg), hashError)
			} else {
				common.Info("%s hash correct", strings.ToUpper(*hashAlg))
			}
		}

		hasher.Reset()
	}
}

func isSerialPortOptions(device string) bool {
	return len(device) > 0 && (strings.Contains(device, ",") || !strings.Contains(device, ":"))
}

func waitForReady(connection endpoint.Connection) error {
	received := ""
	ba := make([]byte, len(READY))

	common.Info("Waiting for READY...")

	for {
		n, err := connection.Read(ba)
		if err != nil {
			return err
		}

		if n > 0 {
			received = received + string(ba[:n])

			if strings.HasSuffix(received, READY) {
				common.Info("Received READY")

				break
			}
		}
	}

	return nil
}

func sendReady(connection endpoint.Connection) error {
	if *readySleep > 0 {
		common.Info("Ready sleep: %v", common.MillisecondToDuration(*readySleep))

		time.Sleep(common.MillisecondToDuration(*readySleep))
	}

	common.Info("Sending READY")
	_, err := io.Copy(connection, strings.NewReader(READY))
	if common.Error(err) {
		return err
	}

	return nil
}

func readData(reader io.Reader) (hash.Hash, int64, time.Duration, error) {
	var writer io.Writer
	var err error

	hasher, err := openHasher()
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

	n, err := io.CopyBuffer(io.MultiWriter(hasher, writer), reader, ba)
	if common.Error(err) {
		return nil, 0, 0, err
	}

	return hasher, n, time.Since(start), nil
}

func sendData(writer io.Writer) (hash.Hash, int64, time.Duration, error) {
	var reader io.Reader
	var err error

	hasher, err := openHasher()
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

	if *filename == "" && *loopTimeout > 0 {
		reader = common.NewDeadlineReader(reader, common.MillisecondToDuration(*loopTimeout))
	}

	n, err := io.CopyBuffer(io.MultiWriter(hasher, writer), reader, ba)
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

	err = connection.Reset()
	if common.Error(err) {
		return err
	}

	defer func() {
		common.DebugError(connection.Close())
	}()

	if mustSendData() {
		if *server != "" {
			err := waitForReady(connection)
			if common.Error(err) {
				return err
			}
		}

		var duration time.Duration
		var hasher hash.Hash
		var n int64

		hasher, n, duration, err = sendData(connection)
		if common.Error(err) {
			return err
		}

		common.Info("Bytes sent: %v/%+v", common.FormatMemory(n), duration)

		closeHasher(hasher)
	}

	if mustReceiveData() {
		if *client != "" {
			err := sendReady(connection)
			if common.Error(err) {
				return err
			}
		}

		var duration time.Duration
		var hasher hash.Hash
		var n int64

		hasher, n, duration, err = readData(connection)
		if common.Error(err) {
			return err
		}

		common.Info("Bytes read: %v/%+v", common.FormatMemory(n), duration)

		closeHasher(hasher)
	}

	common.DebugError(connection.Close())

	if mustSendData() && *loopCount > 1 && *loopSleep > 0 {
		common.Info("Loop sleep: %v", common.MillisecondToDuration(*loopSleep))

		time.Sleep(common.MillisecondToDuration(*loopSleep))
	}

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

	for loop := 0; loop < *loopCount || (*loopCount == 0); loop++ {
		if *loopCount > 1 {
			common.Info("Loop #%v", loop)
		}

		err = work(ep)
		if common.Error(err) {
			return err
		}

		common.Info("")
	}

	return nil
}

func main() {
	defer common.Done()

	//flag.VisitAll(func(fl *flag.Flag) {
	//	fmt.Printf("%s | %s | %s\n", fl.Name, fl.DefValue, fl.Usage)
	//})
	//os.Exit(0)

	common.Run([]string{"c|s"})
}
