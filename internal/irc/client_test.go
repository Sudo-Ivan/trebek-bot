package irc

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"trebek/internal/config"
)

// MockConn implements net.Conn for testing purposes.
type MockConn struct {
	readBuffer  bytes.Buffer
	writeBuffer bytes.Buffer
	closeOnce   sync.Once
	closed      chan struct{}
}

func NewMockConn() *MockConn {
	return &MockConn{
		closed: make(chan struct{}),
	}
}

func (m *MockConn) Read(b []byte) (n int, err error) {
	select {
	case <-m.closed:
		return 0, io.EOF
	default:
		return m.readBuffer.Read(b)
	}
}

func (m *MockConn) Write(b []byte) (n int, err error) {
	select {
	case <-m.closed:
		return 0, io.EOF
	default:
		return m.writeBuffer.Write(b)
	}
}

func (m *MockConn) Close() error {
	m.closeOnce.Do(func() {
		close(m.closed)
	})
	return nil
}

func (m *MockConn) LocalAddr() net.Addr {
	return nil
}

func (m *MockConn) RemoteAddr() net.Addr {
	return nil
}

func (m *MockConn) SetDeadline(t time.Time) error {
	return nil
}

func (m *MockConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (m *MockConn) SetWriteDeadline(t time.Time) error {
	return nil
}

// InjectReadData injects data into the read buffer to simulate incoming messages.
func (m *MockConn) InjectReadData(data string) {
	m.readBuffer.WriteString(data)
}

// GetWrittenData retrieves data written to the connection.
func (m *MockConn) GetWrittenData() string {
	return m.writeBuffer.String()
}

// ClearWrittenData clears the write buffer.
func (m *MockConn) ClearWrittenData() {
	m.writeBuffer.Reset()
}

func TestNewClient(t *testing.T) {
	cfg := &config.Config{}
	client := NewClient(cfg)

	if client == nil {
		t.Fatal("NewClient returned nil")
	}
	if client.Config != cfg {
		t.Errorf("Client config not set correctly")
	}
}

// Mock net.Dial and tls.Dial functions
var (
	mockDial        func(network, address string) (net.Conn, error)
	mockTLSDial     func(network, address string, tlsConfig *tls.Config) (net.Conn, error)
	originalDial    = net.Dial
	originalTLSDial = tls.Dial
)

func init() {
	// Replace original functions with mocks
	netDial = func(network, address string) (net.Conn, error) {
		if mockDial != nil {
			return mockDial(network, address)
		}
		return originalDial(network, address)
	}
	tlsDial = func(network, address string, tlsConfig *tls.Config) (net.Conn, error) {
		if mockTLSDial != nil {
			return mockTLSDial(network, address, tlsConfig)
		}
		return originalTLSDial(network, address, tlsConfig)
	}
}

func TestConnect(t *testing.T) {
	cfg := &config.Config{
		BotName:      "TestBot",
		IRCServer:    "irc.example.com:6667",
		IRCServerTLS: "irc.example.com:6697",
	}
	client := NewClient(cfg)

	// Test non-TLS connection
	t.Run("NonTLS", func(t *testing.T) {
		mockConn := NewMockConn()
		mockDial = func(network, address string) (net.Conn, error) {
			if network != "tcp" || address != cfg.IRCServer {
				t.Errorf("Dial called with unexpected network/address: %s/%s", network, address)
			}
			return mockConn, nil
		}
		defer func() { mockDial = nil }()

		err := client.Connect(false)
		if err != nil {
			t.Fatalf("Connect(false) failed: %v", err)
		}

		written := mockConn.GetWrittenData()
		expected := fmt.Sprintf("NICK %s\r\nUSER %s 0 * :%s\r\n", cfg.BotName, cfg.BotName, cfg.BotName)
		if written != expected {
			t.Errorf("Expected NICK/USER commands:\n%q\nGot:\n%q", expected, written)
		}
		mockConn.ClearWrittenData()
		client.Close()
	})

	// Test TLS connection
	t.Run("TLS", func(t *testing.T) {
		mockConn := NewMockConn()
		mockTLSDial = func(network, address string, tlsConfig *tls.Config) (net.Conn, error) {
			if network != "tcp" || address != cfg.IRCServerTLS {
				t.Errorf("TLSDial called with unexpected network/address: %s/%s", network, address)
			}
			return mockConn, nil
		}
		defer func() { mockTLSDial = nil }()

		err := client.Connect(true)
		if err != nil {
			t.Fatalf("Connect(true) failed: %v", err)
		}

		written := mockConn.GetWrittenData()
		expected := fmt.Sprintf("NICK %s\r\nUSER %s 0 * :%s\r\n", cfg.BotName, cfg.BotName, cfg.BotName)
		if written != expected {
			t.Errorf("Expected NICK/USER commands:\n%q\nGot:\n%q", expected, written)
		}
		mockConn.ClearWrittenData()
		client.Close()
	})

	// Test connection error
	t.Run("ConnectError", func(t *testing.T) {
		mockDial = func(network, address string) (net.Conn, error) {
			return nil, fmt.Errorf("mock connection error")
		}
		defer func() { mockDial = nil }()

		err := client.Connect(false)
		if err == nil {
			t.Fatal("Expected connection error, got nil")
		}
		if !strings.Contains(err.Error(), "mock connection error") {
			t.Errorf("Expected 'mock connection error', got: %v", err)
		}
	})
}

func TestSend(t *testing.T) {
	cfg := &config.Config{
		BotName:   "TestBot",
		IRCServer: "irc.example.com:6667",
	}
	client := NewClient(cfg)
	mockConn := NewMockConn()
	mockDial = func(network, address string) (net.Conn, error) {
		return mockConn, nil
	}
	defer func() { mockDial = nil }()

	err := client.Connect(false)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	mockConn.ClearWrittenData() // Clear NICK/USER commands

	testMsg := "PRIVMSG #channel :Hello"
	client.Send("%s", testMsg)

	written := mockConn.GetWrittenData()
	expected := testMsg + "\r\n"
	if written != expected {
		t.Errorf("Expected %q, got %q", expected, written)
	}
}

func TestJoinChannel(t *testing.T) {
	cfg := &config.Config{
		BotName:   "TestBot",
		IRCServer: "irc.example.com:6667",
	}
	client := NewClient(cfg)
	mockConn := NewMockConn()
	mockDial = func(network, address string) (net.Conn, error) {
		return mockConn, nil
	}
	defer func() { mockDial = nil }()

	err := client.Connect(false)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	mockConn.ClearWrittenData() // Clear NICK/USER commands

	client.JoinChannel("#test")

	written := mockConn.GetWrittenData()
	expected := "JOIN #test\r\n"
	if written != expected {
		t.Errorf("Expected %q, got %q", expected, written)
	}
}

func TestPrivmsg(t *testing.T) {
	cfg := &config.Config{
		BotName:   "TestBot",
		IRCServer: "irc.example.com:6667",
	}
	client := NewClient(cfg)
	mockConn := NewMockConn()
	mockDial = func(network, address string) (net.Conn, error) {
		return mockConn, nil
	}
	defer func() { mockDial = nil }()

	err := client.Connect(false)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	mockConn.ClearWrittenData() // Clear NICK/USER commands

	client.Privmsg("#channel", "Hello, world!")

	written := mockConn.GetWrittenData()
	expected := "PRIVMSG #channel :Hello, world!\r\n"
	if written != expected {
		t.Errorf("Expected %q, got %q", expected, written)
	}
}

func TestClose(t *testing.T) {
	cfg := &config.Config{
		BotName:   "TestBot",
		IRCServer: "irc.example.com:6667",
	}
	client := NewClient(cfg)
	mockConn := NewMockConn()
	mockDial = func(network, address string) (net.Conn, error) {
		return mockConn, nil
	}
	defer func() { mockDial = nil }()

	err := client.Connect(false)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	// Don't clear written data here, as Close might send QUIT

	client.Close()

	// Verify that the mock connection's Close method was called (by checking its closed channel)
	select {
	case <-mockConn.closed:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Fatal("MockConn Close was not called")
	}
}

func TestListen(t *testing.T) {
	// Test PING-PONG
	t.Run("PingPong", func(t *testing.T) {
		cfg := &config.Config{
			BotName:   "TestBot",
			IRCServer: "irc.example.com:6667",
		}
		client := NewClient(cfg)
		mockConn := NewMockConn()
		mockDial = func(network, address string) (net.Conn, error) {
			return mockConn, nil
		}
		defer func() { mockDial = nil }()

		err := client.Connect(false)
		if err != nil {
			t.Fatalf("Connect failed: %v", err)
		}
		mockConn.ClearWrittenData() // Clear NICK/USER commands

		mockConn.InjectReadData("PING :irc.example.com\r\n")
		go client.Listen()

		// Wait for PONG to be written
		time.Sleep(50 * time.Millisecond) // Give goroutine time to process
		written := mockConn.GetWrittenData()
		expected := "PONG :irc.example.com\r\n"
		if written != expected {
			t.Errorf("Expected PONG, got %q", written)
		}
		mockConn.ClearWrittenData()
		client.Close() // Stop the listener goroutine
	})

	// Test PRIVMSG handling
	t.Run("Privmsg", func(t *testing.T) {
		cfg := &config.Config{
			BotName:   "TestBot",
			IRCServer: "irc.example.com:6667",
		}
		client := NewClient(cfg)
		mockConn := NewMockConn()
		mockDial = func(network, address string) (net.Conn, error) {
			return mockConn, nil
		}
		defer func() { mockDial = nil }()

		err := client.Connect(false)
		if err != nil {
			t.Fatalf("Connect failed: %v", err)
		}
		mockConn.ClearWrittenData() // Clear NICK/USER commands

		handlerCalled := make(chan struct{})
		client.Handler = func(target, user, message string) {
			if target != "#channel" {
				t.Errorf("Expected target #channel, got %q", target)
			}
			if user != "testuser" {
				t.Errorf("Expected user testuser, got %q", user)
			}
			if message != "Hello, bot!" {
				t.Errorf("Expected message 'Hello, bot!', got %q", message)
			}
			close(handlerCalled)
		}

		mockConn.InjectReadData(":testuser!~test@host PRIVMSG #channel :Hello, bot!\r\n")
		go client.Listen()

		select {
		case <-handlerCalled:
			// Success
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Handler was not called")
		}
		client.Close()
	})

	// Test Read error
	t.Run("ReadError", func(t *testing.T) {
		cfg := &config.Config{
			BotName:   "TestBot",
			IRCServer: "irc.example.com:6667",
		}
		client := NewClient(cfg)
		mockConn := NewMockConn()
		mockDial = func(network, address string) (net.Conn, error) {
			return mockConn, nil
		}
		defer func() { mockDial = nil }()

		err := client.Connect(false)
		if err != nil {
			t.Fatalf("Connect failed: %v", err)
		}
		mockConn.ClearWrittenData() // Clear NICK/USER commands

		// Simulate read error by closing the connection immediately
		mockConn.Close()

		listenFinished := make(chan struct{})
		go func() {
			client.Listen()
			close(listenFinished)
		}()

		select {
		case <-listenFinished:
			// Success, Listen goroutine exited
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Listen goroutine did not exit on read error")
		}
	})
}
