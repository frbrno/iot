package main

import (
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/nats-io/nats.go"
)

func main() {
	var path_binary string
	flag.StringVar(&path_binary, "path_binary", "/tmp/rusty_falcon_firmware.bin", "file for update board ota")
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

	nc, err := nats.Connect("192.168.10.124:4222")
	if err != nil {
		log.Fatal(err)
	}
	defer nc.Close()

	time.Sleep(time.Second * 2)
	token := time.Now().Unix()

	sig_msg := make(chan *nats.Msg, 3)
	sub, err := nc.ChanSubscribe(fmt.Sprintf("rusty_falcon.goofy_hawk.update_board.*.%v", token), sig_msg)
	if err != nil {
		log.Fatal(err)
	}
	defer sub.Unsubscribe()

	err = nc.Publish(fmt.Sprintf("goofy_hawk.rusty_falcon.update_board.run.%v", token), []byte(`{"url":"http://192.168.10.130:3242/rusty_falcon_firmware.bin"}`))
	if err != nil {
		log.Fatal(err)
	}
	select {
	case <-time.After(time.Second * 6):
		log.Fatal("timeout ack")
	case msg := <-sig_msg:
		spl := strings.Split(msg.Subject, ".")
		if spl[3] != "ack" {
			log.Fatalf("protocol err? subject: %s", msg.Subject)
		}
	}
	log.Printf("rx board ack, update process starded")

	select {
	case <-time.After(time.Minute):
		log.Fatal("timeout waiting for done")
	case msg := <-sig_msg:
		spl := strings.Split(msg.Subject, ".")
		if spl[3] != "done" {
			log.Fatalf("process got canceled or error subject: %s", msg.Subject)
		}
		log.Printf("success, the board restarts itself")
	}
}
