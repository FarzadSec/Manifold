package ingress

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net/textproto"
	"strings"
)

const (
	maxHelloBytes  = 16 << 10
	maxHeaderBytes = 8 << 10
	recordHeader   = 5
	handshakeType  = 0x16
	clientHello    = 0x01
	extServerName  = 0x0000
	extALPN        = 0x0010
)

var errNotHello = errors.New("not a TLS ClientHello")

// peek is what was read off a connection before choosing a route: the host it asked for,
// whether it was TLS, and every byte read, which the target receives first.
type peek struct {
	host  string
	alpn  []string
	tls   bool
	bytes []byte
}

// readFirst reads a TLS ClientHello, which may span records, or an HTTP request head.
func readFirst(r *bufio.Reader) (peek, error) {
	first, err := r.Peek(1)
	if err != nil {
		return peek{}, err
	}
	if first[0] == handshakeType {
		return readClientHello(r)
	}
	return readHTTPHead(r)
}

func readClientHello(r *bufio.Reader) (peek, error) {
	var raw, body []byte
	for {
		head := make([]byte, recordHeader)
		if _, err := io.ReadFull(r, head); err != nil {
			return peek{bytes: raw}, err
		}
		raw = append(raw, head...)
		if head[0] != handshakeType {
			return peek{bytes: raw}, errNotHello
		}
		n := int(binary.BigEndian.Uint16(head[3:5]))
		if n == 0 || len(raw)+n > maxHelloBytes {
			return peek{bytes: raw}, errNotHello
		}
		frag := make([]byte, n)
		if _, err := io.ReadFull(r, frag); err != nil {
			return peek{bytes: raw}, err
		}
		raw = append(raw, frag...)
		body = append(body, frag...)
		if len(body) >= 4 {
			if body[0] != clientHello {
				return peek{bytes: raw}, errNotHello
			}
			want := 4 + (int(body[1])<<16 | int(body[2])<<8 | int(body[3]))
			if want > maxHelloBytes {
				return peek{bytes: raw}, errNotHello
			}
			if len(body) >= want {
				host, alpn := parseHello(body[4:want])
				return peek{host: host, alpn: alpn, tls: true, bytes: raw}, nil
			}
		}
	}
}

// parseHello reads the server name and ALPN list out of a ClientHello body; a malformed
// one yields no name, so it takes the default route rather than failing.
func parseHello(b []byte) (string, []string) {
	c := cursor{b: b}
	c.skip(2 + 32)
	c.skip(int(c.u8()))
	c.skip(int(c.u16()))
	c.skip(int(c.u8()))
	exts := cursor{b: c.take(int(c.u16()))}
	var host string
	var alpn []string
	for !exts.done() {
		typ, data := exts.u16(), cursor{b: exts.take(int(exts.u16()))}
		switch typ {
		case extServerName:
			list := cursor{b: data.take(int(data.u16()))}
			for !list.done() {
				kind, name := list.u8(), list.take(int(list.u16()))
				if kind == 0 && host == "" {
					host = string(name)
				}
			}
		case extALPN:
			list := cursor{b: data.take(int(data.u16()))}
			for !list.done() {
				alpn = append(alpn, string(list.take(int(list.u8()))))
			}
		}
	}
	return strings.ToLower(strings.TrimSuffix(host, ".")), alpn
}

// cursor reads big-endian fields; past the end it yields zeros and marks itself done.
type cursor struct {
	b   []byte
	bad bool
}

func (c *cursor) done() bool { return c.bad || len(c.b) == 0 }

func (c *cursor) take(n int) []byte {
	if c.bad || n < 0 || n > len(c.b) {
		c.bad = true
		return nil
	}
	out := c.b[:n]
	c.b = c.b[n:]
	return out
}

func (c *cursor) skip(n int) { c.take(n) }

func (c *cursor) u8() uint8 {
	b := c.take(1)
	if b == nil {
		return 0
	}
	return b[0]
}

func (c *cursor) u16() uint16 {
	b := c.take(2)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint16(b)
}

// readHTTPHead reads a plain HTTP request head and its Host, without its body.
func readHTTPHead(r *bufio.Reader) (peek, error) {
	var raw []byte
	for {
		line, err := r.ReadSlice('\n')
		raw = append(raw, line...)
		if err != nil {
			return peek{bytes: raw}, err
		}
		if len(raw) > maxHeaderBytes {
			return peek{bytes: raw}, errors.New("request head too large")
		}
		if bytes.Equal(bytes.TrimRight(line, "\r\n"), nil) {
			break
		}
	}
	tp := textproto.NewReader(bufio.NewReader(bytes.NewReader(raw)))
	if _, err := tp.ReadLine(); err != nil {
		return peek{bytes: raw}, err
	}
	header, _ := tp.ReadMIMEHeader()
	host := header.Get("Host")
	if strings.HasPrefix(host, "[") {
		if end := strings.IndexByte(host, ']'); end > 0 {
			host = host[1:end]
		}
	} else if h, _, ok := strings.Cut(host, ":"); ok {
		host = h
	}
	return peek{host: strings.ToLower(strings.TrimSuffix(host, ".")), bytes: raw}, nil
}
