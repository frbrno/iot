package prot

import (
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

const nats_url = "192.168.10.124:4222"
const node_self = "goofy_hawk_test"
const node_rusty_falcon = "rusty_falcon"

func TestGet(t *testing.T) {
	nc, err := nats.Connect(nats_url)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()

	conn := NewConn(node_self, nc)

	t_begin := time.Now()
	msg, err := conn.Get(node_rusty_falcon, "stepper1_state", nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("success, reply_millis: %v, reply: %v", time.Since(t_begin).Milliseconds(), msg)
}

func TestRun(t *testing.T) {
	nc, err := nats.Connect(nats_url)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()

	conn := NewConn(node_self, nc)

	fn_cancel, sig_msg, err := conn.Run(node_rusty_falcon, "stepper1_move_to", []byte(`{"position_final": 20000,"step_delay_micros":1000}`))
	if err != nil {
		t.Fatal(err)
	}
	defer fn_cancel()

	// TODO: should this be done in conn.Run?
	select {
	case <-time.After(time.Second * 6):
		t.Fatal("timeout waiting for ack")
	case msg := <-sig_msg:
		if msg.Typ != MsgTyp_Ack {
			t.Fatal("protocol error, expected ack")
		}
	}

	select {
	case <-time.After(time.Minute * 2):
		t.Fatal("timeout waiting for done")
	case msg := <-sig_msg:
		switch msg.Typ {
		case MsgTyp_Done:
			t.Log("success")
		case MsgTyp_Cancel:
			t.Log("action got canceled, who did this?")
		case MsgTyp_Error:
			t.Log("ups, got error")
		default:
			t.Log("protocol error")
		}
	}
}

func TestRunHandler(t *testing.T) {
	nc, err := nats.Connect(nats_url)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()

	conn := NewConn(node_self, nc)

	gen_data_stepper1_move_to := func(position_final uint) []byte {
		return []byte(fmt.Sprintf(`{"position_final": %v,"step_delay_micros":1000}`, position_final))
	}
	sig_done := make(chan error, 1)

	err = conn.RunHandler().
		WithSigDone(sig_done).
		Run(
			node_rusty_falcon,
			"stepper1_move_to",
			gen_data_stepper1_move_to(35000),
		)

	if err != nil {
		t.Fatal(err)
	}

	err = <-sig_done
	if err != nil {
		t.Fatal(err)
	}

	t.Log("success")
}
