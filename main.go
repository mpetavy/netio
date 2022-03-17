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
	LDFLAG_VERSION   = "1.0.12"                           // will be replaced with ldflag
	LDFLAG_EXPIRE    = ""                                 // will be replaced with ldflag
	LDFLAG_GIT       = ""                                 // will be replaced with ldflag
	LDFLAG_BUILD     = ""                                 // will be replaced with ldflag

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
	hashAlg          *string
	hashExpected     common.MultiValueFlag
	randomBytes      *bool
	bufferSize       int64
	text             *string
	verbose          *bool
	lengthString     *string

	isClient    bool
	device      string
	verifyCount int
	verifyError int
	length      int64
)

func init() {
	common.Init(true, LDFLAG_VERSION, LDFLAG_GIT, LDFLAG_BUILD, "2019", "TCP/TTY performance testing tool", LDFLAG_DEVELOPER, LDFLAG_HOMEPAGE, LDFLAG_LICENSE, nil, start, stop, run, 0)

	client = flag.String("c", "", "Client network address or TTY port")
	server = flag.String("s", "", "Server network address or TTY port")
	flag.Var(&filenames, "f", "Filename(s) to write to (server) or read from (client)")
	useTls = flag.Bool("tls", false, "Use TLS")
	isDataSender = flag.Bool("ds", false, "Act as data sender")
	isDataReceiver = flag.Bool("dr", false, "Act as data receiver")
	hashAlg = flag.String("y", "md5", "Hash algorithm (md5, sha224, sha256)")
	flag.Var(&hashExpected, "e", "Expected hash value(s)")
	randomBytes = flag.Bool("r", true, "Send random bytes or zero bytes")
	bufferSizeString = flag.String("bs", "32K", "Buffer size in bytes")
	loopCount = flag.Int("lc", 1, "Loop count. Must be defined equaly on client and server side")
	loopTimeout = flag.Int("lt", 0, "Loop timeout")
	loopSleep = flag.Int("ls", 0, "Loop sleep between loop steps")
	text = flag.String("t", "", "text to send")
	verbose = flag.Bool("v", false, "output the received content")
	lengthString = flag.String("l", "0", "Amount of bytes to send")

	common.Events.NewFuncReceiver(common.EventFlagsParsed{}, func(event common.Event) {
		if *server != "" && !common.IsRunningAsService() {
			common.Panic(flag.Set(common.FlagNameService, common.SERVICE_SIMULATE))
		}

		var err error

		length, err = common.ParseMemory(*lengthString)

		common.Panic(err)
	})
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

	if *loopTimeout > 0 {
		reader = common.NewTimeoutReader(reader, common.MillisecondToDuration(*loopTimeout), false)
	}

	timeoutReader := reader.(*common.TimeoutReader)

	var cw *consoleWriter

	verboseOutput := io.Discard
	if *verbose {
		cw = &consoleWriter{}

		verboseOutput = cw
	}

	n, err := common.HandleCopyBufferError(io.CopyBuffer(io.MultiWriter(hasher, writer, verboseOutput), reader, ba))
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

	if length > 0 {
		reader = common.NewSizedReader(reader, length)
	}

	start := time.Now()

	n, err := common.HandleCopyBufferError(io.CopyBuffer(io.MultiWriter(hasher, writer), reader, ba))
	if common.Error(err) {
		return nil, 0, 0, err
	}

	d := time.Since(start)

	return hasher, n, d, nil
}

func calcPerformance(n int64, d time.Duration) string {
	if d > 0 {
		bytesPerSecond := int64(math.Round(float64(n) / d.Seconds()))

		return fmt.Sprintf("%s/%.2fs or %s/%v", common.FormatMemory(n), d.Seconds(), common.FormatMemory(bytesPerSecond), time.Second)
	} else {
		return fmt.Sprintf("%s/%.2fs", common.FormatMemory(n), d.Seconds())
	}

}

func work(loop int, connector common.EndpointConnector) error {
	connection, err := connector()
	if common.Error(err) {
		return err
	}

	defer func() {
		common.DebugError(connection.Close())
	}()

	if mustSendData() {
		hasher, n, duration, err := sendData(loop, connection)
		if common.Error(err) {
			return err
		}

		common.Info("Bytes sent: %d bytes, about %s", n, calcPerformance(n, duration))

		closeHasher(loop, hasher)
	}

	if mustReceiveData() {
		hasher, n, duration, err := readData(loop, connection)
		if common.Error(err) {
			return err
		}

		common.Info("Bytes received: % bytes, about %s", n, calcPerformance(n, duration))

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

	if common.IsTTYDevice(*server) || common.IsTTYDevice(*client) {
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
		if length == 0 {
			if *loopTimeout == 0 {
				*loopTimeout = 1000
			}
		}

		if *loopSleep == 0 {
			*loopSleep = 2000
		}
	}

	if *loopCount == 0 {
		if len(filenames) > 0 {
			*loopCount = len(filenames)
		} else {
			if len(hashExpected) > 0 {
				*loopCount = len(hashExpected)
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

	var err error
	var tlsConfig *tls.Config

	if *useTls {
		tlsConfig, err = common.NewTlsConfigFromFlags()
		if common.Error(err) {
			return err
		}
	}

	ep, connector, err := common.NewEndpoint(device, isClient, tlsConfig)
	if common.Error(err) {
		return err
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
			return err
		}

		if mustSendData() && *loopSleep > 0 && ((*loopCount == 0) || ((loop + 1) < *loopCount)) {
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

	common.Run([]string{"c|s"})
}
