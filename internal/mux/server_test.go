package mux

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDaemonLifecycle(t *testing.T) {
	home := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	config, err := loadConfig(home)
	require.NoError(t, err)
	server := NewServer(home, "/bin/sh", log.New(io.Discard, "", 0), config)
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Error("daemon did not exit")
		}
	})
	var conn *net.UnixConn
	require.Eventually(t, func() bool { var err error; conn, err = dial(filepath.Join(home, ".bs")); return err == nil }, 5*time.Second, 10*time.Millisecond)
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	require.NoError(t, send(conn, request{Version: 1, Op: opCreate, Name: "work", Cols: 80, Rows: 24}))
	var result response
	require.NoError(t, receive(conn, &result, maxMessage))
	require.Empty(t, result.Error)
	require.Equal(t, "work", result.Session)
	require.NotNil(t, result.Frame)
	require.NoError(t, send(conn, request{Version: 1, Op: opInput, Data: []byte("printf '\\033[2J\\033[Hcore-ready'\n")}))
	for {
		require.NoError(t, receive(conn, &result, maxMessage))
		require.Empty(t, result.Error)
		if result.Frame != nil && bytes.Contains([]byte(result.Frame.Lines[0]), []byte("core-ready")) {
			break
		}
	}
	dir, err := os.Stat(filepath.Join(home, ".bs"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0700), dir.Mode().Perm())
	sock, err := os.Stat(filepath.Join(home, ".bs", "bs.sock"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), sock.Mode().Perm())
}

func TestProtocolAndNames(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, send(&buf, request{Version: 1, Op: opInput, Data: []byte{0, 1, 255}}))
	var r request
	require.NoError(t, receive(&buf, &r, maxRequest))
	assert.Equal(t, opInput, r.Op)
	assert.Equal(t, []byte{0, 1, 255}, r.Data)
	require.ErrorContains(t, send(io.Discard, request{Commands: []string{string(make([]byte, maxRequest))}}), "IPC request exceeds")
	require.NoError(t, binary.Write(&buf, binary.BigEndian, uint32(maxRequest+1)))
	assert.Error(t, receive(&buf, &r, maxRequest))
	_, err := match([]*session{{name: "work"}, {name: "work2"}}, "wor")
	assert.ErrorContains(t, err, "ambiguous")
	s, err := match([]*session{{name: "work"}, {name: "work2"}}, "work")
	require.NoError(t, err)
	assert.Equal(t, "work", s.name)
	assert.False(t, validName("\x1b]evil"))
}

func TestUnsafeRuntime(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(home, ".bs")))
	_, err := runtimeDir(home)
	require.Error(t, err)
}
