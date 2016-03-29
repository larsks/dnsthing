dnsthing listens to the docker events stream and maintains a hosts
file in response to containers starting and stopping.  The hosts file
can be consumed by `dnsmasq` for your very own dynamic docker dns
environment.

## Requirements

This probably requires Docker 1.10 or later.

## Synopsis

    usage: dnsthing [-h] [--verbose] [--debug] [--domain DOMAIN]
                    [--hostsfile HOSTSFILE] [--update-command UPDATE_COMMAND]

## Options

- `--verbose`
- `--debug`
- `--domain DOMAIN`, `-d DOMAIN`
- `--hostsfile HOSTSFILE`, `-H HOSTSFILE`
- `--update-command UPDATE_COMMAND`, `-c UPDATE_COMMAND`
