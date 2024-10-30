package rpc

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

var ErrCanceled = errors.New("canceled")
var ErrTimeout = errors.New("timeout")
var ErrProtocol = errors.New("protocol")
var ErrNode = errors.New("node")
var ErrDisconnected = errors.New("disconnected")

func Dial(nc *nats.Conn, name_self, name_node string) (*Conn, error) {
	var err error
	c := new(Conn)
	c.nc = nc
	c.mu = new(sync.RWMutex)
	c.name_node = name_node
	c.name_self = name_self
	c.closed_sig = make(chan struct{})

	c.state_sig_list = make(map[chan struct{}]bool)
	c.js, err = jetstream.New(c.nc)
	if err != nil {
		return nil, err
	}

	//establish connection
	go func() {
		ticker := time.NewTicker(time.Second * 6)
		for {
			p2p_token := time.Now().Unix()

			err := c.Request().
				setIgnoreOffline().
				SetCancelSig(c.closed_sig).
				SetDoneTimeout(time.Second * 6).
				SetPayload([]byte(fmt.Sprintf(`{"p2p_token":%v}`, p2p_token))).
				Run("p2p_init")

			if err != nil {
				select {
				case <-ticker.C:
					continue
				case <-c.closed_sig:
					return
				}
			}

			c.mu.Lock()
			if c.is_closed {
				c.mu.Unlock()
				return
			}

			c.p2p_token = p2p_token
			c.is_state_up = true
			for is_online_sig, online := range c.state_sig_list {
				if online {
					close(is_online_sig)
					delete(c.state_sig_list, is_online_sig)
				}
			}
			c.mu.Unlock()

			for {
				select {
				case <-c.closed_sig:
					return
				case <-ticker.C:
				}

				data := struct {
					P2PToken int64 `json:"p2p_token"`
				}{}

				msg, err := c.Request().
					SetAckResult(&data).
					SetAckTimeout(time.Second * 6).
					Get("watchdog")

				success := true
				if success && err != nil {
					success = false
				}
				if success && msg.Status != EventStatus_Ack {
					success = false
				}
				if success && data.P2PToken != p2p_token {
					success = false
				}

				if !success {
					c.mu.Lock()
					if c.is_closed {
						c.mu.Unlock()
						return
					}

					c.is_state_up = false
					c.p2p_token = 0
					for state_sig, online := range c.state_sig_list {
						if !online {
							close(state_sig)
							delete(c.state_sig_list, state_sig)
						}
					}
					c.mu.Unlock()
					break
				}
			}
		}
	}()

	return c, nil
}

type Conn struct {
	mu             *sync.RWMutex
	nc             *nats.Conn
	js             jetstream.JetStream
	name_self      string
	name_node      string
	token          uint
	p2p_token      int64
	is_state_up    bool
	is_closed      bool
	closed_sig     chan struct{}
	state_sig_list map[chan struct{}]bool
}

func (c *Conn) JetStream() jetstream.JetStream {
	return c.js
}

func (c *Conn) StateUp() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.is_state_up
}

// StateSig the returned channel fires if the peer becomes online/offline
// call unsubscribe if u ignore the signal
// it is guaranteed to detect a disconnect,
// since a changed p2p_token triggers disconnect
func (c *Conn) StateSig(up_or_down bool) chan struct{} {
	state_sig := make(chan struct{})
	c.mu.Lock()
	if up_or_down && c.is_state_up || !up_or_down && !c.is_state_up {
		close(state_sig)
		c.mu.Unlock()
		return state_sig
	}
	c.state_sig_list[state_sig] = up_or_down
	c.mu.Unlock()
	return state_sig
}

func (c *Conn) StateSigWithTimeout(up_or_down bool, timeout time.Duration) error {
	state_sig := c.StateSig(up_or_down)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-timer.C:
		c.StateSigUnsubscribe(state_sig)
		return ErrTimeout
	case <-state_sig:
		return nil
	}
}

func (c *Conn) StateSigUnsubscribe(state_sig chan struct{}) {
	c.mu.Lock()
	// TODO check has been closed, if not, close it, otherwise dangling ref
	// not sure if gc can handle this
	// ok for now
	delete(c.state_sig_list, state_sig)
	c.mu.Unlock()
}

func (c *Conn) Close() {
	c.mu.Lock()
	if c.is_closed {
		c.mu.Unlock()
		return
	}
	c.is_closed = true
	c.is_state_up = false
	close(c.closed_sig)
	for state_sig, online := range c.state_sig_list {
		if !online {
			close(state_sig)
		}
		//here delete all online and offline references to the channels
		delete(c.state_sig_list, state_sig)
	}
	c.mu.Unlock()
}

func (c *Conn) Request() *Request {
	return &Request{
		c:            c,
		ack_timeout:  6 * time.Second,
		timeout_done: time.Hour * 9999,
	}
}

const (
	EventStatus_All      = "*"
	EventStatus_Exec     = "exec"
	EventStatus_Ack      = "ack"
	EventStatus_Error    = "error"
	EventStatus_Done     = "done"
	EventStatus_Cancel   = "cancel"
	EventStatus_Internal = "internal"

	EventMethod_Get = "get"
	EventMethod_Set = "set"
	EventMethod_Run = "run"
)

type Event struct {
	*nats.Msg

	Dst      string
	Src      string
	Method   string
	Status   string
	Resource string
	Token    uint

	Error error
}
