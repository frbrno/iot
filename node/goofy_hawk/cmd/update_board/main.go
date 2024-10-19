package main

import (
	"flag"
	"log"
	"time"

	"github.com/frbrno/iot/node/goofy_hawk/lib/everpc"
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

	conn, err := everpc.Connect(nats_url, "update_board")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	peer := everpc.NewPeer(conn, peer_name)
	defer peer.Close()

	peer.IsOnOfflineSigBlocking(true, time.Second*6)
	if err != nil {
		log.Fatal("timeout waiting for connect")
	}

	is_offline_sig := peer.IsOnOfflineSig(false)

	t_begin := time.Now()
	err = peer.Request().
		SetPayload([]byte(`{"url":"http://192.168.10.130:3242/rusty_falcon_firmware.bin"}`)).
		SetDoneTimeout(time.Minute).
		Run("update_board")

	if err != nil {
		log.Fatal(err)
	}

	log.Printf("upload success! board reboots soon. elapsed: %s", time.Since(t_begin).String())
	log.Printf("waiting for board info ...")
	select {
	case <-time.After(time.Second * 20):
		peer.IsOnOfflineSigUnsubscribe(is_offline_sig)
		log.Fatal("timeout waiting for disconnect")
	case <-is_offline_sig:
	}

	peer.IsOnOfflineSigBlocking(true, time.Second*30)
	if err != nil {
		log.Fatal("timeout waiting for connect")
	}
	ack_result := struct {
		BuildTime string `json:"build_time"`
	}{}
	_, err = peer.Request().SetAckResult(&ack_result).Get("info")
	if err != nil {
		log.Fatal("timeout waiting for info")
	}
	log.Println("board info:")
	log.Printf("BuildTime: %v", ack_result.BuildTime)
}
