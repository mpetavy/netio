package endpoint

import (
	"crypto/tls"
	"fmt"
	"github.com/mpetavy/common"
	"io"
	"net"
)

type NetworkConnection struct {
	socket io.ReadWriteCloser
}

func (networkConnection *NetworkConnection) Read(p []byte) (n int, err error) {
	return networkConnection.socket.Read(p)
}

func (networkConnection *NetworkConnection) Write(p []byte) (n int, err error) {
	return networkConnection.socket.Write(p)
}

func (networkConnection *NetworkConnection) Close() error {
	defer func() {
		networkConnection.socket = nil
	}()

	if networkConnection.socket != nil {
		err := networkConnection.socket.Close()
		if common.Error(err) {
			return err
		}
	}

	return nil
}

type NetworkClient struct {
	device       string
	useTls       bool
	useTlsVerify bool
}

func NewNetworkClient(device string, useTls bool, useTlsVerify bool) (*NetworkClient, error) {
	networkClient := &NetworkClient{
		device:       device,
		useTls:       useTls,
		useTlsVerify: useTlsVerify,
	}

	networkClient.device = device
	networkClient.useTls = useTls
	networkClient.useTlsVerify = useTlsVerify

	return networkClient, nil
}

func (networkClient *NetworkClient) Start() error {
	return nil
}

func (networkClient *NetworkClient) Stop() error {
	return nil
}

func (networkClient *NetworkClient) GetConnection() (Connection, error) {
	if networkClient.useTls {
		tlsPackage, err := common.GetTlsPackage()
		if common.Error(err) {
			return nil, err
		}

		hostname, _, err := net.SplitHostPort(networkClient.device)
		if common.Error(err) {
			return nil, err
		}

		if hostname == "" {
			hostname = "localhost"
		}

		// set hostname for self-signed cetificates
		tlsPackage.Config.ServerName = hostname
		tlsPackage.Config.InsecureSkipVerify = !networkClient.useTlsVerify

		common.Info("Dial TLS connection: %s...", networkClient.device)

		socket, err := tls.Dial("tcp", networkClient.device, &tlsPackage.Config)
		if common.Error(err) {
			return nil, err
		}

		common.Info("TLS handshake: %s...", networkClient.device)

		err = socket.Handshake()
		if common.Error(err) {
			return nil, err
		}

		if !socket.ConnectionState().HandshakeComplete {
			return nil, fmt.Errorf("TLS handshake not completed")
		}

		return &NetworkConnection{
			socket: socket,
		}, nil
	} else {
		common.Info("Dial connection: %s...", networkClient.device)

		tcpAddr, err := net.ResolveTCPAddr("tcp", networkClient.device)
		if common.Error(err) {
			return nil, err
		}

		socket, err := net.DialTCP("tcp", nil, tcpAddr)
		if common.Error(err) {
			return nil, err
		}

		return &NetworkConnection{
			socket: socket,
		}, nil
	}
}

type NetworkServer struct {
	device       string
	useTls       bool
	useTlsVerify bool
	listener     net.Listener
}

func NewNetworkServer(device string, useTls bool, useTlsVerify bool) (*NetworkServer, error) {
	networkServer := &NetworkServer{
		device:       device,
		useTls:       useTls,
		useTlsVerify: useTlsVerify,
	}

	networkServer.device = device
	networkServer.useTls = useTls
	networkServer.useTlsVerify = useTlsVerify

	return networkServer, nil
}

func (networkServer *NetworkServer) Start() error {
	ips, err := common.GetHostAddrs(true, nil)
	if common.Error(err) {
		return err
	}

	common.Info("Local IPs: %v", ips)

	if networkServer.useTls {
		tlsPackage, err := common.GetTlsPackage()
		if common.Error(err) {
			return err
		}

		common.Info("Create TLS listener: %s...", networkServer.device)

		if networkServer.useTlsVerify {
			tlsPackage.Config.ClientAuth = tls.RequireAndVerifyClientCert
		}

		networkServer.listener, err = tls.Listen("tcp", networkServer.device, &tlsPackage.Config)
		if common.Error(err) {
			return err
		}
	} else {
		common.Info("Create TCP listener: %s...", networkServer.device)

		tcpAddr, err := net.ResolveTCPAddr("tcp", networkServer.device)
		if common.Error(err) {
			return err
		}

		networkServer.listener, err = net.ListenTCP("tcp", tcpAddr)
		if common.Error(err) {
			return err
		}
	}

	return nil
}

func (networkServer *NetworkServer) Stop() error {
	defer func() {
		networkServer.listener = nil
	}()

	if networkServer.listener != nil {
		err := networkServer.listener.Close()
		if common.Error(err) {
			return err
		}
	}

	return nil
}

func (networkServer *NetworkServer) GetConnection() (Connection, error) {
	for {
		common.Info("Accept connection ...")

		socket, err := networkServer.listener.Accept()
		if common.Error(err) {
			return nil, err
		}

		if networkServer.useTls {
			tlsConn, ok := socket.(*tls.Conn)
			if ok {
				err := tlsConn.Handshake()

				if common.Error(err) {
					common.DebugError(socket.Close())

					continue
				}
			}
		}

		common.Info("Connected: %s", socket.RemoteAddr().String())

		return &NetworkConnection{
			socket: socket,
		}, nil
	}
}
