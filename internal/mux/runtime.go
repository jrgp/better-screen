package mux

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func owned(info os.FileInfo, mode os.FileMode) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("%s is not owned by this user", info.Name())
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != mode {
		return fmt.Errorf("unsafe permissions or symlink: %s (expected %04o)", info.Name(), mode)
	}
	return nil
}

func runtimeDir(home string) (string, error) {
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("HOME must be an absolute path")
	}
	dir := filepath.Join(home, ".bs")
	if err := os.Mkdir(dir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	if err := owned(info, 0700); err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	return dir, nil
}

func secureFile(path string, flags int) (*os.File, error) {
	fd, err := unix.Open(path, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err == nil {
		err = owned(info, 0600)
	}
	if err == nil && (!info.Mode().IsRegular() || info.Sys().(*syscall.Stat_t).Nlink != 1) {
		err = fmt.Errorf("unsafe file: %s", path)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func checkSocket(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if err := owned(info, 0600); err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return nil, fmt.Errorf("%s is not a socket", path)
	}
	return info, nil
}

func dial(dir string) (*net.UnixConn, error) {
	path := filepath.Join(dir, "bs.sock")
	if _, err := checkSocket(path); err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return nil, err
	}
	c := conn.(*net.UnixConn)
	if err := checkPeer(c); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func connect(home string, start bool) (*net.UnixConn, error) {
	dir, err := runtimeDir(home)
	if err != nil {
		return nil, err
	}
	conn, err := dial(dir)
	if err == nil || !start {
		return conn, err
	}
	if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ECONNREFUSED) {
		return nil, err
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	logfile, err := secureFile(filepath.Join(dir, "daemon.log"), unix.O_CREAT|unix.O_WRONLY)
	if err != nil {
		return nil, err
	}
	defer logfile.Close()
	cmd := exec.Command(executable, "__daemon")
	cmd.Stderr = logfile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	// Reap the short startup loser when simultaneous clients race. The winning
	// daemon outlives this client and is reparented by the OS.
	go cmd.Wait()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, err = dial(dir)
		if err == nil {
			return conn, nil
		}
		if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ECONNREFUSED) {
			return nil, err
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil, fmt.Errorf("daemon did not start: %w (see %s/daemon.log)", err, dir)
}
