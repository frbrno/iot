include .env

release:
	cargo build --release
	espflash save-image --chip esp32c3 -s 4mb target/riscv32imc-esp-espidf/release/rusty_falcon /tmp/rusty_falcon_firmware.bin

upload: release
	update_board #node/goofy_hawk/cmd/update_board

run:
	cargo run

print_vars:
	@echo $(WIFI_SSID)
