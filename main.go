package main

import (
	"context"
	"crypto/md5"
	"crypto/rand"
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
	useTls           *bool
	useTlsClientAuth *bool
	size             *string
	count            *int
	timeout          *int
	benchmark        *bool
	hashAlg          *string
	random           *bool

	deadline time.Time
)

func init() {
	common.Init("1.0.0", "2019", "network performance testing tool", "mpetavy", common.APACHE, false, nil, nil, run, 0)

	client = flag.String("c", "", "client socket address to read from")
	server = flag.String("s", "", "server socket server to listen")
	useTls = flag.Bool("tls", false, "use tls")
	useTlsClientAuth = flag.Bool("tlsclientauth", false, "use tls")
	benchmark = flag.Bool("b", true, "benchmark (true) or transfer (false)")
	hashAlg = flag.String("h", "", "hash algorithm")
	random = flag.Bool("r", false, "random bytes")
	size = flag.String("bs", "32K", "blocksize")
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

	if time.Now().After(deadline) {
		return 0, io.EOF
	}

	return len(p), nil
}

type RandomReader struct {
	template [256]byte
}

func NewRandomReadet() *RandomReader {
	r := RandomReader{}

	for i := range r.template {
		r.template[i] = byte(common.Rnd(256))
	}

	return &r
}

func (this RandomReader) Read(p []byte) (n int, err error) {
	copy(p, this.template[:])

	if time.Now().After(deadline) {
		return 0, io.EOF
	}

	return len(p), nil
}

func process(ctx context.Context, cancel context.CancelFunc) error {
	blockSize, err := common.ParseMemory(*size)
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
		reader = NewRandomReadet()
	} else {
		reader = NewZeroReader()
	}

	for {
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
			return fmt.Errorf("unknown hash algorithm: %s", *hashAlg)
		}

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
			common.Info("Block size: %s = %d Bytes", *size, blockSize)
			common.Info("Loop count: %d", *count)
			common.Info("Timeout: %v", common.MsecToDuration(*timeout))
			if *random {
				common.Info("Randonm Bytes")
			} else {
				common.Info("Zero Bytes")
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

		common.Info("Successfully connected: %s", socket.RemoteAddr().String())

		if *benchmark {
			if *server != "" {
				if hasher != nil {
					common.Ignore(io.Copy(hasher, socket))
				} else {
					common.Ignore(io.Copy(ioutil.Discard, socket))
				}
			} else {
				ba := make([]byte, blockSize)

				var n int64
				var err error
				var sum int64

				for i := 0; i < *count; i++ {
					deadline = time.Now().Add(common.MsecToDuration(*timeout))
					n = -1

					if hasher != nil {
						n, err = io.CopyBuffer(io.MultiWriter(socket, hasher), reader, ba)
					} else {
						n, err = io.CopyBuffer(socket, reader, ba)
					}

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
		} else {
			fmt.Printf("copy\n")
			go func() {
				_, err = common.CopyWithContext(ctx, cancel, "read from socket", os.Stdout, socket, -1)
				if common.Error(err) {
					panic(err)
				}
			}()
			go func() {
				_, err = common.CopyWithContext(ctx, cancel, "writer to socket", socket, rand.Reader, -1)
				if common.Error(err) {
					panic(err)
				}
			}()

			select {
			case <-ctx.Done():
			}
		}

		if socket != nil {
			common.Info("Disconnect: %s", socket.RemoteAddr().String())
			if hasher != nil {
				common.Info("%s: %x", strings.ToUpper(*hashAlg), hasher.Sum(nil))
			}

			common.Ignore(socket.Close())
			socket = nil
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
