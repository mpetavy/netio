package endpoint

import "io"

type Endpoint interface {
	Start() error
	Stop() error
	GetConnection() (Connection, error)
}

type Connection interface {
	io.ReadWriteCloser
	Reset() error
}
