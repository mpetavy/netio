package main

import (
	"bufio"
	"crypto/md5"
	"crypto/sha256"
	"crypto/tls"
	"embed"
	"encoding/hex"
	"flag"
	"fmt"
	"hash"
	"io"
	"math"
	"os"
	"path/filepath"
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
	LDFLAG_VERSION   = "1.3.0"                            // will be replaced with ldflag
	LDFLAG_EXPIRE    = ""                                 // will be replaced with ldflag
	LDFLAG_GIT       = ""                                 // will be replaced with ldflag
	LDFLAG_BUILD     = ""                                 // will be replaced with ldflag

	HL7Start = []byte{0xb}
	HL7End   = []byte{0x1c, 0xd}

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
	data             *bool
	lengthString     *string
	hl7              *bool
	prefix           *string
	suffix           *string

	isClient    bool
	device      string
	verifyCount int
	verifyError int
	length      int64
)

//go:embed go.mod
var resources embed.FS

func init() {
	common.Init("", LDFLAG_VERSION, LDFLAG_GIT, LDFLAG_BUILD, "TCP/TTY performance testing tool", LDFLAG_DEVELOPER, LDFLAG_HOMEPAGE, LDFLAG_LICENSE, &resources, start, stop, run, 0)

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
	loopTimeout = flag.Int("lt", 1000, "Loop timeout")
	loopSleep = flag.Int("ls", 0, "Loop sleep between loop steps")
	text = flag.String("t", "", "text to send")
	data = flag.Bool("d", false, "output the received content")
	lengthString = flag.String("l", "0", "Amount of bytes to send")
	hl7 = flag.Bool("hl7", false, "HL7 message processing")
	prefix = flag.String("prefix", "", "Prefix per message frame")
	suffix = flag.String("suffix", "", "Suffix per message frame")

	common.Events.AddListener(common.EventFlags{}, func(event common.Event) {
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

	fmt.Printf("%s", common.PrintBytes(p, false))

	return len(p), err
}

func readMessage(loop int, reader io.Reader) error {
	hasher, err := openHasher()
	if common.Error(err) {
		return err
	}

	common.Info("Reading messages ...")

	ba := make([]byte, bufferSize)

	if *loopTimeout > 0 {
		reader = common.NewTimeoutReader(reader, false, common.MillisecondToDuration(*loopTimeout))
	}

	var cw *consoleWriter

	verboseOutput := io.Discard
	if *data {
		cw = &consoleWriter{}

		verboseOutput = cw
	}

	p, err := hex.DecodeString(*prefix)
	if common.Error(err) {
		return err
	}
	s, err := hex.DecodeString(*suffix)
	if common.Error(err) {
		return err
	}

	sf, err := common.NewSeparatorSplitFunc(p, s, true)
	if common.Error(err) {
		return err
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(ba, len(ba))
	scanner.Split(sf)

	for scanner.Scan() {
		ba := scanner.Bytes()

		filename := ""
		if len(filenames) > 0 {
			filename = fmt.Sprintf("message-%s.msg", common.Trim4Path(time.Now().Format(common.SortedDateTimeMilliMask)))
			common.Info("-- new message %s -----------------------------------------------", filename)
		} else {
			common.Info("-- new message -----------------------------------------------")
		}

		common.Info(string(ba))

		_, err := io.MultiWriter(hasher, verboseOutput).Write(ba)
		if common.Error(err) {
			return err
		}

		if filename != "" {
			err := os.WriteFile(filepath.Join(filenames[0], filename), ba, common.DefaultFileMode)
			if common.Error(err) {
				return err
			}
		}
	}

	if cw != nil && !cw.HasEndedWithCRLF {
		fmt.Printf("\n")
	}

	return nil
}

func readBytes(loop int, reader io.Reader) (hash.Hash, int64, time.Duration, error) {
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

		file, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_RDWR, common.DefaultFileMode)
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

	start := time.Now()
	ba := make([]byte, bufferSize)

	if *loopTimeout > 0 {
		reader = common.NewTimeoutReader(reader, false, common.MillisecondToDuration(*loopTimeout))
	}

	var cw *consoleWriter

	verboseOutput := io.Discard
	if *data {
		cw = &consoleWriter{}

		verboseOutput = cw
	}

	var n int64

	if *hl7 {

		scanner := bufio.NewScanner(reader)
		scanner.Buffer(ba, len(ba))

		sf, err := common.NewSeparatorSplitFunc(HL7Start, HL7End, true)
		if common.Error(err) {
			return nil, 0, 0, err
		}
		scanner.Split(sf)

		for scanner.Scan() {
			ba := scanner.Bytes()

			common.Info("-- new message -----------------------------------------------")
			common.Info(string(ba))
		}
	} else {
		var err error
		n, err = io.CopyBuffer(io.MultiWriter(hasher, writer, verboseOutput), reader, ba)
		if !common.IsErrTimeout(err) && !common.IsErrNetClosed(err) {
			common.WarnError(err)
		}
	}

	d := time.Since(start)

	timeoutReader, ok := reader.(*common.TimeoutReader)
	if ok {
		d = time.Since(timeoutReader.FirstRead)
	}

	if cw != nil && !cw.HasEndedWithCRLF {
		fmt.Printf("\n")
	}

	return hasher, n, d, nil
}

func sendBytes(loop int, writer io.Writer) (hash.Hash, int64, time.Duration, error) {
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

			common.Info("Sending file content: %v %s ...", filename, common.FormatMemory(uint64(filesize)))
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
		reader = common.NewTimeoutReader(reader, true, common.MillisecondToDuration(*loopTimeout))
	}

	if length > 0 {
		reader = common.NewSizedReader(reader, length)
	}

	start := time.Now()

	n, err := io.CopyBuffer(io.MultiWriter(hasher, writer), reader, ba)
	if !common.IsErrTimeout(err) && !common.IsErrNetClosed(err) {
		common.WarnError(err)
	}

	d := time.Since(start)

	return hasher, n, d, nil
}

func sendMessage(loop int, writer io.Writer) error {
	for _, filename := range filenames {
		common.Info("Sending message file content: %v ...", filename)

		ba, err := os.ReadFile(filename)
		if common.Error(err) {
			return err
		}

		if *prefix != "" {
			fix, err := hex.DecodeString(*prefix)
			if common.Error(err) {
				return err
			}

			_, err = writer.Write(fix)
			if common.Error(err) {
				return err
			}
		}

		_, err = writer.Write(ba)
		if common.Error(err) {
			return err
		}

		if *suffix != "" {
			fix, err := hex.DecodeString(*suffix)
			if common.Error(err) {
				return err
			}

			_, err = writer.Write(fix)
			if common.Error(err) {
				return err
			}
		}
	}

	return nil
}

func calcPerformance(n int64, d time.Duration) string {
	if d > 0 {
		bytesPerSecond := int64(math.Round(float64(n) / d.Seconds()))

		return fmt.Sprintf("%s/%.2fs or %s/%v", common.FormatMemory(uint64(n)), d.Seconds(), common.FormatMemory(uint64(bytesPerSecond)), time.Second)
	} else {
		return fmt.Sprintf("%s/%.2fs", common.FormatMemory(uint64(n)), d.Seconds())
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
		if *suffix != "" {
			err := sendMessage(loop, connection)
			if common.Error(err) {
				return err
			}
		} else {
			hasher, n, duration, err := sendBytes(loop, connection)
			if common.Error(err) {
				return err
			}

			common.Info("Bytes sent: %d bytes, about %s", n, calcPerformance(n, duration))

			closeHasher(loop, hasher)
		}
	}

	if mustReceiveData() {
		if *suffix != "" {
			err := readMessage(loop, connection)
			if common.Error(err) {
				return err
			}
		} else {
			hasher, n, duration, err := readBytes(loop, connection)
			if common.Error(err) {
				return err
			}

			common.Info("Bytes received: %d bytes, about %s", n, calcPerformance(n, duration))

			closeHasher(loop, hasher)
		}
	}

	return nil
}

func start() error {
	var err error

	if *hl7 {
		*prefix = hex.EncodeToString(HL7Start)
		*suffix = hex.EncodeToString(HL7End)
	}

	if *prefix != "" && *suffix == "" {
		*suffix = *prefix
		*prefix = ""
	}

	if mustReceiveData() && *suffix != "" && len(filenames) > 0 {
		if len(filenames) > 1 || !common.FileExists(filenames[0]) || !common.IsDirectory(filenames[0]) {
			return common.TraceError(fmt.Errorf("flag filename %s mustd define only 1 existing directory", filenames[0]))
		}
	}

	bufferSize, err = common.ParseMemory(*bufferSizeString)
	if common.Error(err) {
		return err
	}

	if common.IsTTYDevice(*server) || common.IsTTYDevice(*client) {
		bufferSize = int64(min(1024, int(bufferSize)))
	}

	common.Info("Buffer size: %s", common.FormatMemory(uint64(bufferSize)))

	if mustReceiveData() {
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

	if *loopCount == 0 && *suffix == "" {
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
		if *loopCount != 1 {
			common.Info("")
			common.Info("Loop #%v", loop+1)
		}

		err = work(loop, connector)
		if common.Error(err) {
			return err
		}

		if mustSendData() && *loopSleep > 0 && ((*loopCount == 0) || ((loop + 1) < *loopCount)) {
			common.Info("Loop sleep: %v", common.MillisecondToDuration(*loopSleep))

			common.Sleep(common.MillisecondToDuration(*loopSleep))
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
	common.Run([]string{"c|s"})
}
