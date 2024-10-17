package prot

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

// Run helps to write less code, returns nil on success/done.
// note: if u provide a 'sig_done' channel, it is not blocking
func (r *Request) Run(action string) error {

	fn_cancel, sig_msg, err := r.request(MsgTyp_Run, action, r.payload)
	if err != nil {
		return err
	}

	select {
	case <-time.After(r.timeout_ack):
		return ErrTimeout
	case msg := <-sig_msg:
		if msg.Error != nil {
			return msg.Error
		}
		if msg.Typ != MsgTyp_Ack {
			return ErrProtocol
		}
	}

	timeout := time.NewTimer(r.timeout_done)

	fn_recv := func() error {
		defer timeout.Stop()
		defer fn_cancel()

		select {
		case <-timeout.C:
			return ErrTimeout
		case msg := <-sig_msg:
			switch msg.Typ {
			case MsgTyp_Done:
				if r.callback_done != nil {
					r.callback_done(msg.Data)
				}
				return nil
			case MsgTyp_Cancel:
				return ErrCanceled
			case MsgTyp_Error:
				return msg.Error
			default:
				return ErrProtocol
			}
		}
	}

	if r.sig_done != nil {
		go func() {
			r.sig_done <- fn_recv()
		}()
		return nil
	}
	return fn_recv()
}

func (r *Request) Get(action string) (*Msg, error) {

	fn_cancel, sig_msg, err := r.request(MsgTyp_Get, action, r.payload)
	if err != nil {
		return nil, err
	}
	defer fn_cancel()

	select {
	case <-time.After(r.timeout_ack):
		return nil, ErrTimeout
	case msg := <-sig_msg:
		if msg.Error != nil {
			return nil, msg.Error
		}
		if msg.Typ != MsgTyp_Ack {
			return nil, ErrProtocol
		}
		return msg, nil
	}
}

type Request struct {
	peer         *Peer
	timeout_ack  time.Duration
	timeout_done time.Duration
	payload      []byte
	result_ack   any
	result_done  any

	sig_done       chan error
	sig_cancel     chan struct{}
	callback_done  func([]byte)
	ignore_offline bool

	result any
}

func (r *Request) SetPayload(b []byte) *Request {
	r.payload = b
	return r
}
func (r *Request) SetResultAck(v any) *Request {
	r.result_ack = v
	return r
}
func (r *Request) SetResultDone(v any) *Request {
	r.result_done = v
	return r
}

func (r *Request) SetTimeoutAck(t time.Duration) *Request {
	r.timeout_ack = t
	return r
}

func (r *Request) SetSigDone(sig_done chan error) *Request {
	r.sig_done = sig_done
	return r
}

// WithCallbackDone useful to get the payload, gets called before sig_done fires
func (r *Request) SetCallbackDone(callback_done func([]byte)) *Request {
	r.callback_done = callback_done
	return r
}

func (r *Request) SetSigCancel(sig_cancel chan struct{}) *Request {
	r.sig_cancel = sig_cancel
	return r
}

func (r *Request) SetTimeoutDone(t time.Duration) *Request {
	r.timeout_done = t
	return r
}

func (r *Request) setIgnoreOffline() *Request {
	r.ignore_offline = true
	return r
}

func (r *Request) request(msg_typ MsgTyp, action string, payload []byte) (func(), <-chan *Msg, error) {

	r.peer.conn.mu.Lock()
	sig_offline := r.peer.sig_offline
	if !r.ignore_offline {
		if r.peer.is_closed || r.peer.is_offline {
			r.peer.conn.mu.Unlock()
			return nil, nil, ErrDisconnected
		}
	} else {
		sig_offline = make(chan struct{})
	}
	token := r.peer.conn.token
	r.peer.conn.token++
	r.peer.conn.mu.Unlock()

	sig_msg_nats := make(chan *nats.Msg, 10)

	sub_subject := fmt.Sprintf("%s.%s.%s.%s.%v", r.peer.name, r.peer.conn.name_self, action, MsgTyp_All, token)
	sub, err := r.peer.conn.nc.ChanSubscribe(sub_subject, sig_msg_nats)
	if err != nil {
		return nil, nil, errors.Join(ErrProtocol, err)
	}

	pub_subject := fmt.Sprintf("%s.%s.%s.%s.%v", r.peer.conn.name_self, r.peer.name, action, msg_typ, token)
	err = r.peer.conn.nc.Publish(pub_subject, payload)
	if err != nil {
		sub.Unsubscribe()
		return nil, nil, errors.Join(ErrProtocol, err)
	}

	sig_unsubscribe := make(chan struct{})
	sig_unsubscribe_done := make(chan struct{})
	sig_msg := make(chan *Msg, 10)

	go func() {
		defer close(sig_unsubscribe_done)
		defer sub.Unsubscribe()

		for {
			select {
			case <-sig_offline: //whatchdog 'p2p_token.get' gets no response
				sig_msg <- &Msg{
					Typ:   MsgTyp_Internal,
					Error: errors.Join(ErrDisconnected, errors.New("sig_offline")),
				}
				return
			case <-sig_unsubscribe:
				sig_msg <- &Msg{
					Typ:   MsgTyp_Internal,
					Error: errors.Join(ErrCanceled, errors.New("sig_unsubscribe")),
				}
				return
			case <-r.peer.sig_close: // peer.Close() call
				sig_msg <- &Msg{
					Typ:   MsgTyp_Internal,
					Error: errors.Join(ErrDisconnected, errors.New("sig_close")),
				}
				return
			case <-r.sig_cancel: // caller provided cancel channel
				sig_msg <- &Msg{
					Typ:   MsgTyp_Internal,
					Error: errors.Join(ErrCanceled, errors.New("sig_cancel")),
				}
				return
			case msg_nats := <-sig_msg_nats:
				spl := strings.Split(msg_nats.Subject, ".")
				msg := &Msg{
					Msg:    msg_nats,
					Typ:    MsgTyp(spl[3]), // na, dont need check slice boundaries; It is also guaranteed the name_peer only sends valid MsgTyp's
					Action: action,
					Name:   r.peer.name,
				}
				switch msg.Typ {
				case MsgTyp_Cancel:
					msg.Error = ErrCanceled
				case MsgTyp_Error:
					data := struct {
						Message string `json:"message"`
					}{}
					json.Unmarshal(msg_nats.Data, &data)
					msg.Error = errors.Join(ErrPeer, errors.New("message: "+data.Message))
				case MsgTyp_Done:
					if r.result_done != nil {
						err := json.Unmarshal(msg.Data, r.result_done)
						if err != nil {
							sig_msg <- &Msg{
								Typ:   MsgTyp_Internal,
								Error: errors.Join(ErrProtocol, err),
							}
							return
						}
					}
				case MsgTyp_Ack:
					if r.result_ack != nil {
						err := json.Unmarshal(msg.Data, r.result_ack)
						if err != nil {
							sig_msg <- &Msg{
								Typ:   MsgTyp_Internal,
								Error: errors.Join(ErrProtocol, err),
							}
							return
						}
					}
				}

				sig_msg <- msg
			}
		}
	}()

	var once sync.Once

	return func() {
		once.Do(func() {
			close(sig_unsubscribe)
			<-sig_unsubscribe_done
			close(sig_msg)
			// TODO: close sig_msg_nats?
			// I think yes, the caller should be aware of it because he is the one calling unsubscribe
		})
	}, sig_msg, nil
}
