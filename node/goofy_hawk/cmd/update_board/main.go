package main

import (
	"flag"
	"log"
	"time"

	"github.com/frbrno/iot/node/goofy_hawk/lib/prot"
	"github.com/gofiber/fiber/v3"
)

func main() {
	var path_binary string
	var nats_url string
	var peer_name string
	flag.StringVar(&path_binary, "path_binary", "/tmp/rusty_falcon_firmware.bin", "file for update board ota")
	flag.StringVar(&nats_url, "nats_url", "192.168.10.124:4222", "nats url")
	flag.StringVar(&peer_name, "peer_name", "rusty_falcon", "peer name")

	flag.Parse()

	app := fiber.New()

	app.Get("/rusty_falcon_firmware.bin", func(c fiber.Ctx) error {
		// Serve the file using SendFile function
		return c.SendFile(path_binary, fiber.SendFile{
			Compress:  false,
			ByteRange: true,
			Download:  true,
		})
	})

	app.Get("/", func(c fiber.Ctx) error {
		return c.SendString("Hello, World!")
	})

	go func() {
		err := app.Listen(":3242", fiber.ListenConfig{DisableStartupMessage: true})
		if err != nil {
			log.Fatal(err)
		}
	}()

	conn, err := prot.Connect(nats_url, "update_board")
	if err != nil {
		log.Fatal(err)
	}
	//defer conn.Close()

	peer, err := prot.NewPeer(conn, peer_name)
	if err != nil {
		log.Fatal(err)
	}
	defer peer.Close()

	t_begin := time.Now()
	err = peer.Request().
		SetPayload([]byte(`{"url":"http://192.168.10.130:3242/rusty_falcon_firmware.bin"}`)).
		SetDoneTimeout(time.Minute).
		Run("update_board")

	if err != nil {
		log.Fatal(err)
	}

	log.Printf("success! elapsed: %s", time.Since(t_begin).String())
}
