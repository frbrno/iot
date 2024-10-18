package prot

import (
	"errors"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

var ErrCanceled = errors.New("canceled")
var ErrTimeout = errors.New("timeout")
var ErrProtocol = errors.New("protocol")
var ErrPeer = errors.New("peer")
var ErrDisconnected = errors.New("disconnected")

func NewConn(name_self string, nc *nats.Conn) *Conn {
	c := new(Conn)
	c.nc = nc
	c.mu = new(sync.RWMutex)
	c.name_self = name_self

	return c
}

func Connect(url string, name_self string) (*Conn, error) {
	o := nats.GetDefaultOptions()
	o.Name = name_self
	o.MaxReconnect = -1
	o.PingInterval = time.Second * 10
	o.Url = url
	o.DisconnectedErrCB = func(c *nats.Conn, err error) {

	}
	o.ReconnectedCB = func(c *nats.Conn) {

	}
	nc, err := o.Connect()
	if err != nil {
		return nil, err
	}

	c := new(Conn)
	c.nc = nc
	c.mu = new(sync.RWMutex)
	c.name_self = name_self
	c.peers = make(map[string]*Peer)

	return c, nil
}

type Conn struct {
	mu        *sync.RWMutex
	nc        *nats.Conn
	name_self string
	token     uint
	peers     map[string]*Peer
}

type EventTyp string
type ActionTyp string

const (
	EventTyp_All      = "*"
	EventTyp_Ack      = "ack"
	EventTyp_Error    = "error"
	EventTyp_Done     = "done"
	EventTyp_Cancel   = "cancel"
	EventTyp_Get      = "get"
	EventTyp_Set      = "set"
	EventTyp_Run      = "run"
	EventTyp_Internal = "internal"
)

type Event struct {
	*nats.Msg
	Typ      EventTyp
	Name     string
	PeerName string
	Error    error
}
