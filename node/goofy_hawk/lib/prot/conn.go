package prot

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

func NewConn(node_self string, nc *nats.Conn) *Conn {
	c := new(Conn)
	c.nc = nc
	c.mu = new(sync.RWMutex)
	c.node_self = node_self

	return c
}

type Conn struct {
	mu        *sync.RWMutex
	nc        *nats.Conn
	node_self string
	token     uint
}

type MsgTyp string
type ActionTyp string

const (
	MsgTyp_All    = "*"
	MsgTyp_Ack    = "ack"
	MsgTyp_Error  = "error"
	MsgTyp_Done   = "done"
	MsgTyp_Cancel = "cancel"
	MsgTyp_Get    = "get"
	MsgTyp_Set    = "set"
	MsgTyp_Run    = "run"
)

type Msg struct {
	*nats.Msg
	Typ    MsgTyp
	Action string
	Node   string

	Origin *Msg
}

func (c *Conn) Get(node string, action string, payload []byte) (*Msg, error) {
	origin := &Msg{
		Typ:    MsgTyp_Get,
		Action: action,
		Node:   c.node_self,
	}

	c.mu.Lock()
	token := c.token
	c.token++
	c.mu.Unlock()
	//rusty_falcon.goofy_hawk.stepper1_state.ack.2
	sig_msg := make(chan *nats.Msg, 1)
	sub, err := c.nc.ChanSubscribe(fmt.Sprintf("%s.%s.%s.%s.%v", node, c.node_self, action, MsgTyp_Ack, token), sig_msg)
	if err != nil {
		return nil, err
	}
	defer sub.Unsubscribe()

	// goofy_hawk.rusty_falcon.stepper1_state.get.2
	err = c.nc.Publish(fmt.Sprintf("%s.%s.%s.%s.%v", c.node_self, node, action, MsgTyp_Get, token), payload)
	if err != nil {
		return nil, err
	}

	timeout := time.NewTimer(time.Second * 6)
	defer timeout.Stop()

	select {
	case <-timeout.C:
		return nil, fmt.Errorf("timeout waiting for reply")
	case msg_nats := <-sig_msg:
		msg := &Msg{
			Msg:    msg_nats,
			Typ:    MsgTyp_Ack,
			Action: action,
			Node:   node,
			Origin: origin,
		}

		return msg, nil
	}
}

func (c *Conn) Run(node string, action string, payload []byte) (func(), <-chan *Msg, error) {
	origin := &Msg{
		Typ:    MsgTyp_Run,
		Action: action,
		Node:   c.node_self,
	}

	c.mu.Lock()
	token := c.token
	c.token++
	c.mu.Unlock()
	//rusty_falcon.goofy_hawk.stepper1_state.ack.2
	sig_msg_nats := make(chan *nats.Msg, 10)
	sub, err := c.nc.ChanSubscribe(fmt.Sprintf("%s.%s.%s.%s.%v", node, c.node_self, action, MsgTyp_All, token), sig_msg_nats)
	if err != nil {
		return nil, nil, err
	}
	//defer sub.Unsubscribe()

	err = c.nc.Publish(fmt.Sprintf("%s.%s.%s.%s.%v", c.node_self, node, action, MsgTyp_Run, token), payload)
	if err != nil {
		sub.Unsubscribe()
		return nil, nil, err
	}

	sig_cancel := make(chan struct{})
	sig_cancel_done := make(chan struct{})
	sig_msg := make(chan *Msg, 10)

	// TODO: here we should atleast check for rx ack from node

	go func() {
		defer close(sig_cancel_done)
		for {
			select {
			case <-sig_cancel:
				return
			case msg_nats := <-sig_msg_nats:
				spl := strings.Split(msg_nats.Subject, ".")
				msg := &Msg{
					Msg:    msg_nats,
					Typ:    MsgTyp(spl[3]), // na, dont need check slice boundaries; It is also guaranteed the node only sends valid MsgTyp's
					Action: action,
					Node:   node,
					Origin: origin,
				}
				sig_msg <- msg
			}
		}
	}()

	return func() {
		// TODO: just call it once! for now ok
		sub.Unsubscribe()
		close(sig_cancel)
		<-sig_cancel_done
		close(sig_msg)
		// TODO: close sig_msg_nats?
	}, sig_msg, nil

}
