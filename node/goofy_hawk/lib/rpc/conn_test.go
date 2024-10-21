package rpc

import (
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

const nats_url = "192.168.10.124:4222"
const name_self = "goofy_hawk_test"
const name_rusty_falcon = "rusty_falcon"

func TestPeer(t *testing.T) {
	o := nats.GetDefaultOptions()
	o.Name = name_self
	o.MaxReconnect = -1
	o.PingInterval = time.Second * 10
	o.Url = nats_url
	nc, err := o.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	conn, err := Dial(nc, name_self, name_rusty_falcon)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.nc.Close()

	_ = conn
	time.Sleep(time.Minute * 30)
}

func TestRequest(t *testing.T) {
	o := nats.GetDefaultOptions()
	o.Name = name_self
	o.MaxReconnect = -1
	o.PingInterval = time.Second * 10
	o.Url = nats_url
	nc, err := o.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	conn, err := Dial(nc, name_self, name_rusty_falcon)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	gen_data_stepper1_move_to := func(position_final uint) []byte {
		return []byte(fmt.Sprintf(`{"position_final": %v,"step_delay_micros":1000}`, position_final))
	}
	done_sig := make(chan error, 1)

	err = conn.Request().
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

// go test -v -run=TestRunHandlerInfinite -timeout 10h lib/rpc/*.go
func TestRequestInfinite(t *testing.T) {
	o := nats.GetDefaultOptions()
	o.Name = name_self
	o.MaxReconnect = -1
	o.PingInterval = time.Second * 10
	o.Url = nats_url
	nc, err := o.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	conn, err := Dial(nc, name_self, name_rusty_falcon)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	err = conn.StateSigWithTimeout(true, time.Second*6)
	if err != nil {
		t.Fatal("node connect timeout")
	}

	gen_data_stepper1_move_to := func(position_final uint) []byte {
		return []byte(fmt.Sprintf(`{"position_final": %v,"step_delay_micros":1000}`, position_final))
	}
	position_final := uint(10000)
	for {
		done_sig := make(chan error, 1)

		err = conn.Request().
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
