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
// note: if u provide a 'done_sig' channel, it is not blocking
func (r *Request) Run(event_name string) error {

	fn_cancel, event_sig, err := r.request(EventTyp_Run, event_name, r.payload)
	if err != nil {
		return err
	}

	select {
	case <-time.After(r.ack_timeout):
		return ErrTimeout
	case event := <-event_sig:
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
		case event := <-event_sig:
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

	if r.done_sig != nil {
		go func() {
			r.done_sig <- fn_recv()
		}()
		return nil
	}
	return fn_recv()
}

func (r *Request) Get(event_name string) (*Event, error) {

	fn_cancel, event_sig, err := r.request(EventTyp_Get, event_name, r.payload)
	if err != nil {
		return nil, err
	}
	defer fn_cancel()

	select {
	case <-time.After(r.ack_timeout):
		return nil, ErrTimeout
	case event := <-event_sig:
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
	ack_timeout  time.Duration
	timeout_done time.Duration
	payload      []byte
	result_ack   any
	done_result  any

	done_sig       chan error
	cancel_sig     chan struct{}
	ignore_offline bool
}

func (r *Request) SetPayload(b []byte) *Request {
	r.payload = b
	return r
}
func (r *Request) SetAckResult(v any) *Request {
	r.result_ack = v
	return r
}
func (r *Request) SetDoneResult(v any) *Request {
	r.done_result = v
	return r
}

func (r *Request) SetAckTimeout(t time.Duration) *Request {
	r.ack_timeout = t
	return r
}

func (r *Request) SetDoneSig(done_sig chan error) *Request {
	r.done_sig = done_sig
	return r
}

func (r *Request) SetCancelSig(cancel_sig chan struct{}) *Request {
	r.cancel_sig = cancel_sig
	return r
}

func (r *Request) SetDoneTimeout(t time.Duration) *Request {
	r.timeout_done = t
	return r
}

func (r *Request) setIgnoreOffline() *Request {
	r.ignore_offline = true
	return r
}

func (r *Request) request(event_typ EventTyp, event_name string, payload []byte) (func(), <-chan *Event, error) {

	r.peer.conn.mu.Lock()
	is_offline_sig := r.peer.is_offline_sig
	if !r.ignore_offline {
		if r.peer.is_closed || r.peer.is_offline {
			r.peer.conn.mu.Unlock()
			return nil, nil, ErrDisconnected
		}
	} else {
		is_offline_sig = make(chan struct{})
	}
	token := r.peer.conn.token
	r.peer.conn.token++
	r.peer.conn.mu.Unlock()

	msg_nats_sig := make(chan *nats.Msg, 10)

	sub_subject := fmt.Sprintf("%s.%s.%s.%s.%v", r.peer.name, r.peer.conn.name_self, event_name, EventTyp_All, token)
	sub, err := r.peer.conn.nc.ChanSubscribe(sub_subject, msg_nats_sig)
	if err != nil {
		return nil, nil, errors.Join(ErrProtocol, err)
	}

	pub_subject := fmt.Sprintf("%s.%s.%s.%s.%v", r.peer.conn.name_self, r.peer.name, event_name, event_typ, token)
	err = r.peer.conn.nc.Publish(pub_subject, payload)
	if err != nil {
		sub.Unsubscribe()
		return nil, nil, errors.Join(ErrProtocol, err)
	}

	unsubscribe_sig := make(chan struct{})
	unsubscribe_done_sig := make(chan struct{})
	event_sig := make(chan *Event, 10)

	go func() {
		defer close(unsubscribe_done_sig)
		defer sub.Unsubscribe()

		for {
			select {
			case <-is_offline_sig: //whatchdog 'p2p_token.get' gets no response
				event_sig <- &Event{
					Typ:   EventTyp_Internal,
					Error: errors.Join(ErrDisconnected, errors.New("is_offline_sig")),
				}
				return
			case <-unsubscribe_sig:
				event_sig <- &Event{
					Typ:   EventTyp_Internal,
					Error: errors.Join(ErrCanceled, errors.New("unsubscribe_sig")),
				}
				return
			case <-r.peer.is_close_sig: // peer.Close() call
				event_sig <- &Event{
					Typ:   EventTyp_Internal,
					Error: errors.Join(ErrDisconnected, errors.New("is_close_sig")),
				}
				return
			case <-r.cancel_sig: // caller provided cancel channel
				event_sig <- &Event{
					Typ:   EventTyp_Internal,
					Error: errors.Join(ErrCanceled, errors.New("cancel_sig")),
				}
				return
			case msg_nats := <-msg_nats_sig:
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
					if r.done_result != nil {
						err := json.Unmarshal(event.Data, r.done_result)
						if err != nil {
							event_sig <- &Event{
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
							event_sig <- &Event{
								Typ:   EventTyp_Internal,
								Error: errors.Join(ErrProtocol, err),
							}
							return
						}
					}
				}

				event_sig <- event
			}
		}
	}()

	var once sync.Once

	return func() {
		once.Do(func() {
			close(unsubscribe_sig)
			<-unsubscribe_done_sig
			close(event_sig)
			// TODO: close msg_nats_sig?
			// I think yes, the caller should be aware of it because he is the one calling unsubscribe
		})
	}, event_sig, nil
}
