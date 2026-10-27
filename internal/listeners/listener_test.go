package listeners

import (
	"net"
	"strconv"
	"testing"
)

func TestRejectsInvalidInheritedListener(t *testing.T) {
	for _, fd := range []string{"no", "-1", "2", "999999"} {
		if listener, err := Open("127.0.0.1:1", fd); err == nil {
			listener.Close()
			t.Fatalf("accepted fd %s", fd)
		}
	}
	addr, file, err := Reserve()
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if listener, err := Open("127.0.0.1:1", strconv.Itoa(int(file.Fd()))); err == nil {
		listener.Close()
		t.Fatalf("accepted address mismatch for %s", addr)
	}
}

func TestInheritedListenerStaysBound(t *testing.T) {
	addr, file, err := Reserve()
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if listener, err := net.Listen("tcp", addr); err == nil {
		listener.Close()
		t.Fatal("reservation released before inheritance")
	}
	listener, err := Open(addr, strconv.Itoa(int(file.Fd())))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	connection, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	connection.Close()
	peer, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	peer.Close()
	listener.Close()
	rebound, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal("listener leaked", err)
	}
	rebound.Close()
}
