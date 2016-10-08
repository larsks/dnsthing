dnsthing listens to the docker events stream and maintains a hosts
file in response to containers starting and stopping.  The hosts file
can be consumed by `dnsmasq` for your very own dynamic docker dns
environment.

## Requirements

This probably requires Docker 1.10 or later.

### Windows User Notes
If you want to use this, please run:
```
route add 172.17.0.0 mask 255.255.0.0 10.0.75.2 -p
```
in a privileged powershell / command prompt (right click, and choose run as administrator)

note: 172.17.0.0 is the default subnet for docker bridge, and the 10.0.75.2 is the default ip of linux vm that hosts the container, change these to your need.

## Synopsis

    usage: dnsthing [-h] [--verbose] [--debug] [--domain DOMAIN]
                    [--hostsfile HOSTSFILE] [--update-command UPDATE_COMMAND]

## Options

- `--verbose`
- `--debug`
- `--domain DOMAIN`, `-d DOMAIN`
- `--hostsfile HOSTSFILE`, `-H HOSTSFILE`
- `--update-command UPDATE_COMMAND`, `-c UPDATE_COMMAND`
