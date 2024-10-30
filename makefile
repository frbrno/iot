NODE = rusty_falcon
NODE_INDEX = 1
	
livereload_hub:
	cd hub; go run github.com/bokwoon95/wgo@v0.5.6d \
		-file=.go -file=.templ -xfile=_templ.go \
		templ generate :: \
		podman rm hub_livereload --force :: \
		podman-compose -f ../compose.yaml run --rm --service-ports --no-deps --build --name=hub_livereload hub

livereload_node:
	cd node; go run github.com/bokwoon95/wgo@v0.5.6d \
		-file=.go -file=.templ -xfile=_templ.go -xdir=.*/firmware \
		templ generate :: \
		podman rm $(NODE)_$(NODE_INDEX)_livereload --force :: \
		podman-compose -f ../compose.yaml run --rm --service-ports --no-deps --build --name=$(NODE)_$(NODE_INDEX)_livereload $(NODE)_$(NODE_INDEX)