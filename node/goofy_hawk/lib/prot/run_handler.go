package prot

import (
	"encoding/json"
	"errors"
	"time"
)

func NewRunHandler(conn *Conn) *runHandler {
	return &runHandler{
		conn:         conn,
		timeout_ack:  6 * time.Second,
		timeout_done: time.Hour * 9999,
	}
}

// Run helps to write less code, returns nil on success/done.
// note: if u provide a 'sig_done' channel, it is not blocking
func (rh *runHandler) Run(node string, action string, payload []byte) error {

	fn_cancel, sig_msg, err := rh.conn.Run(node, action, payload)
	if err != nil {
		return err
	}

	select {
	case <-time.After(rh.timeout_ack):
		return ErrTimeout
	case msg := <-sig_msg:
		if msg.Typ != MsgTyp_Ack {
			return ErrProtocol
		}
	}

	timeout := time.NewTimer(rh.timeout_done)

	fn_recv := func() error {
		defer timeout.Stop()
		defer fn_cancel()

		select {
		case <-timeout.C:
			return ErrTimeout
		case <-rh.sig_cancel:
			return ErrCanceled
		case msg := <-sig_msg:
			switch msg.Typ {
			case MsgTyp_Done:
				if rh.callback_done != nil {
					rh.callback_done(msg.Data)
				}
				return nil
			case MsgTyp_Cancel:
				return ErrCanceled
			case MsgTyp_Error:
				data := struct {
					Message string `json:"message"`
				}{}
				json.Unmarshal(msg.Data, &data)
				return errors.Join(ErrGeneric, errors.New(data.Message))
			default:
				return ErrProtocol
			}
		}
	}

	if rh.sig_done != nil {
		go func() {
			rh.sig_done <- fn_recv()
		}()
		return nil
	}
	return fn_recv()
}

type runHandler struct {
	conn         *Conn
	timeout_ack  time.Duration
	timeout_done time.Duration

	sig_done      chan error
	sig_cancel    chan struct{}
	callback_done func([]byte)
}

func (rh *runHandler) WithTimeoutAck(t time.Duration) *runHandler {
	rh.timeout_ack = t
	return rh
}

func (rh *runHandler) WithSigDone(sig_done chan error) *runHandler {
	rh.sig_done = sig_done
	return rh
}

// WithCallbackDone useful to get the payload, gets called before sig_done fires
func (rh *runHandler) WithCallbackDone(callback_done func([]byte)) *runHandler {
	rh.callback_done = callback_done
	return rh
}

func (rh *runHandler) WithSigCancel(sig_cancel chan struct{}) *runHandler {
	rh.sig_cancel = sig_cancel
	return rh
}

func (rh *runHandler) WithTimeoutDone(t time.Duration) *runHandler {
	rh.timeout_done = t
	return rh
}
