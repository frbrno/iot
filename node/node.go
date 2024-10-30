package node

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/jirenius/go-res"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var Hostname string

func Init() {
	log.Logger = log.With().Caller().Logger().Output(zerolog.ConsoleWriter{Out: os.Stderr})

	var err error
	Hostname, err = os.Hostname()
	if err != nil {
		log.Fatal().Msgf("hostname err: %s", err)
	}
	if Hostname == "" {
		log.Fatal().Msg("hostname not set")
	}

	ips, _ := net.InterfaceAddrs()

	log.Info().Str("foo", "bahhhhh").
		Msgf("Hello world hostname: %s, time: %s, local-addrs: %v",
			Hostname,
			time.Now().Format(time.DateTime),
			ips,
		)

	o := nats.GetDefaultOptions()
	o.Name = Hostname
	o.MaxReconnect = -1
	o.PingInterval = time.Second * 10
	o.Url = "nats:4222"
	nc, err := o.Connect()
	if err != nil {
		log.Fatal().Msg(err.Error())
	}

	s := res.NewService(Hostname)
	s.Handle("ui",
		res.Access(res.AccessGranted),
		res.GetModel(func(r res.ModelRequest) {
			r.Model(struct {
				Message string `json:"message"`
			}{"<h1>Hello from rusty_falcon_1</h1>"})
		}),
	)

	s.SetOnServe(func(s *res.Service) {
		ticker := time.NewTicker(time.Second * 3)
		for {
			_, err := nc.Request("call.hub.nodes.register", []byte(fmt.Sprintf(`{"params":{"id":"%s"}}`, Hostname)), time.Second*6)
			if err != nil {
				select {
				case <-ticker.C:
					continue
					//case shutdown
				}
			}

			ping_success := true
			for {
				_, err := nc.Request("call.hub.node."+Hostname+".ping", nil, time.Second*6)
				if err != nil {
					log.Warn().Msgf("call.hub.node.ping failed")
					ping_success = false
				}
				select {
				case <-ticker.C:
					//case shutdown
				}
				if !ping_success {
					break
				}
			}
		}
	})

	// data_watchdog := map[string]any{"message": "alive"}
	// s.Handle("watchdog",
	// 	res.Access(res.AccessGranted),
	// 	res.GetModel(func(r res.ModelRequest) {
	// 		r.Model(data_watchdog)
	// 	}),
	// )

	// go func() {
	// 	rid := fmt.Sprintf("%s.watchdog", Hostname)
	// 	ticker := time.NewTicker(time.Second * 3)
	// 	for {
	// 		s.With(rid, func(r res.Resource) {
	// 			data_watchdog["message"] = fmt.Sprintf("alive %v", time.Now().Unix())
	// 			r.ChangeEvent(data_watchdog)
	// 		})
	// 		<-ticker.C
	// 	}
	// }()

	err = s.Serve(nc)
	if err != nil {
		log.Fatal().Msgf("resgate serve err: %s", err.Error())
	}

}

type Node struct{}
