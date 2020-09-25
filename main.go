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

// -s COM3,115200 -e 73e635a16e430826fef008d97abf6012
// -c COM4,115200 -f README.md

// -s COM3,115200 -ds -f README.md -lc 5 -ls 2000
// -c COM4,115200 -dr -rs 3000 -e 73e635a16e430826fef008d97abf6012

// -s COM3,115200
// -c COM4,115200 -lc 5 -r

var (
	LDFLAG_DEVELOPER = "mpetavy"                          // will be replaced with ldflag
	LDFLAG_HOMEPAGE  = "https://github.com/mpetavy/netio" // will be replaced with ldflag
	LDFLAG_LICENSE   = common.APACHE                      // will be replaced with ldflag
	LDFLAG_VERSION   = "1.0.2"                            // will be replaced with ldflag
	LDFLAG_EXPIRE    = ""                                 // will be replaced with ldflag
	LDFLAG_GIT       = ""                                 // will be replaced with ldflag
	LDFLAG_COUNTER   = "9999"                             // will be replaced with ldflag

	client           *string
	server           *string
	filenames        common.MultiValueFlag
	useTls           *bool
	useTlsVerify     *bool
	isDataSender     *bool
	isDataReceiver   *bool
	bufferSizeString *string
	loopCount        *int
	loopTimeout      *int
	readySleep       *int
	hashAlg          *string
	hashExpected     common.MultiValueFlag
	randomBytes      *bool
	loopSleep        *int
	bufferSize       int64

	isClient       bool
	device         string
	isSerialDevice bool
	verifyCount    int
	verifyError    int
)

const (
	READY = "###-READY-###"
)

func init() {
	common.Init(true, LDFLAG_VERSION, "2019", "network/serial performance testing tool", LDFLAG_DEVELOPER, LDFLAG_HOMEPAGE, LDFLAG_LICENSE, start, stop, run, 0)

	client = flag.String("c", "", "Client address/serial port")
	server = flag.String("s", "", "Server address/serial port")
	flag.Var(&filenames, "f", "Filename(s) to write to (server) or read from (client)")
	useTls = flag.Bool("tls", false, "Use TLS")
	useTlsVerify = flag.Bool("tls.verify", false, "TLS verification")
	isDataSender = flag.Bool("ds", false, "Act as data sender")
	isDataReceiver = flag.Bool("dr", false, "Act as data receiver")
	hashAlg = flag.String("h", "md5", "Hash algorithm (md5, sha224, sha256)")
	flag.Var(&hashExpected, "e", "Expected hash value(s)")
	randomBytes = flag.Bool("r", false, "Send random bytes (or '0' bytes)")
	bufferSizeString = flag.String("bs", "32K", "Buffer size in bytes")
	loopCount = flag.Int("lc", 0, "Loop count")
	loopTimeout = flag.Int("lt", 0, "Loop timeout")
	loopSleep = flag.Int("ls", 0, "Loop sleep between loop steps")
	readySleep = flag.Int("rs", common.DurationToMillisecond(time.Second), "Sender sleep time before send READY")
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

func closeHasher(loop int, hasher hash.Hash) {
	if hasher != nil {
		hashCalculated := fmt.Sprintf("%x", hasher.Sum(nil))

		common.Info("%s hash: %s", strings.ToUpper(*hashAlg), hashCalculated)

		if len(hashExpected) > 0 {
			verifyCount++
			expected := hashExpected[loop%len(hashExpected)]

			common.Info("%s expect: %s", strings.ToUpper(*hashAlg), expected)

			if strings.ToUpper(expected) != strings.ToUpper(hashCalculated) {
				verifyError++

				common.Info("Hash Error #%d", verifyError)
			} else {
				common.Info("Hash Correct!")
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

func readData(loop int, reader io.Reader) (hash.Hash, int64, time.Duration, error) {
	var writer io.Writer
	var err error

	hasher, err := openHasher()
	if common.Error(err) {
		return nil, 0, 0, err
	}

	if len(filenames) > 0 {
		filename := filenames[loop%len(filenames)]

		err := common.FileBackup(filename)
		if common.Error(err) {
			return nil, 0, 0, err
		}

		file, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_RDWR, os.ModePerm)
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

func sendData(loop int, writer io.Writer) (hash.Hash, int64, time.Duration, error) {
	var reader io.Reader
	var err error

	hasher, err := openHasher()
	if common.Error(err) {
		return nil, 0, 0, err
	}

	if len(filenames) > 0 {
		filename := filenames[loop%len(filenames)]

		file, err := os.Open(filename)
		if err != nil {
			return nil, 0, 0, err
		}

		defer func() {
			common.DebugError(file.Close())
		}()

		reader = file

		filesize, err := common.FileSize(filename)
		if err != nil {
			return nil, 0, 0, err
		}

		common.Info("Sending file content: %v %s ...", filename, common.FormatMemory(filesize))
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

	if len(filenames) == 0 && len(hashExpected) == 0 && *loopTimeout > 0 {
		reader = common.NewDeadlineReader(reader, common.MillisecondToDuration(*loopTimeout))
	}

	n, err := io.CopyBuffer(io.MultiWriter(hasher, writer), reader, ba)
	if common.Error(err) {
		return nil, 0, 0, err
	}

	return hasher, n, time.Since(start), nil
}

func calcPerformance(n int64, duration time.Duration) string {
	if duration.Seconds() >= 1 {
		bytesPerSecond := int64(float64(n) / duration.Seconds())

		return fmt.Sprintf("%s/%v", common.FormatMemory(bytesPerSecond), time.Second)
	} else {
		return fmt.Sprintf("%s/%v", common.FormatMemory(n), duration)
	}
}

func work(loop int, ep endpoint.Endpoint) error {
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

		hasher, n, duration, err := sendData(loop, connection)
		if common.Error(err) {
			return err
		}

		common.Info("Bytes sent: %s", calcPerformance(n, duration))

		closeHasher(loop, hasher)
	}

	if mustReceiveData() {
		if *client != "" {
			err := sendReady(connection)
			if common.Error(err) {
				return err
			}
		}

		hasher, n, duration, err := readData(loop, connection)
		if common.Error(err) {
			return err
		}

		common.Info("Bytes received: %s", calcPerformance(n, duration))

		closeHasher(loop, hasher)
	}

	common.DebugError(connection.Close())

	if mustSendData() && *loopCount > 1 && *loopSleep > 0 {
		common.Info("Loop sleep: %v", common.MillisecondToDuration(*loopSleep))

		time.Sleep(common.MillisecondToDuration(*loopSleep))
	}

	return nil
}

func start() error {
	var err error

	bufferSize, err = common.ParseMemory(*bufferSizeString)
	if common.Error(err) {
		return err
	}

	if isSerialPortOptions(*server) || isSerialPortOptions(*client) {
		bufferSize = int64(common.Min(1024, int(bufferSize)))
	}

	common.Info("Buffer size: %s", common.FormatMemory(bufferSize))

	if mustReceiveData() {
		if *loopTimeout == 0 {
			*loopTimeout = 1000
		}

		if *loopSleep == 0 {
			*loopSleep = 1000
		}
	}

	if mustSendData() {
		if *loopTimeout == 0 {
			*loopTimeout = 1000
		}

		if *loopSleep == 0 {
			*loopSleep = 2000
		}
	}

	if *loopCount == 0 {
		if len(filenames) > 0 {
			*loopCount = len(filenames)
		} else {
			*loopCount = 1
		}
	}

	for _, filename := range filenames {
		if mustSendData() {
			b, _ := common.FileExists(filename)

			if !b {
				return common.ErrorReturn(&common.ErrFileNotFound{FileName: filename})
			}
		}

		if mustReceiveData() {
			b, _ := common.FileExists(filename)

			if b {
				return common.ErrorReturn(fmt.Errorf("file already exists: %s", filename))
			}
		}
	}

	return nil
}

func run() error {
	isClient = *client != ""

	if isClient {
		device = *client
	} else {
		device = *server
	}

	isSerialDevice = isSerialPortOptions(device)

	var err error
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

	for loop := 0; mustReceiveData() || (loop < *loopCount); loop++ {
		if *loopCount > 1 {
			common.Info("Loop #%v", loop)
		}

		err = work(loop, ep)
		if common.Error(err) {
			return err
		}
	}

	return nil
}

func stop() error {
	if len(hashExpected) > 0 {
		common.Info("")
		common.Info("--- Summary ------------------------")
		common.Info("Runs:    %d", verifyCount)
		common.Info("Correct: %d", verifyCount-verifyError)
		common.Info("Errors:  %d", verifyError)
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
