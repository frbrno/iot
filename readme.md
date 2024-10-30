# iot playground

#### dev

- start nats and resgate  
`podman-compose up --build --remove-orphans --force-recreate nats resgate && podman-compose down --volumes --remove-orphans`
- start/restart hub  
`podman-compose run --rm --service-ports --no-deps --build hub`  
or with wgo for live reload  
`make livereload_hub`
- start/restart node  
`podman-compose run --rm --service-ports --no-deps --build rusty_falcon_1`  
or with wgo for live reload  
`make livereload_node`
