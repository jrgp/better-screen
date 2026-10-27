package mux

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"

	"github.com/jrgp/bs/internal/vt"
)

const (
	protocolVersion = 1
	maxMessage      = 8 << 20
	maxRequest      = 64 << 10
	maxClients      = 64
	maxSessions     = 32
	maxWindows      = 32
	maxTotalWindows = 128
	maxPanes        = 32
	maxTotalPanes   = 128
)

type request struct {
	Version      int
	Op, Name     string
	Dir          string
	Cols, Rows   int
	Multi, Force bool
	Data         []byte
	Commands     []string
	Number       int
}

type sessionInfo struct {
	Name              string
	Windows, Attached int
}

type windowInfo struct {
	Number    int
	Title     string
	Active    bool
	Broadcast bool
}

type response struct {
	Version  int
	Error    string
	Done     bool
	Session  string
	Window   int
	Frame    *vt.Frame
	Sessions []sessionInfo
	Windows  []windowInfo
}

// Standard-library JSON with a fixed-width length prefix keeps framing small
// and lets us reject oversized payloads before allocation or decoding.
func send(w io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, ok := value.(request); ok && len(data) > maxRequest {
		return fmt.Errorf("IPC request exceeds %d bytes", maxRequest)
	}
	if len(data) > maxMessage {
		return fmt.Errorf("IPC message exceeds %d bytes", maxMessage)
	}
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(data)))
	if _, err := io.Copy(w, io.MultiReader(
		bytes.NewReader(size[:]), bytes.NewReader(data),
	)); err != nil {
		return err
	}
	return nil
}

func receive(r io.Reader, value any, limit uint32) error {
	var size [4]byte
	if _, err := io.ReadFull(r, size[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(size[:])
	if n == 0 || n > limit {
		return fmt.Errorf("invalid IPC message length %d", n)
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(r, data); err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

func validSize(cols, rows int) bool {
	return cols >= 2 && cols <= 500 && rows >= 2 && rows <= 200
}
