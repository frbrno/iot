package rpc

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
func (r *Request) Run(resource string) error {

	fn_cancel, event_sig, err := r.request(EventMethod_Run, resource, r.payload)
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
		if event.Status != EventStatus_Ack {
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
			switch event.Status {
			case EventStatus_Done:
				return nil
			case EventStatus_Cancel:
				return ErrCanceled
			case EventStatus_Error:
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

func (r *Request) Get(resource string) (*Event, error) {

	fn_cancel, event_sig, err := r.request(EventMethod_Get, resource, r.payload)
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
		if event.Status != EventStatus_Ack {
			return nil, ErrProtocol
		}
		return event, nil
	}
}

type Request struct {
	c            *Conn
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

func (r *Request) request(method string, resource string, payload []byte) (func(), <-chan *Event, error) {

	r.c.mu.Lock()

	if !r.ignore_offline {
		if !r.c.is_state_up {
			r.c.mu.Unlock()
			return nil, nil, ErrDisconnected
		}
	}

	if r.c.is_closed {
		r.c.mu.Unlock()
		return nil, nil, ErrDisconnected
	}

	var state_sig chan struct{}
	if r.ignore_offline {
		state_sig = make(chan struct{})
	} else {
		r.c.state_sig_list[state_sig] = false
	}

	token := r.c.token
	r.c.token++
	r.c.mu.Unlock()

	msg_nats_sig := make(chan *nats.Msg, 10)

	sub_subject := fmt.Sprintf("iot.%s.tx.%s.%s.%s.%s.%v", r.c.name_node, r.c.name_self, method, EventStatus_All, resource, token)
	sub, err := r.c.nc.ChanSubscribe(sub_subject, msg_nats_sig)
	if err != nil {
		return nil, nil, errors.Join(ErrProtocol, err)
	}

	pub_subject := fmt.Sprintf("iot.%s.rx.%s.%s.%s.%s.%v", r.c.name_node, r.c.name_self, method, EventStatus_Exec, resource, token)
	err = r.c.nc.Publish(pub_subject, payload)
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
			ev := &Event{
				Msg:      nil,
				Dst:      r.c.name_node,
				Src:      r.c.name_self,
				Method:   method,
				Status:   "",
				Resource: resource,
				Token:    token,
				Error:    nil,
			}
			select {
			case <-state_sig: //whatchdog 'p2p_token.get' gets no response
				ev.Status = EventStatus_Internal
				ev.Error = errors.Join(ErrDisconnected, errors.New("state_sig"))
			case <-unsubscribe_sig:
				ev.Status = EventStatus_Internal
				ev.Error = errors.Join(ErrDisconnected, errors.New("unsubscribe_sig"))
			case <-r.c.closed_sig: // c.Close() call
				ev.Status = EventStatus_Internal
				ev.Error = errors.Join(ErrDisconnected, errors.New("closed_sig"))
			case <-r.cancel_sig: // caller provided cancel channel
				ev.Status = EventStatus_Internal
				ev.Error = errors.Join(ErrDisconnected, errors.New("cancel_sig"))
			case msg_nats := <-msg_nats_sig:
				spl := strings.Split(msg_nats.Subject, ".")
				ev.Msg = msg_nats
				ev.Status = spl[5]

				switch ev.Status {
				case EventStatus_Cancel:
					ev.Error = ErrCanceled
				case EventStatus_Error:
					data := struct {
						Message string `json:"message"`
					}{}
					json.Unmarshal(msg_nats.Data, &data)
					ev.Error = errors.Join(ErrNode, errors.New("message: "+data.Message))
				case EventStatus_Done:
					if r.done_result != nil {
						err := json.Unmarshal(ev.Data, r.done_result)
						if err != nil {
							ev.Status = EventStatus_Internal
							ev.Error = errors.Join(ErrProtocol, err)
						}
					}
				case EventStatus_Ack:
					if r.result_ack != nil {
						err := json.Unmarshal(ev.Data, r.result_ack)
						if err != nil {
							ev.Status = EventStatus_Internal
							ev.Error = errors.Join(ErrProtocol, err)
						}
					}
				}
			}
			event_sig <- ev
			if ev.Error != nil {
				return
			}
		}
	}()

	var once sync.Once

	return func() {
		once.Do(func() {
			close(unsubscribe_sig)
			<-unsubscribe_done_sig
			close(event_sig)
			r.c.StateSigUnsubscribe(state_sig)

			// TODO: close msg_nats_sig?
			// I think yes, the caller should be aware of it because he is the one calling unsubscribe
		})
	}, event_sig, nil
}
