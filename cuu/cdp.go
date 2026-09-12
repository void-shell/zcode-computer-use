package main

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Minimal RFC 6455 client plus a Chrome DevTools Protocol caller.
//
// Chrome's debugging socket only needs masked text frames out and
// unfragmented text frames back, so cuu stays on the standard library
// instead of taking a websocket dependency. Frames above maxFrameBytes are
// refused: a screenshot is the largest thing Chrome sends, and a runaway
// declared length means the stream is already broken.
//
// The JSON envelope structs below carry no tags: encoding/json matches field
// names case-insensitively, so ID/Result/Error already bind id/result/error.

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA
)

const maxFrameBytes = 64 << 20

var wsCRLF = string(rune(13)) + string(rune(10))

type wsConn struct {
	conn net.Conn
	r    *bufio.Reader
	w    *bufio.Writer
	mu   sync.Mutex
}

// wsDial performs the HTTP Upgrade handshake and verifies Sec-WebSocket-Accept.
func wsDial(rawURL string, timeout time.Duration) (*wsConn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "ws" {
		return nil, fmt.Errorf("unsupported websocket scheme %q", u.Scheme)
	}
	host := u.Host
	if u.Port() == "" {
		host += ":80"
	}
	conn, err := net.DialTimeout("tcp", host, timeout)
	if err != nil {
		return nil, err
	}
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		conn.Close()
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	req := "GET " + path + " HTTP/1.1" + wsCRLF +
		"Host: " + host + wsCRLF +
		"Upgrade: websocket" + wsCRLF +
		"Connection: Upgrade" + wsCRLF +
		"Sec-WebSocket-Key: " + key + wsCRLF +
		"Sec-WebSocket-Version: 13" + wsCRLF + wsCRLF
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, err
	}
	br := bufio.NewReader(conn)
	status, err := br.ReadString(byte(10))
	if err != nil {
		conn.Close()
		return nil, err
	}
	if !strings.Contains(status, " 101") {
		conn.Close()
		return nil, fmt.Errorf("websocket handshake rejected: %s", strings.TrimSpace(status))
	}
	accept := ""
	for {
		line, err := br.ReadString(byte(10))
		if err != nil {
			conn.Close()
			return nil, err
		}
		line = strings.TrimRight(line, wsCRLF)
		if line == "" {
			break
		}
		if k, v, ok := strings.Cut(line, ":"); ok &&
			strings.EqualFold(strings.TrimSpace(k), "Sec-WebSocket-Accept") {
			accept = strings.TrimSpace(v)
		}
	}
	sum := sha1.Sum([]byte(key + wsGUID))
	if accept != base64.StdEncoding.EncodeToString(sum[:]) {
		conn.Close()
		return nil, errors.New("websocket accept header did not match the request key")
	}
	return &wsConn{conn: conn, r: br, w: bufio.NewWriter(conn)}, nil
}

func (ws *wsConn) close() { _ = ws.conn.Close() }

// writeFrame emits one client frame. Clients must mask; Chrome closes the
// socket on an unmasked frame.
func (ws *wsConn) writeFrame(opcode byte, payload []byte) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	hdr := []byte{0x80 | opcode}
	n := len(payload)
	switch {
	case n < 126:
		hdr = append(hdr, 0x80|byte(n))
	case n <= 0xFFFF:
		hdr = append(hdr, 0x80|126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, 0x80|127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		hdr = append(hdr, ext[:]...)
	}
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	hdr = append(hdr, mask[:]...)
	body := make([]byte, n)
	for i := 0; i < n; i++ {
		body[i] = payload[i] ^ mask[i%4]
	}
	if _, err := ws.w.Write(hdr); err != nil {
		return err
	}
	if _, err := ws.w.Write(body); err != nil {
		return err
	}
	return ws.w.Flush()
}

func (ws *wsConn) writeText(payload []byte) error { return ws.writeFrame(opText, payload) }

func (ws *wsConn) readFrame() (bool, byte, []byte, error) {
	var h [2]byte
	if _, err := io.ReadFull(ws.r, h[:]); err != nil {
		return false, 0, nil, err
	}
	fin := h[0]&0x80 != 0
	opcode := h[0] & 0x0F
	masked := h[1]&0x80 != 0
	n := uint64(h[1] & 0x7F)
	switch n {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(ws.r, ext[:]); err != nil {
			return false, 0, nil, err
		}
		n = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(ws.r, ext[:]); err != nil {
			return false, 0, nil, err
		}
		n = binary.BigEndian.Uint64(ext[:])
	}
	if n > maxFrameBytes {
		return false, 0, nil, fmt.Errorf("websocket frame of %d bytes exceeds the %d byte limit", n, maxFrameBytes)
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(ws.r, mask[:]); err != nil {
			return false, 0, nil, err
		}
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(ws.r, payload); err != nil {
		return false, 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return fin, opcode, payload, nil
}

// readMessage returns one complete data message, reassembling continuations
// and answering ping/close control frames along the way.
func (ws *wsConn) readMessage() ([]byte, error) {
	var msg []byte
	for {
		fin, opcode, payload, err := ws.readFrame()
		if err != nil {
			return nil, err
		}
		switch opcode {
		case opContinuation, opText, opBinary:
			msg = append(msg, payload...)
		case opClose:
			_ = ws.writeFrame(opClose, nil)
			return nil, io.EOF
		case opPing:
			_ = ws.writeFrame(opPong, payload)
			continue
		default:
			continue
		}
		if fin {
			return msg, nil
		}
	}
}

// ---------------------------------------------------------------- cdp

type cdpConn struct {
	ws   *wsConn
	next int64
	mu   sync.Mutex
}

type cdpError struct {
	Code    int
	Message string
}

func cdpDial(wsURL string, timeout time.Duration) (*cdpConn, error) {
	ws, err := wsDial(wsURL, timeout)
	if err != nil {
		return nil, err
	}
	return &cdpConn{ws: ws}, nil
}

func (c *cdpConn) close() { c.ws.close() }

// Call sends one CDP command and waits for the reply carrying its id, skipping
// the events Chrome interleaves. One command is in flight at a time.
func (c *cdpConn) Call(method string, params any, out any, timeout time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	id := c.next
	req := map[string]any{"id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if err := c.ws.conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	if err := c.ws.writeText(raw); err != nil {
		return err
	}
	for {
		msg, err := c.ws.readMessage()
		if err != nil {
			return err
		}
		var env struct {
			ID     *int64
			Result json.RawMessage
			Error  *cdpError
		}
		if err := json.Unmarshal(msg, &env); err != nil {
			continue
		}
		if env.ID == nil || *env.ID != id {
			continue // an event, or a reply to a command we already gave up on
		}
		if env.Error != nil {
			return fmt.Errorf("cdp %s: %s (code %d)", method, env.Error.Message, env.Error.Code)
		}
		if out != nil && len(env.Result) > 0 {
			return json.Unmarshal(env.Result, out)
		}
		return nil
	}
}
