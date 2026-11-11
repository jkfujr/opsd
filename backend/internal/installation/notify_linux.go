package installation

import (
	"golang.org/x/sys/unix"
	"net"
	"os"
	"strings"
)

func PrivateFiles() { unix.Umask(0077) }

func NotifyReady() error {
	address := os.Getenv("NOTIFY_SOCKET")
	if address == "" {
		return nil
	}
	if strings.HasPrefix(address, "@") {
		address = "\x00" + address[1:]
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: address, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write([]byte("READY=1"))
	return err
}
