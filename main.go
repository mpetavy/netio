package main

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"crypto/tls"
	"flag"
	"fmt"
	"github.com/mpetavy/common"
	"hash"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var (
	client           *string
	server           *string
	filename         *string
	useTls           *bool
	useTlsClientAuth *bool
	blocksizeString  *string
	limitsizeString  *string
	count            *int
	timeout          *int
	hashAlg          *string
	random           *bool
)

func init() {
	common.Init("1.0.0", "2019", "network performance testing tool", "mpetavy", common.APACHE, false, nil, nil, run, 0)

	client = flag.String("c", "", "client socket address to read from")
	server = flag.String("s", "", "server socket server to listen")
	filename = flag.String("f", "", "filename to send/ to receive")
	useTls = flag.Bool("tls", false, "use tls")
	useTlsClientAuth = flag.Bool("tlsclientauth", false, "use tls")
	hashAlg = flag.String("h", "", "hash algorithm")
	random = flag.Bool("r", false, "random bytes")
	blocksizeString = flag.String("bs", "32K", "block size in bytes")
	limitsizeString = flag.String("l", "0", "limit bytes/sec")
	count = flag.Int("lc", 10, "loop count")
	timeout = flag.Int("t", common.DurationToMsec(time.Second), "block timeout")
}

type ZeroReader struct {
}

func NewZeroReader() *ZeroReader {
	return &ZeroReader{}
}

func (this ZeroReader) Read(p []byte) (n int, err error) {
	for i := range p {
		p[i] = 0
	}

	return len(p), nil
}

type RandomReader struct {
	template [256]byte
}

func NewRandomReader() *RandomReader {
	r := RandomReader{}

	for i := range r.template {
		r.template[i] = byte(common.Rnd(256))
	}

	return &r
}

func (this RandomReader) Read(p []byte) (n int, err error) {
	copy(p, this.template[:])

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

func process(ctx context.Context, cancel context.CancelFunc) error {
	blockSize, err := common.ParseMemory(*blocksizeString)
	if common.Error(err) {
		return err
	}

	limitSize, err := common.ParseMemory(*limitsizeString)
	if common.Error(err) {
		return err
	}

	var socket net.Conn
	var tlsPackage *common.TLSPackage
	var listener net.Listener

	if *server != "" {
		if *useTls {
			tlsPackage, err = common.GetTLSPackage()
			if common.Error(err) {
				return err
			}

			if *useTlsClientAuth {
				tlsPackage.Config.ClientAuth = tls.RequireAndVerifyClientCert
			}

			listener, err = tls.Listen("tcp", *server, &tlsPackage.Config)
			if common.Error(err) {
				return err
			}
		} else {
			listener, err = net.Listen("tcp", *server)
			if common.Error(err) {
				return err
			}
		}
	}
	var reader io.Reader

	if *random {
		reader = NewRandomReader()
	} else {
		reader = NewZeroReader()
	}

	for {
		if *server != "" {
			common.Info("Accept connection: %s...", *server)

			socketCh := make(chan net.Conn)
			socket = nil

			go func() {
				var err error
				var s net.Conn

				s, err = listener.Accept()
				common.WarnError(err)
				if s != nil {
					socketCh <- s
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
			common.Info("Block size: %s = %d Bytes", *blocksizeString, blockSize)
			common.Info("Limit bytes/sec: %s = %d Bytes", *limitsizeString, limitSize)
			common.Info("Loop count: %d", *count)
			common.Info("Timeout: %v", common.MsecToDuration(*timeout))

			if *filename != "" {
				b, _ := common.FileExists(*filename)

				if !b {
					return &common.ErrFileNotFound{*filename}
				}

				common.Info("Sending file: %s", *filename)
			} else {
				if *random {
					common.Info("Sending: Random Bytes")
				} else {
					common.Info("Sending: Zero Bytes")
				}
			}

			if *useTls {
				config := &tls.Config{
					InsecureSkipVerify: true,
				}

				common.Info("Dial TLS connection: %s...", *client)

				socket, err = tls.Dial("tcp", *client, config)
				if common.Error(err) {
					return err
				}
			} else {
				common.Info("Dial connection: %s...", *client)

				socket, err = net.Dial("tcp", *client)
				if common.Error(err) {
					return err
				}
			}
		}

		common.Info("Connected: %s", socket.RemoteAddr().String())

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

						common.WarnError(file.Close())
					}()
				}

				if hasher != nil {
					common.Ignore(io.Copy(io.MultiWriter(f, hasher), socket))
				} else {
					common.Ignore(io.Copy(io.MultiWriter(f, ioutil.Discard), socket))
				}

				common.Ignore(io.Copy(hasher, socket))

				endSession(socket, hasher)
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

			ba := make([]byte, blockSize)

			var n int64
			var err error
			var sum int64

			var writer io.Writer

			if hasher != nil {
				writer = io.MultiWriter(socket, hasher)
			} else {
				writer = socket
			}

			writer = common.NewThrottledWriter(writer, int(limitSize))

			if *filename != "" {
				f, err := os.Open(*filename)
				if err != nil {
					return err
				}

				start := time.Now()

				n, err = io.Copy(writer, f)

				common.WarnError(f.Close())

				needed := time.Now().Sub(start)
				needed.Seconds()

				bytesPerSecond := int(float64(n) / needed.Seconds())

				common.Info("Average Bytes sent: %s", common.FormatMemory(bytesPerSecond))
			} else {
				for i := 0; i < *count; i++ {
					deadline := time.Now().Add(common.MsecToDuration(*timeout))
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

					common.Info("Loop #%d Bytes sent: %s", i, common.FormatMemory(int(n)))
					sum += n
				}

				common.Info("Average Bytes sent: %s", common.FormatMemory(int(sum/int64(*count))))
			}

			endSession(socket, hasher)
		}

		if *server == "" {
			break
		}
	}

	return nil
}

func run() error {
	ctrlC := make(chan os.Signal, 1)
	signal.Notify(ctrlC, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())

	var err error

	go func() {
		err = process(ctx, cancel)
		cancel()
	}()

	select {
	case <-ctx.Done():
		common.Error(err)
	case <-ctrlC:
		common.Info("Terminate: CTRL-C pressed")
		cancel()
	}

	return err
}

func main() {
	defer common.Done()

	common.Run(nil)
}
