gen:
	templ generate

setup_tools:
	go install github.com/air-verse/air@latest
	go install github.com/a-h/templ/cmd/templ@latest

setup_web:
# create tmp folder
	mkdir -p tmp
# create web folder
	mkdir -p web/assets/vendor/
# get bulma css
	curl -o web/assets/vendor/bulma.min.css https://cdn.jsdelivr.net/npm/bulma@1.0.2/css/bulma.min.css
# get htmx.js
	curl -o web/assets/vendor/htmx.min.js https://unpkg.com/htmx.org@2.0.3/dist/htmx.min.js
# get htmx sse plugin
	curl -o web/assets/vendor/htmx-ext-sse.js https://unpkg.com/htmx-ext-sse@2.2.2/sse.js
# get resclient
	curl -o web/assets/vendor/resclient.min.js https://unpkg.com/resclient@2.5.0/dist/resclient.min.js
# get fontawesome
	rm -rf tmp/fontawesome
	git clone -n --depth=1 --branch "6.6.0" --filter=tree:0 \
	https://github.com/FortAwesome/Font-Awesome.git tmp/fontawesome;
	cd tmp/fontawesome; \
	git sparse-checkout set --no-cone "css" "webfonts"; \
	git checkout;
	mkdir -p web/assets/vendor/fontawesome
	cp -r tmp/fontawesome/css web/assets/vendor/fontawesome/css
	cp -r tmp/fontawesome/webfonts web/assets/vendor/fontawesome/webfonts

install_update_board:
	go install github.com/frbrno/iot/hub/cmd/update_board

