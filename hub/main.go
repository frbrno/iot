package main

import (
	"embed"
	"fmt"
	"io/fs"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/frbrno/iot/hub/web/handler"
	"github.com/frbrno/iot/lib/go/tempil"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/static"
	"github.com/jirenius/go-res"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var Hostname string

func main() {
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

	log.Info().Str("foo", "b12").
		Msgf("Hello world hostname: %s, time: %s, local-addrs: %v",
			Hostname,
			time.Now().Format(time.DateTime),
			ips,
		)

	app := fiber.New(fiber.Config{
		// Setting centralized error hanling.
		//ErrorHandler: handlers.CustomErrorHandler,
		StreamRequestBody: true,
	})

	app.Get("/assets*", static.New("", static.Config{
		FS:       static_dir_stripped(),
		Browse:   true,
		Compress: true,
	}))

	app.Use(logger.New())
	app.Use(func(c fiber.Ctx) error {
		tempil.LoadVars(c)
		return c.Next()
	})

	o := nats.GetDefaultOptions()
	o.Name = Hostname
	o.MaxReconnect = -1
	o.PingInterval = time.Second * 10
	o.Url = "nats:4222"
	nc, err := o.Connect()
	if err != nil {
		log.Fatal().Msg(err.Error())
	}
	defer nc.Close()

	// rpc_rusty_falcon, err := rpc.Dial(nc, o.Name, o.Name+"_mcu")
	// if err != nil {
	// 	log.Fatal().Msg(err.Error())
	// }
	// defer rpc_rusty_falcon.Close()

	//m_status := map[string]any{}

	mu := new(sync.RWMutex)
	s := res.NewService(Hostname)

	s.Handle("status",
		res.Access(res.AccessGranted),
		res.GetModel(func(r res.ModelRequest) {
			r.Model(struct {
				Message string `json:"message"`
			}{"<h1>Hello from rusty_falcon_1</h1>"})
		}),
	)
	nodes_online := []string{}
	nodes_last_ping := map[string]time.Time{}
	s.Handle("nodes", res.Access(res.AccessGranted),
		res.GetCollection(func(cr res.CollectionRequest) {
			mu.Lock()
			nodes_online_copy := make([]string, len(nodes_online))
			copy(nodes_online_copy, nodes_online)
			mu.Unlock()

			cr.Collection(nodes_online_copy)
		}),
		res.Call("register", func(cr res.CallRequest) {
			var p struct {
				ID string `json:"id"`
			}
			cr.ParseParams(&p)

			id := strings.TrimSpace(p.ID)

			if id == "" {
				cr.Error(fmt.Errorf("no id provided"))
				return
			}

			idx := -1
			mu.Lock()
			if !slices.Contains(nodes_online, id) {
				nodes_online = append(nodes_online, id)
				idx = len(nodes_online) - 1
			}
			mu.Unlock()

			if idx >= 0 {
				cr.AddEvent(id, idx)
			}

			cr.OK("success")
		}),
	)

	s.Handle("node.$id", res.Access(res.AccessGranted),
		res.Call("ping", func(cr res.CallRequest) {
			id := cr.PathParam("id")
			mu.Lock()
			idx := slices.Index(nodes_online, id)
			if idx < 0 {
				mu.Unlock()
				cr.Error(fmt.Errorf("u are considered offline, call register again"))
				return
			}
			nodes_last_ping[id] = time.Now()
			mu.Unlock()
			cr.OK("pong")
		}),
	)

	s.SetOnServe(func(s *res.Service) {
		go func() {
			ticker := time.NewTicker(time.Second * 6)
			for {
				select {
				case <-ticker.C:
					//case shutdown
				}
				mu.Lock()
				for id, last_ping := range nodes_last_ping {
					if time.Since(last_ping) > time.Second*6 {
						//consider node offline
						delete(nodes_last_ping, id)
						idx := slices.Index(nodes_online, id)
						if idx >= 0 {
							//TODO check if holding lock is ok here
							s.With(Hostname+".nodes", func(r res.Resource) {
								nodes_online = append(nodes_online[:idx], nodes_online[idx+1:]...)
								r.RemoveEvent(idx)
							})
						}
					}
				}
				mu.Unlock()
			}
		}()
	})

	go func() {
		err = s.Serve(nc)
		if err != nil {
			log.Fatal().Msgf("resgate serve err: %s", err.Error())
		}
	}()

	handler.Setup(app, nil)

	log.Fatal().Msgf("%v", app.Listen(":3000", fiber.ListenConfig{DisableStartupMessage: true}))
}

//go:embed web/assets
var static_dir embed.FS

func static_dir_stripped() fs.FS {
	static_dir_stripped, err := fs.Sub(static_dir, "web/assets")
	if err != nil {
		log.Fatal().Msg(err.Error())
	}
	return static_dir_stripped
}
