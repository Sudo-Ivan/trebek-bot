package irc

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"strings"

	"trebek/internal/config"
)

// For testing purposes, allow overriding net.Dial and tls.Dial
var (
	netDial                                                                  = net.Dial
	tlsDial func(network, addr string, config *tls.Config) (net.Conn, error) = func(network, addr string, config *tls.Config) (net.Conn, error) {
		return tls.Dial(network, addr, config)
	}
)

// Client represents an IRC client.
type Client struct {
	conn    net.Conn
	reader  *bufio.Reader
	writer  *bufio.Writer
	Config  *config.Config
	Handler func(string, string, string) // Callback for messages: channel, user, message
}

// NewClient creates a new IRC client.
func NewClient(cfg *config.Config) *Client {
	return &Client{
		Config: cfg,
	}
}

// Connect establishes a connection to the IRC server.
func (c *Client) Connect(useTLS bool) error {
	var conn net.Conn
	var err error

	serverAddr := c.Config.IRCServer
	if useTLS {
		serverAddr = c.Config.IRCServerTLS
		log.Printf("Connecting to IRC server (TLS): %s", serverAddr)
		conn, err = tlsDial("tcp", serverAddr, &tls.Config{InsecureSkipVerify: true}) // #nosec G402 - InsecureSkipVerify for simplicity, should be false in production
	} else {
		log.Printf("Connecting to IRC server (non-TLS): %s", serverAddr)
		conn, err = netDial("tcp", serverAddr)
	}

	if err != nil {
		return fmt.Errorf("failed to connect to IRC server: %w", err)
	}

	c.conn = conn
	c.reader = bufio.NewReader(conn)
	c.writer = bufio.NewWriter(conn)

	log.Printf("Connected to %s", serverAddr)

	// Send NICK and USER commands
	c.Send("NICK %s", c.Config.BotName)
	c.Send("USER %s 0 * :%s", c.Config.BotName, c.Config.BotName)

	return nil
}

// Send sends a raw IRC command to the server.
func (c *Client) Send(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("--> %s", msg)
	_, err := fmt.Fprintf(c.writer, "%s\r\n", msg)
	if err != nil {
		log.Printf("Error writing to IRC: %v", err)
		return
	}
	err = c.writer.Flush()
	if err != nil {
		log.Printf("Error flushing writer: %v", err)
	}
}

// JoinChannel joins the specified IRC channel.
func (c *Client) JoinChannel(channel string) {
	c.Send("JOIN %s", channel)
}

// Privmsg sends a private message to a target (channel or user).
func (c *Client) Privmsg(target, message string) {
	c.Send("PRIVMSG %s :%s", target, message)
}

// Close closes the IRC client connection.
func (c *Client) Close() {
	if c.conn != nil {
		err := c.conn.Close()
		if err != nil {
			log.Printf("Error closing IRC connection: %v", err)
		}
		log.Println("IRC connection closed.")
	}
}

// Listen starts listening for messages from the IRC server.
func (c *Client) Listen() {
	for {
		line, err := c.reader.ReadString('\n')
		if err != nil {
			log.Printf("Error reading from IRC: %v", err)
			c.Close() // Use the public Close method
			return
		}

		line = strings.TrimSpace(line)
		log.Printf("<-- %s", line)

		// Handle PING requests
		if strings.HasPrefix(line, "PING :") {
			pongMsg := strings.TrimPrefix(line, "PING :")
			c.Send("PONG :%s", pongMsg)
			continue
		}

		// Parse PRIVMSG
		if strings.Contains(line, " PRIVMSG ") {
			parts := strings.SplitN(line, " PRIVMSG ", 2)
			if len(parts) != 2 {
				continue
			}

			prefix := parts[0]
			messagePart := parts[1]

			user := ""
			if strings.HasPrefix(prefix, ":") {
				user = strings.SplitN(prefix[1:], "!", 2)[0]
			}

			msgParts := strings.SplitN(messagePart, " :", 2)
			if len(msgParts) != 2 {
				continue
			}

			target := msgParts[0]
			message := msgParts[1]

			target = strings.TrimSpace(target)
			message = strings.TrimSpace(message)

			if c.Handler != nil {
				c.Handler(target, user, message)
			}
		}
	}
}
