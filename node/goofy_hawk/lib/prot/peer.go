package prot

import (
	"fmt"
	"log"
	"time"
)

func NewPeer(conn *Conn, name string) (*Peer, error) {
	conn.mu.Lock()
	if p := conn.peers[name]; p != nil {
		is_init, sig_init := p.is_init, p.sig_init
		conn.mu.Unlock()
		if !is_init {
			select {
			case <-time.After(time.Second * 10):
				return nil, ErrDisconnected
			case <-sig_init:
				return p, nil
			}
		}
		return p, nil
	}

	p := &Peer{
		conn:      conn,
		name:      name,
		sig_close: make(chan struct{}),
		sig_init:  make(chan struct{}),
	}

	conn.peers[name] = p
	conn.mu.Unlock()

	//establish connection
	go func() {
		ticker := time.NewTicker(time.Second * 6)
		for {
			p2p_token := time.Now().Unix()

			err := p.Request().
				setIgnoreOffline().
				SetTimeoutDone(time.Second * 6).
				SetPayload([]byte(fmt.Sprintf(`{"p2p_token":%v}`, p2p_token))).
				Run("p2p_init")

			if err != nil {
				log.Printf("[%s] p2p_init failed, err: %s", p.name, err.Error())
				select {
				case <-ticker.C:
					continue
				case <-p.sig_close:
					return
				}
			}

			log.Printf("[%s] p2p_init success", p.name)

			p.conn.mu.Lock()
			if p.is_closed {
				p.conn.mu.Unlock()
				return
			}
			if !p.is_init {
				p.is_init = true
				close(p.sig_init)
			}
			p.p2p_token = p2p_token
			p.is_offline = false
			p.sig_offline = make(chan struct{})
			p.conn.mu.Unlock()

			for {
				select {
				case <-p.sig_close:
					return
				case <-ticker.C:
				}

				data := struct {
					P2PToken int64 `json:"p2p_token"`
				}{}

				msg, err := p.Request().
					SetResultAck(&data).
					SetTimeoutAck(time.Second * 6).
					Get("p2p_token")

				success := true
				if success && err != nil {
					success = false
				}
				if success && msg.Typ != MsgTyp_Ack {
					success = false
				}
				if success && data.P2PToken != p2p_token {
					success = false
				}

				if !success {
					log.Printf("[%s] p2p_token watchdog fail", p.name)

					p.conn.mu.Lock()
					if p.is_closed {
						p.conn.mu.Unlock()
						return
					}
					p.is_offline = true
					close(p.sig_offline)
					p.p2p_token = 0
					p.conn.mu.Unlock()
					break
				}
			}
		}
	}()

	select {
	case <-time.After(time.Second * 10):
		p.Close()
		return nil, ErrDisconnected
	case <-p.sig_init:
		return p, nil
	}
}

type Peer struct {
	conn      *Conn
	name      string
	p2p_token int64
	// need is/sig_init
	// NewPeer could be called concurrent
	// need to protect from panics
	// so many channels, TODO
	is_init     bool
	sig_init    chan struct{}
	is_offline  bool
	sig_offline chan struct{}
	is_closed   bool
	sig_close   chan struct{}
}

func (p *Peer) Close() {
	p.conn.mu.Lock()
	if p.is_closed {
		//already closed
		p.conn.mu.Unlock()
		return
	}
	p.is_closed = true
	p.is_offline = true
	delete(p.conn.peers, p.name)
	close(p.sig_close)
	p.conn.mu.Unlock()
}

func (p *Peer) Request() *Request {
	return &Request{
		peer:         p,
		timeout_ack:  6 * time.Second,
		timeout_done: time.Hour * 9999,
	}
}
