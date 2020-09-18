package main

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"go.bug.st/serial"
	"hash"
	"io"
	"io/ioutil"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mpetavy/common"
)

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
	showTlsInfo         *bool
	useTlsVerify        *bool
	isDataSender        *bool
	isDataReceiver      *bool
	blocksizeString     *string
	readThrottleString  *string
	writeThrottleString *string
	loopCount           *int
	loopTimeout         *int
	serialTimeout       *int
	helloTimeout        *int
	hashAlg             *string
	hashExpected        *string
	randomBytes         *bool
	loopSleep           *int
	blockSize           int64
	readThrottle        int64
	writeThrottle       int64
	connection          io.ReadWriteCloser
)

const (
	HELLO = "###HELLO###"
)

func init() {
	common.Init(false, LDFLAG_VERSION, "2019", "network/serial performance testing tool", LDFLAG_DEVELOPER, LDFLAG_HOMEPAGE, LDFLAG_LICENSE, nil, nil, run, 0)

	client = flag.String("c", "", "Client address/serial port")
	server = flag.String("s", "", "Server address/serial port")
	filename = flag.String("f", "", "Filename to write to (server) or read from (client)")
	useTls = flag.Bool("tls", false, "Use TLS")
	showTlsInfo = flag.Bool("tls.info", false, "Show TLS info")
	useTlsVerify = flag.Bool("tls.verify", false, "TLS verification verification")
	isDataSender = flag.Bool("ds", false, "Act as data sender")
	isDataReceiver = flag.Bool("dr", false, "Act as data receiver")
	hashAlg = flag.String("h", "", "Hash algorithm (md5, sha224, sha256)")
	hashExpected = flag.String("e", "", "Expected hash")
	randomBytes = flag.Bool("r", false, "Send random bytes (or '0' bytes)")
	blocksizeString = flag.String("bs", "32K", "Block size in bytes")
	readThrottleString = flag.String("rt", "0", "Read throttled bytes/sec")
	writeThrottleString = flag.String("wt", "0", "Write throttled bytes/sec")
	loopCount = flag.Int("lc", 1, "Loop count")
	loopTimeout = flag.Int("lt", common.DurationToMillisecond(time.Second), "Loop timeout")
	loopSleep = flag.Int("ls", 0, "Loop sleep between loop steps")
	serialTimeout = flag.Int("st", common.DurationToMillisecond(time.Second), "Serial read timeout for disconnect")
	helloTimeout = flag.Int("ht", common.DurationToMillisecond(time.Second), "Sender sleep time after HELLO and before send start")

}

type DeadlineReset struct {
}

func (this DeadlineReset) Write(p []byte) (n int, err error) {
	tlsConn, ok := connection.(*tls.Conn)
	if ok {
		common.Error(tlsConn.SetDeadline(time.Now().Add(common.MillisecondToDuration(*serialTimeout))))
	} else {
		conn, ok := connection.(*net.TCPConn)
		if ok {
			common.Error(conn.SetDeadline(time.Now().Add(common.MillisecondToDuration(*serialTimeout))))
		}
	}

	return len(p), nil
}

func startSession() hash.Hash {
	var hasher hash.Hash

	switch *hashAlg {
	case "":
	case "md5":
		hasher = md5.New()
	case "sha224":
		hasher = sha256.New224()
	case "sha256":
		hasher = sha256.New()
	default:
		common.Error(fmt.Errorf("unknown hash algorithm: %s", *hashAlg))
	}

	return hasher
}

func endSession(hasher hash.Hash) {
	if hasher != nil {
		hashCalculated := fmt.Sprintf("%x", hasher.Sum(nil))

		if hasher != nil {
			common.Info("%s digest: %s", strings.ToUpper(*hashAlg), hashCalculated)

			if *hashExpected != "" {
				if *hashExpected != hashCalculated {
					common.Info("%s expect: %s", strings.ToUpper(*hashAlg), *hashExpected)
					common.Warn("%s hash is wrong!", strings.ToUpper(*hashAlg))
				} else {
					common.Info("%s hash is correct!", strings.ToUpper(*hashAlg))
				}
			}
		}

		common.DebugError(connection.Close())
	}
}

func isSerialPortOptions(txt string) bool {
	return len(txt) > 0 && (strings.Contains(txt, ",") || !strings.Contains(txt, ":"))
}

func asSocket(rwc io.ReadWriteCloser) net.Conn {
	socket, ok := rwc.(net.Conn)

	if !ok {
		return nil
	}

	return socket
}

func evaluateSerialPortOptions(txt string) (string, *serial.Mode, error) {
	ss := strings.Split(txt, ",")

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

func waitForHello() error {
	received := ""
	ba := make([]byte, len(HELLO))

	common.Info("Waiting for HELLO...")

	for {
		n, err := connection.Read(ba)
		if err != nil {
			return err
		}

		if n > 0 {
			received = received + string(ba[:n])

			if strings.HasSuffix(received, HELLO) {
				common.Info("Received HELLO")

				break
			}
		}
	}

	return nil
}

func sendHello() error {
	common.Info("Sending HELLO")
	_, err := io.Copy(connection, strings.NewReader(HELLO))
	if common.Error(err) {
		return err
	}

	return nil
}

func dataReceiver(device string) error {
	if *isDataReceiver {
		err := sendHello()
		if common.Error(err) {
			return err
		}
	}

	fileWriter := ioutil.Discard
	hashDigest := startSession()
	ba := make([]byte, blockSize)

	if *filename != "" {
		common.Info("Dump to file: %s", *filename)

		file, err := os.OpenFile(*filename, os.O_CREATE|os.O_APPEND|os.O_RDWR, os.ModePerm)
		if common.Error(err) {
			return err
		}

		fileWriter = file
	}

	common.Info("Receiving bytes...")

	reader := common.NewThrottledReader(connection, int(readThrottle))
	start := time.Now()

	var n int64

	if !isSerialPortOptions(device) {

		if hashDigest != nil {
			n, _ = io.CopyBuffer(io.MultiWriter(DeadlineReset{}, fileWriter, hashDigest), reader, ba)
		} else {
			n, _ = io.CopyBuffer(io.MultiWriter(DeadlineReset{}, fileWriter, ioutil.Discard), reader, ba)
		}
	} else {
		var writer io.Writer

		st := common.MillisecondToDuration(*serialTimeout)

		if hashDigest != nil {
			writer = io.MultiWriter(fileWriter, hashDigest)
		} else {
			writer = io.MultiWriter(fileWriter, ioutil.Discard)
		}

		timer := time.NewTimer(st)
		timer.Stop()

		isTimedout := false

		go func() {
			for common.AppLifecycle().IsSet() {
				nn, err := reader.Read(ba)

				if isTimedout {
					return
				}

				timer.Stop()

				if n == 0 {
					common.Info("Connected")
				}

				if common.Error(err) {
					return
				}

				_, err = writer.Write(ba[:nn])
				if common.Error(err) {
					return
				}

				n = n + int64(nn)

				timer.Reset(st)
			}
		}()

		for {
			<-timer.C
			isTimedout = true
			common.Error(connection.Close())
			time.Sleep(time.Millisecond * 250)
			break
		}
	}

	if asSocket(connection) != nil {
		common.Info("Disconnect: %s", asSocket(connection).RemoteAddr().String())
	} else {
		common.Info("Disconnect")
	}

	endSession(hashDigest)

	needed := time.Since(start)

	if needed.Seconds() >= 1 {
		bytesPerSecond := int(float64(n) / float64(needed.Seconds()))

		common.Info("Average Bytes received: %s/%v", common.FormatMemory(bytesPerSecond), common.MillisecondToDuration(*loopTimeout))
	} else {
		common.Info("Bytes received: %s/%v", common.FormatMemory(int(n)), needed)
	}

	if *filename != "" {
		common.Info("Close file: %s", *filename)

		file := fileWriter.(*os.File)

		common.DebugError(file.Close())
	}

	return nil
}

func dataSender(device string) error {
	if *isDataSender {
		err := waitForHello()
		if common.Error(err) {
			return err
		}

		time.Sleep(common.MillisecondToDuration(*helloTimeout))
	}

	if *filename != "" {
		common.Info("Send file: %s", *filename)
	} else {
		common.Info("Timeout: %v", common.MillisecondToDuration(*loopTimeout))
		common.Info("Loop count: %d", *loopCount)

		if *randomBytes {
			common.Info("Send: Random Bytes")
		} else {
			common.Info("Send: Zero Bytes")
		}
	}

	ba := make([]byte, blockSize)
	hashDigest := startSession()

	var n int64
	var sum float64
	var writer io.Writer

	if hashDigest != nil {
		writer = io.MultiWriter(connection, hashDigest)
	} else {
		writer = connection
	}

	writer = common.NewThrottledWriter(writer, int(writeThrottle))

	var fileBuffer []byte
	var reader io.Reader
	var err error

	if *filename != "" {
		fileBuffer, err = ioutil.ReadFile(*filename)
		if err != nil {
			return err
		}
	}

	for i := 0; i < *loopCount; i++ {
		if *filename != "" {
			reader = bytes.NewReader(fileBuffer)
		} else {
			if *randomBytes {
				reader = common.NewRandomReader()
			} else {
				reader = common.NewZeroReader()
			}
		}

		reader = common.NewThrottledReader(reader, int(readThrottle))

		deadline := time.Now().Add(common.MillisecondToDuration(*loopTimeout))

		if *useTls || isSerialPortOptions(device) {
			n = 0

			start := time.Now()
			for time.Now().Before(deadline) {
				blockN, blockErr := io.CopyN(writer, reader, blockSize)

				if blockErr != nil {
					err = blockErr
				}

				n += blockN
			}
			elapsed := time.Since(start)
			n = int64(float64(n) / float64(elapsed.Seconds()) * float64(time.Second.Seconds()))

			if err != nil && err != io.EOF && common.Error(err) {
				return err
			}
		} else {
			err = asSocket(connection).SetDeadline(deadline)
			if err != nil {
				return err
			}

			n, err = io.CopyBuffer(writer, reader, ba)

			if err != nil {
				if neterr, ok := err.(net.Error); !ok || !neterr.Timeout() {
					return err
				}
			}
		}

		if *loopCount > 1 {
			common.Info("Loop #%d Bytes sent: %s/%v", i, common.FormatMemory(int(n)), common.MillisecondToDuration(*loopTimeout))

			if *loopSleep > 0 {
				common.Info("Loop sleep timeout: %v", common.MillisecondToDuration(*loopSleep))

				time.Sleep(common.MillisecondToDuration(*loopSleep))
			}
		} else {
			common.Info("Bytes sent: %s/%v", common.FormatMemory(int(n)), common.MillisecondToDuration(*loopTimeout))
		}
		sum += float64(n)
	}

	common.Info("Average Bytes sent: %s/%v", common.FormatMemory(int(sum/float64(*loopCount))), common.MillisecondToDuration(*loopTimeout))

	if asSocket(connection) != nil {
		common.Info("Disconnect: %s", asSocket(connection).RemoteAddr().String())
	} else {
		common.Info("Disconnect")
	}

	endSession(hashDigest)

	return nil
}

func run() error {
	var err error

	blockSize, err = common.ParseMemory(*blocksizeString)
	if common.Error(err) {
		return err
	}

	//if isSerialPortOptions(*server) || isSerialPortOptions(*client) {
	//	blockSize = int64(common.Min(int(blockSize), 115200/8))
	//}

	common.Info("Block size: %s = %d Bytes", common.FormatMemory(int(blockSize)), blockSize)

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
		if *isDataSender || (*client != "" && !*isDataReceiver) {
			b, _ := common.FileExists(*filename)

			if !b {
				return &common.ErrFileNotFound{FileName: *filename}
			}
		}

		if *isDataReceiver || (*server != "" && !*isDataSender) {
			b, _ := common.FileExists(*filename)

			if b {
				return fmt.Errorf("file already exists: %s", *filename)
			}
		}
	}

	if *server != "" {
		var tcpListener *net.TCPListener
		var tlsListener net.Listener

		if !isSerialPortOptions(*server) {
			if *useTls {
				tlsPackage, err := common.GetTlsPackage()
				if common.Error(err) {
					return err
				}

				if *useTlsVerify {
					tlsPackage.Config.ClientAuth = tls.RequireAndVerifyClientCert
				}

				tlsListener, err = tls.Listen("tcp", *server, &tlsPackage.Config)
				if common.Error(err) {
					return err
				}
			} else {
				tcpAddr, err := net.ResolveTCPAddr("tcp", *server)
				if common.Error(err) {
					return err
				}

				tcpListener, err = net.ListenTCP("tcp", tcpAddr)
				if common.Error(err) {
					return err
				}
			}
		}

		for {
			if isSerialPortOptions(*server) {
				tty, mode, err := evaluateSerialPortOptions(*server)
				if common.Error(err) {
					return err
				}

				connection, err = serial.Open(tty, mode)
				if common.Error(err) {
					return err
				}
			} else {
				ips, err := common.GetHostAddrs(true, nil)
				if common.Error(err) {
					return err
				}

				common.Info("Local IPs: %v", ips)
				common.Info("Accept connection: %s...", *server)

				if *useTls {
					s, err := tlsListener.Accept()
					if common.Error(err) {
						return err
					}

					tlsConn, ok := s.(*tls.Conn)
					if ok {
						err := tlsConn.Handshake()

						if common.Error(err) {
							common.DebugError(s.Close())

							continue
						}
					}

					connection = s
				} else {
					s, err := tcpListener.AcceptTCP()
					if common.Error(err) {
						return err
					}

					connection = s
				}

				if asSocket(connection) != nil {
					common.Info("Connected: %s", asSocket(connection).RemoteAddr().String())
				} else {
					common.Info("Connected: %s", *server)
				}
			}

			if *isDataSender {
				common.Error(dataSender(*server))
			} else {
				common.Error(dataReceiver(*server))
			}
		}
	}

	if isSerialPortOptions(*client) {
		tty, mode, err := evaluateSerialPortOptions(*client)
		if common.Error(err) {
			return err
		}

		connection, err = serial.Open(tty, mode)
		if common.Error(err) {
			return err
		}
	} else {
		if *useTls {
			tlsPackage, err := common.GetTlsPackage()
			if common.Error(err) {
				return err
			}

			hostname, _, err := net.SplitHostPort(*client)
			if common.Error(err) {
				return err
			}

			if hostname == "" {
				hostname = "localhost"
			}

			// set hostname for self-signed cetificates
			tlsPackage.Config.ServerName = hostname
			tlsPackage.Config.InsecureSkipVerify = !*useTlsVerify

			common.Info("Dial TLS connection: %s...", *client)

			connection, err = tls.Dial("tcp", *client, &tlsPackage.Config)
			if common.Error(err) {
				return err
			}

			tlsSocket, ok := connection.(*tls.Conn)
			if ok {
				if *showTlsInfo {
					ba, err := json.MarshalIndent(tlsSocket.ConnectionState(), "", "    ")
					if common.Error(err) {
						return err
					}

					common.Info("TLS connection state: %s", string(ba))
				}

				if !tlsSocket.ConnectionState().HandshakeComplete {
					return fmt.Errorf("TLS handshake not completed")
				}
			}
		} else {
			common.Info("Dial connection: %s...", *client)

			tcpAddr, err := net.ResolveTCPAddr("tcp", *client)
			if common.Error(err) {
				return err
			}

			tcpCon, err := net.DialTCP("tcp", nil, tcpAddr)
			if common.Error(err) {
				return err
			}

			connection = tcpCon
		}
	}

	if asSocket(connection) != nil {
		common.Info("Connected: %s", asSocket(connection).RemoteAddr().String())
	} else {
		common.Info("Connected: %s", *client)
	}

	if *isDataReceiver {
		common.Error(dataReceiver(*client))
	} else {
		common.Error(dataSender(*client))
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
