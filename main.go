package main

import (
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
	client              *string
	server              *string
	filename            *string
	useTls              *bool
	showTlsInfo         *bool
	useTlsVerify        *bool
	blocksizeString     *string
	readThrottleString  *string
	writeThrottleString *string
	loopCount           *int
	loopTimeout         *int
	serialTimeout       *int
	hashAlg             *string
	randomBytes         *bool
	loopSleep           *int
)

func init() {
	common.Init(false, "1.0.0", "2019", "network/serial performance testing tool", "mpetavy", fmt.Sprintf("https://github.com/mpetavy/%s", common.Title()), common.APACHE, nil, nil, run, 0)

	client = flag.String("c", "", "client socket address or TTY port")
	server = flag.String("s", "", "server socket address")
	filename = flag.String("f", "", "filename to write (client)/read (server)")
	useTls = flag.Bool("tls", false, "use TLS")
	showTlsInfo = flag.Bool("tls-info", false, "show TLS info")
	useTlsVerify = flag.Bool("tls-verify", false, "TLS server verification/client verification")
	hashAlg = flag.String("h", "", "hash algorithm")
	randomBytes = flag.Bool("r", false, "write random bytes")
	blocksizeString = flag.String("bs", "32K", "block size in bytes")
	readThrottleString = flag.String("rt", "0", "read throttled bytes/sec")
	writeThrottleString = flag.String("wt", "0", "write throttled bytes/sec")
	loopCount = flag.Int("lc", 10, "loop count")
	loopTimeout = flag.Int("lt", common.DurationToMillisecond(time.Second), "loop timeout")
	loopSleep = flag.Int("ls", 0, "loop sleep timeout between loop steps")
	serialTimeout = flag.Int("st", common.DurationToMillisecond(time.Second), "serial read timeout for disconnect")
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

func endSession(socket io.ReadWriteCloser, hasher hash.Hash) {
	if socket != nil {
		if asSocket(socket) != nil {
			common.Info("Disconnect: %s", asSocket(socket).RemoteAddr().String())
		} else {
			common.Info("Disconnect")
		}

		if hasher != nil {
			common.Info("%s: %x", strings.ToUpper(*hashAlg), hasher.Sum(nil))
		}

		common.DebugError(socket.Close())
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

func run() error {
	blockSize, err := common.ParseMemory(*blocksizeString)
	if common.Error(err) {
		return err
	}

	if isSerialPortOptions(*client) {
		blockSize = int64(common.Min(int(blockSize), 115200/8))
	}

	readThrottle, err := common.ParseMemory(*readThrottleString)
	if common.Error(err) {
		return err
	}

	writeThrottle, err := common.ParseMemory(*writeThrottleString)
	if common.Error(err) {
		return err
	}

	var connection io.ReadWriteCloser

	common.Info("Block size: %s = %d Bytes", common.FormatMemory(int(blockSize)), blockSize)
	if readThrottle > 0 {
		common.Info("READ throttle bytes/sec: %s = %d Bytes", *readThrottleString, readThrottle)
	}
	if writeThrottle > 0 {
		common.Info("WRITE throttle bytes/sec: %s = %d Bytes", *writeThrottleString, writeThrottle)
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

			fileWriter := ioutil.Discard
			hashDigest := startSession()
			ba := make([]byte, blockSize)

			if *filename != "" {
				common.Info("Create file: %s", *filename)

				file, err := os.Create(*filename)
				if common.Error(err) {
					return err
				}

				fileWriter = file
			}

			reader := common.NewThrottledReader(connection, int(readThrottle))
			start := time.Now()

			var n int64

			if !isSerialPortOptions(*server) {
				if hashDigest != nil {
					n, _ = io.CopyBuffer(io.MultiWriter(fileWriter, hashDigest), reader, ba)
				} else {
					n, _ = io.CopyBuffer(io.MultiWriter(fileWriter, ioutil.Discard), reader, ba)
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

						//portError, ok := err.(*serial.PortError)
						//if ok && portError.Code() == serial.PortClosed {
						//	return
						//}

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
					break
				}
			}

			endSession(connection, hashDigest)

			needed := time.Since(start)

			if needed.Seconds() >= 1 {
				bytesPerSecond := int(float64(n) / float64(needed.Seconds()))

				common.Info("Average Bytes sent: %s/%v", common.FormatMemory(bytesPerSecond), common.MillisecondToDuration(*loopTimeout))
			} else {
				common.Info("Bytes sent: %s/%v", common.FormatMemory(int(n)), needed)
			}

			if *filename != "" {
				common.Info("Close file: %s", *filename)

				file := fileWriter.(*os.File)

				common.DebugError(file.Close())
			}
		}
	}

	if *filename != "" {
		b, _ := common.FileExists(*filename)

		if !b {
			return &common.ErrFileNotFound{FileName: *filename}
		}

		common.Info("Sending file: %s", *filename)
	} else {
		common.Info("Timeout: %v", common.MillisecondToDuration(*loopTimeout))
		common.Info("Loop count: %d", *loopCount)

		if *randomBytes {
			common.Info("Sending: Random Bytes")
		} else {
			common.Info("Sending: Zero Bytes")
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

	if *filename != "" {
		fileReader, err := os.Open(*filename)
		if err != nil {
			return err
		}

		start := time.Now()
		reader := common.NewThrottledReader(fileReader, int(readThrottle))

		n, err = io.CopyBuffer(writer, reader, ba)
		if err != nil {
			return err
		}

		common.DebugError(fileReader.Close())

		needed := time.Since(start)

		if needed.Seconds() >= 1 {
			bytesPerSecond := int(float64(n) / needed.Seconds())

			common.Info("Average Bytes sent: %s/%v", common.FormatMemory(bytesPerSecond), common.MillisecondToDuration(*loopTimeout))
		} else {
			common.Info("Bytes sent: %s/%v", common.FormatMemory(int(n)), needed)
		}
	} else {
		var reader io.Reader

		if *randomBytes {
			reader = common.NewRandomReader()
		} else {
			reader = common.NewZeroReader()
		}

		reader = common.NewThrottledReader(reader, int(readThrottle))

		for i := 0; i < *loopCount; i++ {
			deadline := time.Now().Add(common.MillisecondToDuration(*loopTimeout))

			if *useTls || isSerialPortOptions(*client) {
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

				if common.Error(err) {
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
					common.Info("intermediate sleep timeout: %v", common.MillisecondToDuration(*loopSleep))

					time.Sleep(common.MillisecondToDuration(*loopSleep))
				}
			} else {
				common.Info("Bytes sent: %s/%v", common.FormatMemory(int(n)), common.MillisecondToDuration(*loopTimeout))
			}
			sum += float64(n)
		}

		common.Info("Average Bytes sent: %s/%v", common.FormatMemory(int(sum/float64(*loopCount))), common.MillisecondToDuration(*loopTimeout))
	}

	endSession(connection, hashDigest)

	return nil
}

func main() {
	defer common.Done()

	common.Run(nil)
}
