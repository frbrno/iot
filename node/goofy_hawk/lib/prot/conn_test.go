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

	peer1 := NewPeer(conn, name_rusty_falcon)

	_ = peer1
	time.Sleep(time.Minute * 30)
}

func TestRequest(t *testing.T) {
	conn, err := Connect(nats_url, name_self)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.nc.Close()

	peer1 := NewPeer(conn, name_rusty_falcon)

	gen_data_stepper1_move_to := func(position_final uint) []byte {
		return []byte(fmt.Sprintf(`{"position_final": %v,"step_delay_micros":1000}`, position_final))
	}
	done_sig := make(chan error, 1)

	err = peer1.Request().
		SetDoneSig(done_sig).
		SetPayload(gen_data_stepper1_move_to(20000)).
		Run("stepper1_move_to")

	if err != nil {
		t.Fatal(err)
	}

	err = <-done_sig
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

	peer1 := NewPeer(conn, name_rusty_falcon)

	err = peer1.IsOnOfflineSigBlocking(true, time.Second*6)
	if err != nil {
		t.Fatal("peer connect timeout")
	}

	gen_data_stepper1_move_to := func(position_final uint) []byte {
		return []byte(fmt.Sprintf(`{"position_final": %v,"step_delay_micros":1000}`, position_final))
	}
	position_final := uint(10000)
	for {
		done_sig := make(chan error, 1)

		err = peer1.Request().
			SetDoneSig(done_sig).
			SetPayload(gen_data_stepper1_move_to(position_final)).
			Run("stepper1_move_to")

		if err != nil {
			t.Fatal(err)
		}

		err = <-done_sig
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
