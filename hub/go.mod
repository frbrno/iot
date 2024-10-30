module github.com/frbrno/iot/hub

go 1.23.2

replace github.com/frbrno/iot/lib/go/rpc => ../lib/go/rpc

replace github.com/frbrno/iot/lib/go/tempil => ../lib/go/tempil

require (
	github.com/a-h/templ v0.2.778
	github.com/frbrno/iot/lib/go/rpc v0.0.0-00010101000000-000000000000
	github.com/frbrno/iot/lib/go/tempil v0.0.0-00010101000000-000000000000
	github.com/gofiber/fiber/v3 v3.0.0-beta.3
	github.com/jirenius/go-res v0.5.1
	github.com/nats-io/nats.go v1.37.0
	github.com/rs/zerolog v1.33.0
)

require (
	github.com/andybalholm/brotli v1.1.0 // indirect
	github.com/gofiber/utils/v2 v2.0.0-beta.4 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jirenius/timerqueue v1.0.0 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/nats-io/nkeys v0.4.7 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasthttp v1.56.0 // indirect
	github.com/valyala/tcplisten v1.0.0 // indirect
	golang.org/x/crypto v0.27.0 // indirect
	golang.org/x/sys v0.25.0 // indirect
	golang.org/x/text v0.18.0 // indirect
)
