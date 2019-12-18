package main

import (
	"context"
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
	blockSizeString  *string
	loopCount        *int
	benchmark        *bool
	random           *bool
)

type ZeroReader struct {
}

func (this ZeroReader) Read(p []byte) (n int, err error) {
	for i := 0; i < len(p); i++ {
		p[i] = 0
	}

	return len(p), nil
}

type RandomReader struct {
}

func (this RandomReader) Read(p []byte) (n int, err error) {
	for i := 0; i < len(p); i++ {
		p[i] = byte(common.Rnd(256))
	}

	return len(p), nil
}

func init() {
	common.Init("1.0.0", "2019", "net testing tool", "mpetavy", common.APACHE, false, nil, nil, run, 0)

	client = flag.String("c", "", "client socket address to read from")
	server = flag.String("s", "", "server socket server to listen")
	useTls = flag.Bool("tls", false, "use tls")
	useTlsClientAuth = flag.Bool("tlsclientauth", false, "use tls")
	benchmark = flag.Bool("b", true, "benchmark (true) or transfer (false)")
	random = flag.Bool("r", false, "random bytes")
	blockSizeString = flag.String("bs", "1M", "blocksize")
	loopCount = flag.Int("lc", 10, "loop count")
}

func process(ctx context.Context, cancel context.CancelFunc) error {
	blockSize, err := common.ParseMemory(*blockSizeString)
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
		}

		if tlsPackage != nil {
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

			for socket == nil {
				tcplistener := listener.(*net.TCPListener)
				err := tcplistener.SetDeadline(time.Now().Add(time.Millisecond * 250))

				socket, err = listener.Accept()
				if err != nil {
					if neterr, ok := err.(net.Error); ok && neterr.Timeout() {
						continue
					}

					return err
				}
			}
		} else {
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
				io.Copy(ioutil.Discard, socket)
			} else {
				ba := make([]byte, blockSize)

				common.Info("Block size: %s = %d bytes", *blockSizeString, blockSize)
				common.Info("Loop count: %d", *loopCount)

				var n int64
				var err error

				for i := 0; i < *loopCount; i++ {
					err = socket.SetDeadline(time.Now().Add(time.Second))
					if common.Error(err) {
						panic(err)
					}

					n = -1

					if *random {
						//_, err = common.CopyWithContext(ctx, cancel, "read from socket", socket, RandomReader{}, int(blockSize))
						n, err = io.CopyBuffer(socket, RandomReader{}, ba)
					} else {
						//_, err = common.CopyWithContext(ctx, cancel, "read from socket", socket, ZeroReader{}, int(blockSize))
						n, err = io.CopyBuffer(socket, ZeroReader{}, ba)
					}

					if err != nil {
						if neterr, ok := err.(net.Error); !ok || !neterr.Timeout() {
							return nil
						}
					}

					common.Info("#%d Bytes written: %s", i, common.FormatMemory(int(n)))
				}
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

			err := socket.Close()
			if common.Error(err) {
				panic(err)
			}
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
