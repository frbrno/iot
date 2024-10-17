package prot

import (
	"fmt"
	"testing"
	"time"
)

const nats_url = "192.168.10.124:4222"
const name_self = "goofy_hawk_test"
const name_rusty_falcon = "rusty_falcon"

func TestPeer(t *testing.T) {
	conn, err := Connect(nats_url, name_self)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.nc.Close()

	peer1, err := NewPeer(conn, name_rusty_falcon)
	if err != nil {
		t.Fatal(err)
	}
	_ = peer1
	time.Sleep(time.Minute * 30)
}

func TestRequest(t *testing.T) {
	conn, err := Connect(nats_url, name_self)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.nc.Close()

	peer1, err := NewPeer(conn, name_rusty_falcon)
	if err != nil {
		t.Fatal(err)
	}

	gen_data_stepper1_move_to := func(position_final uint) []byte {
		return []byte(fmt.Sprintf(`{"position_final": %v,"step_delay_micros":1000}`, position_final))
	}
	sig_done := make(chan error, 1)

	err = peer1.Request().
		SetSigDone(sig_done).
		SetPayload(gen_data_stepper1_move_to(20000)).
		Run("stepper1_move_to")

	if err != nil {
		t.Fatal(err)
	}

	err = <-sig_done
	if err != nil {
		t.Fatal(err)
	}

	t.Log("success")
}

// go test -v -run=TestRunHandlerInfinite -timeout 10h lib/prot/*.go
func TestRequestInfinite(t *testing.T) {
	conn, err := Connect(nats_url, name_self)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.nc.Close()

	peer1, err := NewPeer(conn, name_rusty_falcon)
	if err != nil {
		t.Fatal(err)
	}

	gen_data_stepper1_move_to := func(position_final uint) []byte {
		return []byte(fmt.Sprintf(`{"position_final": %v,"step_delay_micros":1000}`, position_final))
	}
	position_final := uint(10000)
	for {
		sig_done := make(chan error, 1)

		err = peer1.Request().
			SetSigDone(sig_done).
			SetPayload(gen_data_stepper1_move_to(position_final)).
			Run("stepper1_move_to")

		if err != nil {
			t.Fatal(err)
		}

		err = <-sig_done
		if err != nil {
			t.Fatal(err)
		}

		t.Log("success")

		if position_final >= 20000 {
			position_final = 10000
		} else {
			position_final = 20000
		}
	}
}
