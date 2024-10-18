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
func (r *Request) Run(event_name string) error {

	fn_cancel, sig_event, err := r.request(EventTyp_Run, event_name, r.payload)
	if err != nil {
		return err
	}

	select {
	case <-time.After(r.timeout_ack):
		return ErrTimeout
	case event := <-sig_event:
		if event.Error != nil {
			return event.Error
		}
		if event.Typ != EventTyp_Ack {
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
		case event := <-sig_event:
			switch event.Typ {
			case EventTyp_Done:
				return nil
			case EventTyp_Cancel:
				return ErrCanceled
			case EventTyp_Error:
				return event.Error
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

func (r *Request) Get(event_name string) (*Event, error) {

	fn_cancel, sig_event, err := r.request(EventTyp_Get, event_name, r.payload)
	if err != nil {
		return nil, err
	}
	defer fn_cancel()

	select {
	case <-time.After(r.timeout_ack):
		return nil, ErrTimeout
	case event := <-sig_event:
		if event.Error != nil {
			return nil, event.Error
		}
		if event.Typ != EventTyp_Ack {
			return nil, ErrProtocol
		}
		return event, nil
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
	ignore_offline bool
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

func (r *Request) request(event_typ EventTyp, event_name string, payload []byte) (func(), <-chan *Event, error) {

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

	sig_event_nats := make(chan *nats.Msg, 10)

	sub_subject := fmt.Sprintf("%s.%s.%s.%s.%v", r.peer.name, r.peer.conn.name_self, event_name, EventTyp_All, token)
	sub, err := r.peer.conn.nc.ChanSubscribe(sub_subject, sig_event_nats)
	if err != nil {
		return nil, nil, errors.Join(ErrProtocol, err)
	}

	pub_subject := fmt.Sprintf("%s.%s.%s.%s.%v", r.peer.conn.name_self, r.peer.name, event_name, event_typ, token)
	err = r.peer.conn.nc.Publish(pub_subject, payload)
	if err != nil {
		sub.Unsubscribe()
		return nil, nil, errors.Join(ErrProtocol, err)
	}

	sig_unsubscribe := make(chan struct{})
	sig_unsubscribe_done := make(chan struct{})
	sig_event := make(chan *Event, 10)

	go func() {
		defer close(sig_unsubscribe_done)
		defer sub.Unsubscribe()

		for {
			select {
			case <-sig_offline: //whatchdog 'p2p_token.get' gets no response
				sig_event <- &Event{
					Typ:   EventTyp_Internal,
					Error: errors.Join(ErrDisconnected, errors.New("sig_offline")),
				}
				return
			case <-sig_unsubscribe:
				sig_event <- &Event{
					Typ:   EventTyp_Internal,
					Error: errors.Join(ErrCanceled, errors.New("sig_unsubscribe")),
				}
				return
			case <-r.peer.sig_close: // peer.Close() call
				sig_event <- &Event{
					Typ:   EventTyp_Internal,
					Error: errors.Join(ErrDisconnected, errors.New("sig_close")),
				}
				return
			case <-r.sig_cancel: // caller provided cancel channel
				sig_event <- &Event{
					Typ:   EventTyp_Internal,
					Error: errors.Join(ErrCanceled, errors.New("sig_cancel")),
				}
				return
			case msg_nats := <-sig_event_nats:
				spl := strings.Split(msg_nats.Subject, ".")
				event := &Event{
					Msg:      msg_nats,
					Typ:      EventTyp(spl[3]), // na, dont need check slice boundaries; It is also guaranteed the name_peer only sends valid EventTyp's
					Name:     event_name,
					PeerName: r.peer.name,
				}
				switch event.Typ {
				case EventTyp_Cancel:
					event.Error = ErrCanceled
				case EventTyp_Error:
					data := struct {
						Message string `json:"message"`
					}{}
					json.Unmarshal(msg_nats.Data, &data)
					event.Error = errors.Join(ErrPeer, errors.New("message: "+data.Message))
				case EventTyp_Done:
					if r.result_done != nil {
						err := json.Unmarshal(event.Data, r.result_done)
						if err != nil {
							sig_event <- &Event{
								Typ:   EventTyp_Internal,
								Error: errors.Join(ErrProtocol, err),
							}
							return
						}
					}
				case EventTyp_Ack:
					if r.result_ack != nil {
						err := json.Unmarshal(event.Data, r.result_ack)
						if err != nil {
							sig_event <- &Event{
								Typ:   EventTyp_Internal,
								Error: errors.Join(ErrProtocol, err),
							}
							return
						}
					}
				}

				sig_event <- event
			}
		}
	}()

	var once sync.Once

	return func() {
		once.Do(func() {
			close(sig_unsubscribe)
			<-sig_unsubscribe_done
			close(sig_event)
			// TODO: close sig_event_nats?
			// I think yes, the caller should be aware of it because he is the one calling unsubscribe
		})
	}, sig_event, nil
}
