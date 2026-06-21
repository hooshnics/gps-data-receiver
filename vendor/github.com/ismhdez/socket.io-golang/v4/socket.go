package socketio

import (
	"errors"
	"io"
	"net"
	"strings"
	"time"

	"github.com/gofiber/websocket/v2"
	gWebsocket "github.com/gorilla/websocket"
	"github.com/ismhdez/socket.io-golang/v4/engineio"
	"github.com/ismhdez/socket.io-golang/v4/socket_protocol"
)

type Conn struct {
	fasthttp *websocket.Conn
	http     *gWebsocket.Conn
}

func (c *Conn) NextWriter(messageType int) (io.WriteCloser, error) {
	if c.http != nil {
		return c.http.NextWriter(messageType)
	}
	if c.fasthttp != nil {
		return c.fasthttp.NextWriter(messageType)
	}
	return nil, errors.New("not found http or fasthttp socket")
}

func (c *Conn) SetReadDeadline(t time.Time) error {
	if c.http != nil {
		return c.http.SetReadDeadline(t)
	}
	if c.fasthttp != nil {
		return c.fasthttp.SetReadDeadline(t)
	}
	return errors.New("not found http or fasthttp socket")
}

func (c *Conn) Close() error {
	if c.http != nil {
		return c.http.Close()
	}
	if c.fasthttp != nil {
		return c.fasthttp.Close()
	}
	return errors.New("not found http or fasthttp socket")
}

func (c *Conn) RemoteAddr() string {
	if c.fasthttp != nil {
		return strings.Split(c.fasthttp.Headers("X-Forwarded-For", c.fasthttp.RemoteAddr().String()), ",")[0]
	}

	if c.http != nil {
		return c.http.RemoteAddr().(*net.TCPAddr).IP.String()
	}

	return ""
}

func (c *Conn) Headers(key string, defaultValue ...string) string {
	if c.fasthttp != nil {
		return c.fasthttp.Headers(key, defaultValue...)
	}

	return ""
}

func (c *Conn) Query(key string, defaultValue ...string) string {
	if c.fasthttp != nil {
		return c.fasthttp.Query(key, defaultValue...)
	}

	return ""
}

func (c *Conn) Params(key string, defaultValue ...string) string {
	if c.fasthttp != nil {
		return c.fasthttp.Params(key, defaultValue...)
	}

	return ""
}

func (c *Conn) Cookies(key string, defaultValue ...string) string {
	if c.fasthttp != nil {
		return c.fasthttp.Cookies(key, defaultValue...)
	}

	return ""
}

func (c *Conn) UserAgent() string {
	if c.fasthttp != nil {
		return c.fasthttp.Headers("User-Agent")
	}

	return ""
}

type Socket struct {
	Id        string
	Nps       string
	Conn      *Conn
	metadata  map[string]interface{}
	rooms     roomNames
	listeners listeners
	pingTime  time.Duration
	dispose   []func()
	Join      func(room string)
	Leave     func(room string)
	To        func(room string) *Room
}

func (s *Socket) Metadata(key string, value ...interface{}) interface{} {
	if s.metadata == nil {
		s.metadata = make(map[string]interface{})
	}

	if len(value) > 0 {
		s.metadata[key] = value[0]
	}

	return s.metadata[key]
}

func (s *Socket) On(event string, fn eventCallback) {
	s.listeners.set(event, fn)
}

func (s *Socket) Emit(event string, agrs ...interface{}) error {
	c := s.Conn
	if c == nil {
		return errors.New("socket has disconnected")
	}
	agrs = append([]interface{}{event}, agrs...)
	return s.writer(socket_protocol.EVENT, agrs)
}

func (s *Socket) ack(ackEvent string, agrs ...interface{}) error {
	c := s.Conn
	if c == nil {
		return errors.New("socket has disconnected")
	}
	agrs = append([]interface{}{ackEvent}, agrs...)
	return s.writer(socket_protocol.ACK, agrs)
}

func (s *Socket) Ping() error {
	c := s.Conn
	if c == nil {
		return errors.New("socket has disconnected")
	}
	w, err := c.NextWriter(websocket.TextMessage)
	if err != nil {
		c.Close()
		return err
	}
	engineio.WriteByte(w, engineio.PING, []byte{})
	return w.Close()
}

func (s *Socket) Disconnect() error {
	c := s.Conn
	if c == nil {
		return errors.New("socket has disconnected")
	}
	s.writer(socket_protocol.DISCONNECT)
	return c.SetReadDeadline(time.Now())
}

func (s *Socket) Rooms() []string {
	return s.rooms.all()
}

func (s *Socket) disconnect() {
	s.Conn.Close()
	s.Conn = nil
	// s.rooms = []string{}
	if len(s.dispose) > 0 {
		for _, dispose := range s.dispose {
			dispose()
		}
	}
}

func (s *Socket) engineWrite(t engineio.PacketType, arg ...interface{}) error {
	w, err := s.Conn.NextWriter(websocket.TextMessage)
	if err != nil {
		return err
	}
	engineio.WriteTo(w, t, arg...)
	return w.Close()
}

func (s *Socket) writer(t socket_protocol.PacketType, arg ...interface{}) error {
	w, err := s.Conn.NextWriter(websocket.TextMessage)
	if err != nil {
		return err
	}
	nps := ""
	if s.Nps != "/" {
		nps = s.Nps + ","
	}
	if t == socket_protocol.ACK {
		agrs := append([]interface{}{}, arg[0].([]interface{})[1:])
		socket_protocol.WriteToWithAck(w, t, nps, arg[0].([]interface{})[0].(string), agrs...)
	} else {
		socket_protocol.WriteTo(w, t, nps, arg...)
	}
	return w.Close()
}
