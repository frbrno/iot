# rusty_falcon

mcu to control some stuff

### wiki

### setup
- install rustup
- `cargo install espup espflash ldproxy`
- `espup install`
- install vscode extension 'rust analyzer'
- `cd $HOME/.espressif/esp-idf/v5.2.3/ git submodule update --init --recursive`

#### setup esp32
TODO  
use google  
I think I had to use master branch of idf because of bugs  

links:  
- https://github.com/esp-rs/awesome-esp-rust
- https://github.com/esp-rs/espup
- https://docs.espressif.com/projects/esp-idf/en/stable/esp32/get-started/linux-macos-setup.html
- https://github.com/johnthagen/min-sized-rust

#### partitions
- need create a partitions.bin from csv file:  
`cargo-espflash espflash partition-table partitions.csv --to-binary >> partitions.bin`
- the key 'partition_table=' in espflash.toml is used by 'cargo run'.

#### ota (over-the-air) update related 

links:
- https://quan.hoabinh.vn/post/2024/3/programming-esp32-with-rust-ota-firmware-update  
- https://docs.rs/crate/esp-ota/latest

more:
- build release `cargo build --release`  
- create .bin for ota update process 
`espflash save-image --chip esp32c3 -s 4mb target/riscv32imc-esp-espidf/release/rusty_falcon /tmp/rusty_falcon_firmware.bin`
- run update process with code/goofy_hawk/cmd/update_board/main.go

#### memo
streams:  
```go
	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name: "rusty_falcon_run",
		Subjects: []string{
			"iot.rusty_falcon.rx.*.run.*.*.*",
			"iot.rusty_falcon.tx.*.run.*.*.*",
		},
	})

	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name: "rusty_falcon_set",
		Subjects: []string{
			"iot.rusty_falcon.rx.*.set.*.*.*",
			"iot.rusty_falcon.tx.*.set.*.*.*",
		},
	})

	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name: "rusty_falcon_get",
		Subjects: []string{
			"iot.rusty_falcon.rx.*.get.*.*.*",
			"iot.rusty_falcon.tx.*.get.*.*.*",
		},
	})
```
