dnsthing listens to the docker events stream and maintains a hosts
file in response to containers starting and stopping.

The intended purpose of this is to provide a file suitable for use with the
[dnsmasq] `--hostsdir` argument (so that `dnsmasq` will update automatically
when the file is modified).

## Requirements

- Go 1.24.0 or later

## Building

```
go build
```

## Usage

```sh
Usage of ./dnsthing:
  -d, --domain string                      domain to append to container names
  -i, --minimum-update-interval duration   minimum time between hostfile updates
  -m, --multiple-networks                  create entries for all networks
  -r, --replace                            replace hosts file (ignore existing entries)
  -u, --update-command string              command to run after hostfile updates
```

## Examples

### Simple networking

If I run:

```sh
./dnsthing --replace --domain example.org data/hosts.txt
```

And then run:

```sh
for x in 1 2 3; do docker run -d --rm --name container$x docker.io/traefik/whoami; done
```

I will see something like the following in `data/hosts.txt`:

```
172.17.0.2	container1.example.org
172.17.0.3	container2.example.org
172.17.0.4	container3.example.org
```

### Multiple networks

If I run:

```sh
./dnsthing --replace --domain example.org --multiple-networks data/hosts.txt
```

And then deploy the following `compose.yaml` configuration:

```yaml
networks:
  network1:
    name: network1
  network2:
    name: network2

services:
  container1:
    container_name: container1
    image: docker.io/traefik/whoami:latest
    networks:
      - network1
  container2:
    container_name: container2
    image: docker.io/traefik/whoami:latest
    networks:
      - network1
  container3:
    container_name: container3
    image: docker.io/traefik/whoami:latest
    networks:
      - network2
```

I would find the following content in `data/hosts.txt`:

```
172.18.0.3	container1.network1.example.org
172.18.0.2	container2.network1.example.org
172.19.0.2	container3.network2.example.org
```
