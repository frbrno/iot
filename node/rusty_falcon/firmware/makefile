include .env

release:
	cargo build --release
	espflash save-image --chip esp32c3 -s 4mb target/riscv32imc-esp-espidf/release/rusty_falcon /tmp/rusty_falcon_firmware.bin

upload: release
	update_board -nats_url=192.168.10.130:4222 -node_name=rusty_falcon_1_mcu

run:
	cargo run

print_vars:
	@echo $(WIFI_SSID)
