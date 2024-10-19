package prot

import (
	"fmt"
	"log"
	"sync"
	"time"
)

func NewPeer(conn *Conn, name string) *Peer {
	conn.mu.Lock()
	if p := conn.peers[name]; p != nil {
		conn.mu.Unlock()
		return p
	}

	p := &Peer{
		mu:                     conn.mu,
		conn:                   conn,
		name:                   name,
		is_close_sig:           make(chan struct{}),
		is_init_sig:            make(chan struct{}),
		is_on_offline_sig_list: make(map[chan struct{}]bool),
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
			p.is_online = true
			for is_online_sig, online := range p.is_on_offline_sig_list {
				if online {
					close(is_online_sig)
					delete(p.is_on_offline_sig_list, is_online_sig)
				}
			}
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
					for is_offline_sig, online := range p.is_on_offline_sig_list {
						if !online {
							close(is_offline_sig)
							delete(p.is_on_offline_sig_list, is_offline_sig)
						}
					}

					p.conn.mu.Lock()
					if p.is_closed {
						p.conn.mu.Unlock()
						return
					}
					p.is_online = false
					p.p2p_token = 0
					p.conn.mu.Unlock()
					break
				}
			}
		}
	}()

	return p
}

type Peer struct {
	mu        *sync.RWMutex //same as conn.mu
	conn      *Conn
	name      string
	p2p_token int64
	// need is/is_init_sig
	// NewPeer could be called concurrent
	// need to protect from panics
	// so many channels, TODO
	is_init                bool
	is_init_sig            chan struct{}
	is_online              bool
	is_closed              bool
	is_close_sig           chan struct{}
	is_on_offline_sig_list map[chan struct{}]bool
}

func (p *Peer) IsOnline() bool {
	p.conn.mu.RLock()
	defer p.conn.mu.RUnlock()

	return p.is_online
}

// IsOnOfflineSig the returned channel fires if the peer come online/offline
// call unsubscribe if u ignore the signal
func (p *Peer) IsOnOfflineSig(on_or_off bool) chan struct{} {
	is_on_offline_sig := make(chan struct{})
	p.mu.Lock()
	if !p.is_online {
		close(is_on_offline_sig)
		p.mu.Unlock()
		return is_on_offline_sig
	}
	p.is_on_offline_sig_list[is_on_offline_sig] = on_or_off
	p.mu.Unlock()
	return is_on_offline_sig
}

func (p *Peer) IsOnOfflineSigUnsubscribe(is_on_offline_sig chan struct{}) {
	p.mu.Lock()
	// TODO check has been closed, if not, close it, otherwise dangling ref
	// not sure if gc can handle this
	// ok for now
	delete(p.is_on_offline_sig_list, is_on_offline_sig)
	p.mu.Unlock()
}

func (p *Peer) Close() {
	p.conn.mu.Lock()
	if p.is_closed {
		p.conn.mu.Unlock()
		return
	}
	p.is_closed = true
	p.is_online = false
	delete(p.conn.peers, p.name)
	close(p.is_close_sig)
	for is_offline_sig, online := range p.is_on_offline_sig_list {
		if !online {
			close(is_offline_sig)
		}
		//here delete all online and offline references to the channels
		delete(p.is_on_offline_sig_list, is_offline_sig)
	}
	p.conn.mu.Unlock()
}

func (p *Peer) Request() *Request {
	return &Request{
		peer:         p,
		ack_timeout:  6 * time.Second,
		timeout_done: time.Hour * 9999,
	}
}
