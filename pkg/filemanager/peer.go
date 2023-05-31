package filemanager

import (
	"io"
)

type Peer interface {
	io.ReadWriter
	Shutdown()

	OnEstablish(func())
}
