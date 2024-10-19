package prot

import (
	"fmt"
	"log"
	"time"
)

func NewPeer(conn *Conn, name string) (*Peer, error) {
	conn.mu.Lock()
	if p := conn.peers[name]; p != nil {
		is_init, is_init_sig := p.is_init, p.is_init_sig
		conn.mu.Unlock()
		if !is_init {
			select {
			case <-time.After(time.Second * 10):
				return nil, ErrDisconnected
			case <-is_init_sig:
				return p, nil
			}
		}
		return p, nil
	}

	p := &Peer{
		conn:         conn,
		name:         name,
		is_close_sig: make(chan struct{}),
		is_init_sig:  make(chan struct{}),
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
				SetCancelSig(p.is_close_sig).
				SetDoneTimeout(time.Second * 6).
				SetPayload([]byte(fmt.Sprintf(`{"p2p_token":%v}`, p2p_token))).
				Run("p2p_init")

			if err != nil {
				log.Printf("[%s] p2p_init failed, err: %s", p.name, err.Error())
				select {
				case <-ticker.C:
					continue
				case <-p.is_close_sig:
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
				close(p.is_init_sig)
			}
			p.p2p_token = p2p_token
			p.is_offline = false
			p.is_offline_sig = make(chan struct{})
			p.conn.mu.Unlock()

			for {
				select {
				case <-p.is_close_sig:
					return
				case <-ticker.C:
				}

				data := struct {
					P2PToken int64 `json:"p2p_token"`
				}{}

				msg, err := p.Request().
					SetAckResult(&data).
					SetAckTimeout(time.Second * 6).
					Get("p2p_token")

				success := true
				if success && err != nil {
					success = false
				}
				if success && msg.Typ != EventTyp_Ack {
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
					close(p.is_offline_sig)
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
	case <-p.is_init_sig:
		return p, nil
	}
}

type Peer struct {
	conn      *Conn
	name      string
	p2p_token int64
	// need is/is_init_sig
	// NewPeer could be called concurrent
	// need to protect from panics
	// so many channels, TODO
	is_init        bool
	is_init_sig    chan struct{}
	is_offline     bool
	is_offline_sig chan struct{}
	is_closed      bool
	is_close_sig   chan struct{}
}

func (p *Peer) IsOnline() bool {
	p.conn.mu.RLock()
	defer p.conn.mu.RUnlock()

	return !p.is_offline
}

func (p *Peer) Close() {
	p.conn.mu.Lock()
	if p.is_closed {
		p.conn.mu.Unlock()
		return
	}
	p.is_closed = true
	p.is_offline = true
	delete(p.conn.peers, p.name)
	close(p.is_close_sig)
	p.conn.mu.Unlock()
}

func (p *Peer) Request() *Request {
	return &Request{
		peer:         p,
		ack_timeout:  6 * time.Second,
		timeout_done: time.Hour * 9999,
	}
}
