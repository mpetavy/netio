package main

import (
	"crypto/md5"
	"crypto/sha256"
	"crypto/tls"
	"flag"
	"fmt"
	"hash"
	"io"
	"math"
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
	LDFLAG_VERSION   = "1.0.3"                            // will be replaced with ldflag
	LDFLAG_EXPIRE    = ""                                 // will be replaced with ldflag
	LDFLAG_GIT       = ""                                 // will be replaced with ldflag
	LDFLAG_COUNTER   = "9999"                             // will be replaced with ldflag

	client           *string
	server           *string
	filenames        common.MultiValueFlag
	useTls           *bool
	isDataSender     *bool
	isDataReceiver   *bool
	bufferSizeString *string
	loopCount        *int
	loopTimeout      *int
	loopSleep        *int
	readySleep       *int
	hashAlg          *string
	hashExpected     common.MultiValueFlag
	randomBytes      *bool
	bufferSize       int64
	text             *string
	verbose          *bool

	isClient    bool
	device      string
	isTTYDevice bool
	verifyCount int
	verifyError int
)

const (
	READY = "###-READY-###"
)

type Endpoint interface {
	Start() error
	Stop() error
}

type EndpointConnector func() (io.ReadWriteCloser, error)

func init() {
	common.Init(true, LDFLAG_VERSION, LDFLAG_GIT, "2019", "TCP/TTY performance testing tool", LDFLAG_DEVELOPER, LDFLAG_HOMEPAGE, LDFLAG_LICENSE, nil, start, stop, run, 0)

	client = flag.String("c", "", "Client network address or TTY port")
	server = flag.String("s", "", "Server network address or TTY port")
	flag.Var(&filenames, "f", "Filename(s) to write to (server) or read from (client)")
	useTls = flag.Bool("tls", false, "Use TLS")
	isDataSender = flag.Bool("ds", false, "Act as data sender")
	isDataReceiver = flag.Bool("dr", false, "Act as data receiver")
	hashAlg = flag.String("h", "md5", "Hash algorithm (md5, sha224, sha256)")
	flag.Var(&hashExpected, "e", "Expected hash value(s)")
	randomBytes = flag.Bool("r", true, "Send random bytes or zero bytes")
	bufferSizeString = flag.String("bs", "32K", "Buffer size in bytes")
	loopCount = flag.Int("lc", 0, "Loop count. Must be defined equaly on client and server side")
	loopTimeout = flag.Int("lt", 0, "Loop timeout")
	loopSleep = flag.Int("ls", 0, "Loop sleep between loop steps")
	readySleep = flag.Int("rs", common.DurationToMillisecond(time.Second), "Sender sleep time before send READY")
	text = flag.String("t", "", "text to send")
	verbose = flag.Bool("v", false, "output the received content")
}

func mustSendData() bool {
	return (*client != "" && !*isDataReceiver) || (*server != "" && *isDataSender)
}

func mustReceiveData() bool {
	return (*client != "" && *isDataReceiver) || (*server != "" && !*isDataSender)
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

func isTTYOptions(device string) bool {
	return len(device) > 0 && (strings.Contains(device, ",") || !strings.Contains(device, ":"))
}

func waitForReady(connection io.ReadWriteCloser) error {
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

func sendReady(connection io.ReadWriteCloser) error {
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

type consoleWriter struct {
	HasEndedWithCRLF bool
}

func (this *consoleWriter) Write(p []byte) (n int, err error) {
	txt := string(p)

	this.HasEndedWithCRLF = strings.HasSuffix(txt, "\n")

	return fmt.Printf("%s", txt)
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
		writer = io.Discard
	}

	common.Info("Reading bytes ...")

	ba := make([]byte, bufferSize)

	timeoutReader := common.NewTimeoutReader(reader, common.MillisecondToDuration(*loopTimeout), false)

	reader = timeoutReader

	var cw *consoleWriter

	verboseOutput := io.Discard
	if *verbose {
		cw = &consoleWriter{}

		verboseOutput = cw
	}

	n, err := common.CopyBufferError(io.CopyBuffer(io.MultiWriter(hasher, writer, verboseOutput), reader, ba))
	if common.Error(err) {
		return nil, 0, 0, err
	}

	d := time.Since(timeoutReader.FirstRead)

	if cw != nil && !cw.HasEndedWithCRLF {
		fmt.Printf("\n")
	}

	return hasher, n, d, nil
}

func sendData(loop int, writer io.Writer) (hash.Hash, int64, time.Duration, error) {
	var reader io.Reader
	var err error

	hasher, err := openHasher()
	if common.Error(err) {
		return nil, 0, 0, err
	}

	if *text != "" {
		reader = strings.NewReader(*text)

		common.Info("Sending text %s ...", *text)
	} else {
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
	}

	ba := make([]byte, bufferSize)

	if len(filenames) == 0 && len(hashExpected) == 0 && *loopTimeout > 0 {
		reader = common.NewDeadlineReader(reader, common.MillisecondToDuration(*loopTimeout))
	}

	start := time.Now()

	n, err := common.CopyBufferError(io.CopyBuffer(io.MultiWriter(hasher, writer), reader, ba))
	if common.Error(err) {
		return nil, 0, 0, err
	}

	d := time.Since(start)
	n = n / 2 // sent to io.MultiWriter(hasher, writer) ...

	return hasher, n, d, nil
}

func calcPerformance(n int64, d time.Duration) string {
	if d.Seconds() >= 1 {
		bytesPerSecond := int64(float64(n) / math.Round(d.Seconds()))

		return fmt.Sprintf("%s/%v", common.FormatMemory(bytesPerSecond), time.Second)
	} else {
		return fmt.Sprintf("%s/%v", common.FormatMemory(n), d)
	}
}

func work(loop int, connector EndpointConnector) error {
	connection, err := connector()
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

	return nil
}

func start() error {
	var err error

	bufferSize, err = common.ParseMemory(*bufferSizeString)
	if common.Error(err) {
		return err
	}

	if isTTYOptions(*server) || isTTYOptions(*client) {
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
		if *text != "" {
			*loopCount = 1
		} else {
			if len(filenames) > 0 {
				*loopCount = len(filenames)
			} else {
				if len(hashExpected) > 0 {
					*loopCount = len(hashExpected)
				}
			}
		}
	}

	for _, filename := range filenames {
		if mustSendData() {
			if !common.FileExists(filename) {
				return common.ErrorReturn(&common.ErrFileNotFound{FileName: filename})
			}
		}

		if mustReceiveData() {
			if !common.FileExists(filename) {
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

	isTTYDevice = isTTYOptions(device)

	var err error
	var ep Endpoint
	var connector EndpointConnector

	if isTTYDevice {
		tty, err := common.NewTTY(device)
		if common.Error(err) {
			return err
		}

		ep = tty

		connector = func() (io.ReadWriteCloser, error) {
			return tty.Connect()
		}
	} else {
		var tlsConfig *tls.Config
		var err error

		if *useTls {
			tlsConfig, err = common.NewTlsConfigFromFlags()
			if common.Error(err) {
				return err
			}
		}

		if isClient {
			networkClient, err := common.NewNetworkClient(device, tlsConfig)
			if common.Error(err) {
				return err
			}

			connector = func() (io.ReadWriteCloser, error) {
				return networkClient.Connect()
			}

			ep = networkClient
		} else {
			networkServer, err := common.NewNetworkServer(device, tlsConfig)
			if common.Error(err) {
				return err
			}

			connector = func() (io.ReadWriteCloser, error) {
				return networkServer.Connect()
			}

			ep = networkServer
		}
	}

	err = ep.Start()
	if common.Error(err) {
		return err
	}

	defer func() {
		common.Error(ep.Stop())
	}()

	for loop := 0; (*loopCount == 0) || (loop < *loopCount); loop++ {
		common.Info("")
		common.Info("Loop #%v", loop+1)

		err = work(loop, connector)
		if common.Error(err) {
			if mustSendData() {
				return err
			}
		}

		if mustSendData() && *loopSleep > 0 && (*loopCount == 0) || ((loop + 1) < *loopCount) {
			common.Info("Loop sleep: %v", common.MillisecondToDuration(*loopSleep))

			time.Sleep(common.MillisecondToDuration(*loopSleep))
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
