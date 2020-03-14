package main

import (
	"crypto/md5"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/mpetavy/common"
	"hash"
	"io"
	"io/ioutil"
	"net"
	"os"
	"strings"
	"time"
)

var (
	client              *string
	server              *string
	filename            *string
	useTls              *bool
	useTlsInfo          *bool
	useTlsVerify        *bool
	blocksizeString     *string
	readThrottleString  *string
	writeThrottleString *string
	loopCount           *int
	timeout             *int
	hashAlg             *string
	random              *bool
)

func init() {
	common.Init("1.0.0", "2019", "network performance testing tool", "mpetavy", fmt.Sprintf("https://github.com/mpetavy/%s", common.Title()), common.APACHE, false, nil, nil, run, 0)

	client = flag.String("c", "", "client socket address to read from")
	server = flag.String("s", "", "server socket server to listen")
	filename = flag.String("f", "", "filename to send/ to receive")
	useTls = flag.Bool("tls", false, "use TLS")
	useTlsInfo = flag.Bool("tls-info", false, "show TLS info")
	useTlsVerify = flag.Bool("tls-verify", false, "TLS server verification/client verification")
	hashAlg = flag.String("h", "", "hash algorithm")
	random = flag.Bool("r", false, "random bytes")
	blocksizeString = flag.String("bs", "32K", "block size in bytes")
	readThrottleString = flag.String("tr", "0", "READ throttle bytes/sec")
	writeThrottleString = flag.String("tw", "0", "WRITE Throttle bytes/sec")
	loopCount = flag.Int("lc", 10, "loop count")
	timeout = flag.Int("t", common.DurationToMsec(time.Second), "block timeout")
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
		common.Fatal(fmt.Errorf("unknown hash algorithm: %s", *hashAlg))
	}

	return hasher
}

func endSession(socket net.Conn, hasher hash.Hash) {
	if socket != nil {
		common.Info("Disconnect: %s", socket.RemoteAddr().String())
		if hasher != nil {
			common.Info("%s: %x", strings.ToUpper(*hashAlg), hasher.Sum(nil))
		}

		common.Ignore(socket.Close())
	}
}

func run() error {
	blockSize, err := common.ParseMemory(*blocksizeString)
	if common.Error(err) {
		return err
	}

	readThrottle, err := common.ParseMemory(*readThrottleString)
	if common.Error(err) {
		return err
	}

	writeThrottle, err := common.ParseMemory(*writeThrottleString)
	if common.Error(err) {
		return err
	}

	var socket net.Conn
	var tlsSocket *tls.Conn
	var tcpListener *net.TCPListener
	var listener net.Listener

	if *server != "" {
		if *useTls {
			_, tlsPackage, err := common.GetTlsPackage()
			if common.Error(err) {
				return err
			}

			if *useTlsVerify {
				tlsPackage.Config.ClientAuth = tls.RequireAndVerifyClientCert
			}

			listener, err = tls.Listen("tcp", *server, &tlsPackage.Config)
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
		common.Info("Block size: %s = %d Bytes", *blocksizeString, blockSize)
		common.Info("READ throttle bytes/sec: %s = %d Bytes", *readThrottleString, readThrottle)
		common.Info("WRITE throttle bytes/sec: %s = %d Bytes", *writeThrottleString, writeThrottle)

		if *server != "" {
			ips, err := common.GetActiveAddrs(true)
			if common.Error(err) {
				return err
			}

			common.Info("Local IPs: %v", ips)
			common.Info("Accept connection: %s...", *server)

			socketCh := make(chan net.Conn)
			socket = nil

			go func() {

				if *useTls {
					s, err := listener.Accept()
					common.Error(err)

					tlsConn, ok := s.(*tls.Conn)
					if ok {
						err := tlsConn.Handshake()

						if common.Error(err) {
							common.Error(s.Close())
						}
					}

					if s != nil {
						socketCh <- s
					}
				} else {
					s, err := tcpListener.AcceptTCP()
					common.Error(err)

					if s != nil {
						socketCh <- s
					}
				}
			}()

			for socket == nil {
				select {
				case <-time.After(time.Millisecond * 250):
					continue
				case socket = <-socketCh:
					break
				}
			}
		} else {
			if *filename != "" {
				b, _ := common.FileExists(*filename)

				if !b {
					return &common.ErrFileNotFound{*filename}
				}

				common.Info("Sending file: %s", *filename)
			} else {
				common.Info("Timeout: %v", common.MillisecondToDuration(*timeout))

				if *useTls && *loopCount > 1 {
					*loopCount = 1
					common.Info("As of TLS connection loop count is reset to 1")
				} else {
					common.Info("Loop count: %d", *loopCount)
				}

				if *random {
					common.Info("Sending: Random Bytes")
				} else {
					common.Info("Sending: Zero Bytes")
				}
			}

			if *useTls {
				_, tlsPackage, err := common.GetTlsPackage()
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

				socket, err = tls.Dial("tcp", *client, &tlsPackage.Config)
				if common.Error(err) {
					return err
				}

				var ok bool

				tlsSocket, ok = socket.(*tls.Conn)
				if ok {
					if *useTlsInfo {
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

				socket = tcpCon
			}
		}

		common.Info("Connected: %s", socket.RemoteAddr().String())

		ba := make([]byte, blockSize)

		if *server != "" {
			go func(socket net.Conn) {
				hasher := startSession()

				var f io.Writer

				f = ioutil.Discard

				if *filename != "" {
					var file *os.File

					common.Info("Create file: %s", *filename)

					file, err = os.Create(*filename)
					if err != nil {
						common.Error(err)
						return
					}

					f = file

					defer func() {
						common.Info("Close file: %s", *filename)

						common.Error(file.Close())
					}()
				}

				reader := common.NewThrottledReader(socket, int(readThrottle))
				start := time.Now()

				var n int64

				if hasher != nil {
					n, _ = io.CopyBuffer(io.MultiWriter(f, hasher), reader, ba)
				} else {
					n, _ = io.CopyBuffer(io.MultiWriter(f, ioutil.Discard), reader, ba)
				}

				end := time.Now()

				endSession(socket, hasher)

				d := end.Sub(start)

				common.Info("Average Bytes received: %s", common.FormatMemory(int(float64(n)/float64(d.Milliseconds()))))
			}(socket)
		} else {
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
				common.Fatal(fmt.Errorf("unknown hash algorithm: %s", *hashAlg))
			}

			var n int64
			var err error
			var sum float64

			var writer io.Writer

			if hasher != nil {
				writer = io.MultiWriter(socket, hasher)
			} else {
				writer = socket
			}

			writer = common.NewThrottledWriter(writer, int(writeThrottle))

			if *filename != "" {
				f, err := os.Open(*filename)
				if err != nil {
					return err
				}

				start := time.Now()
				reader := common.NewThrottledReader(f, int(readThrottle))

				n, err = io.CopyBuffer(writer, reader, ba)

				common.Error(f.Close())

				needed := time.Now().Sub(start)

				if needed.Seconds() >= 1 {
					bytesPerSecond := int(float64(n) / needed.Seconds())

					common.Info("Average Bytes sent: %s/%v", common.FormatMemory(bytesPerSecond), common.MillisecondToDuration(*timeout))
				} else {
					common.Info("Bytes sent: %s/%v", common.FormatMemory(int(n)), needed)
				}
			} else {
				var reader io.Reader

				if *random {
					reader = common.NewRandomReader()
				} else {
					reader = common.NewZeroReader()
				}

				reader = common.NewThrottledReader(reader, int(readThrottle))

				for i := 0; i < *loopCount; i++ {
					deadline := time.Now().Add(common.MillisecondToDuration(*timeout))
					err = socket.SetDeadline(deadline)
					if err != nil {
						return err
					}

					n, err = io.CopyBuffer(writer, reader, ba)

					if err != nil {
						if neterr, ok := err.(net.Error); !ok || !neterr.Timeout() {
							return err
						}
					}

					if *loopCount > 1 {
						common.Info("Loop #%d Bytes sent: %s/%v", i, common.FormatMemory(int(n)), common.MillisecondToDuration(*timeout))
					} else {
						common.Info("Bytes sent: %s/%v", common.FormatMemory(int(n)), common.MillisecondToDuration(*timeout))
					}
					sum += float64(n)
				}

				common.Info("Average Bytes sent: %s/%v", common.FormatMemory(int(sum/float64(*loopCount))), common.MillisecondToDuration(*timeout))
			}

			endSession(socket, hasher)
		}

		if *server == "" {
			break
		}
	}

	return nil
}

func main() {
	defer common.Done()

	common.Run(nil)
}
