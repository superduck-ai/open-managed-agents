package listeners

import (
	"errors"
	"net"
	"os"
	"strconv"
)

func Open(addr, inheritedFD string) (net.Listener, error) {
	if inheritedFD == "" {
		return net.Listen("tcp", addr)
	}
	fd, err := strconv.Atoi(inheritedFD)
	if err != nil || fd < 3 {
		return nil, errors.New("invalid inherited listener descriptor")
	}
	file := os.NewFile(uintptr(fd), "inherited-listener")
	if file == nil {
		return nil, errors.New("inherited listener unavailable")
	}
	defer file.Close()
	listener, err := net.FileListener(file)
	if err != nil {
		return nil, err
	}
	if listener.Addr().String() != addr {
		_ = listener.Close()
		return nil, errors.New("inherited listener address differs from configured address")
	}
	return listener, nil
}

func Reserve() (string, *os.File, error) {
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return "", nil, err
	}
	defer listener.Close()
	file, err := listener.File()
	return listener.Addr().String(), file, err
}
