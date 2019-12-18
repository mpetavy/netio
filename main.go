package main

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/tls"
	"flag"
	"fmt"
	"github.com/mpetavy/common"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/signal"
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
	useHash          *bool
	random           *bool
)

type ZeroReader struct {
	deadline time.Time
}

func (this ZeroReader) Read(p []byte) (n int, err error) {
	for i := range p {
		p[i] = 0
	}

	if time.Now().After(this.deadline) {
		return 0, io.EOF
	}

	return len(p), nil
}

type RandomReader struct {
	deadline time.Time
}

func (this RandomReader) Read(p []byte) (n int, err error) {
	for i := range p {
		p[i] = byte(common.Rnd(256))
	}

	if time.Now().After(this.deadline) {
		return 0, io.EOF
	}

	return len(p), nil
}

func init() {
	common.Init("1.0.0", "2019", "network performance testing tool", "mpetavy", common.APACHE, false, nil, nil, run, 0)

	client = flag.String("c", "", "client socket address to read from")
	server = flag.String("s", "", "server socket server to listen")
	useTls = flag.Bool("tls", false, "use tls")
	useTlsClientAuth = flag.Bool("tlsclientauth", false, "use tls")
	benchmark = flag.Bool("b", true, "benchmark (true) or transfer (false)")
	useHash = flag.Bool("h", false, "hash transfer")
	random = flag.Bool("r", false, "random bytes")
	size = flag.String("bs", "1M", "blocksize")
	count = flag.Int("lc", 10, "loop count")
	timeout = flag.Int("t", common.DurationToMsec(time.Second), "block timeout")
}

func process(ctx context.Context, cancel context.CancelFunc) error {
	blockSize, err := common.ParseMemory(*size)
	if common.Error(err) {
		return err
	}

	var socket net.Conn
	var tlsPackage *common.TLSPackage
	var listener net.Listener

	hash := md5.New()

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
			common.Info("Block size: %s = %d bytes", *size, blockSize)
			common.Info("Loop count: %d", *count)
			common.Info("Timeout: %v", common.MsecToDuration(*timeout))
			if *random {
				common.Info("Randonm bytes")
			} else {
				common.Info("Zero bytes")
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
					return nil
				}
			}
		}

		common.Info("Successfully connected: %s", socket.RemoteAddr().String())

		if *benchmark {
			if *server != "" {
				if *useHash {
					common.Ignore(io.Copy(hash, socket))
				} else {
					common.Ignore(io.Copy(ioutil.Discard, socket))
				}
			} else {
				ba := make([]byte, blockSize)

				var n int64
				var err error
				var sum int64

				for i := 0; i < *count; i++ {
					deadline := time.Now().Add(common.MsecToDuration(*timeout))
					n = -1

					if *useHash {
						if *random {
							n, err = io.CopyBuffer(io.MultiWriter(socket, hash), RandomReader{
								deadline: deadline,
							}, ba)
						} else {
							n, err = io.CopyBuffer(io.MultiWriter(socket, hash), ZeroReader{
								deadline: deadline,
							}, ba)
						}
					} else {
						if *random {
							n, err = io.CopyBuffer(socket, RandomReader{
								deadline: deadline,
							}, ba)
						} else {
							n, err = io.CopyBuffer(socket, ZeroReader{
								deadline: deadline,
							}, ba)
						}
					}

					if err != nil {
						if neterr, ok := err.(net.Error); !ok || !neterr.Timeout() {
							return err
						}
					}

					common.Info("#%d Bytes written: %s", i, common.FormatMemory(int(n)))
					sum += n
				}
				common.Info("Average bytes written: %s", common.FormatMemory(int(sum/int64(*count))))
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
			if *useHash {
				common.Info("MD5: %x", hash.Sum(nil))
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
