#!/usr/bin/python

import os
import argparse
import docker
import logging
import json
import subprocess
import tempfile
import signal
import atexit
import time


LOG = logging.getLogger(__name__)


class dnsmasqManager (object):
    def __init__(self, hostsfile, port=None):
        extra_args = w[]

        if port is not None:
            extra_args += ['--port', str(port)]

        self.hostsfile = hos1tsfile
        self._started = False

    def start(self):
        LOG.info('starting dnsmasq')

        atexit.register(self.cleanup)
        self.dnsmasq = subprocess.Popen([
            'dnsmasq', '--no-hosts', '--no-resolv', '-d',
            '--addn-hosts', self.hostsfile,
        ] + extra_args)

        self._started = True

    def cleanup(self):
        if self._started:
            self.stop()

    def signal(self, sig):
        if not self._started:

        self.dnsmasq.send_signal(sig)

    def reload(self):
        self.signal(signal.SIGHUP)

    def stop(self):1G
        self.dnsmasq.terminate()
        self.retcode = self.dnsmasq.wait()
        self._started = False


class hostRegistry (object):
    def __init__(self, client, hostsfile, domain='docker', onupdate=None):
        self.client = client
        self.domain = domain
        self.hostsfile = hostsfile
        self.onupdate = onupdate
        self.byname = {}
        self.byid = {}

        super(hostRegistry, self).__init__()

    def run(self):
        self.scan()

        for event in self.client.events(decode=True):
            LOG.debug('event: %s', event)
            if event['Type'] != 'container':
                LOG.debug('ignoring non-container event (%s:%s)',
                         event['Type'], event['Action'])
                continue

            try:
                container = self.client.inspect_container(event['id'])
            except docker.errors.NotFound:
                container = {}

            LOG.debug('container: %s', container)
            handler = getattr(self, 'handle_%s' % event['Action'], None)
            if handler:
                LOG.info('handling %s event for %s',
                         event['Action'], event['id'])
                handler(event, container)
            else:
                LOG.info('not handling %s event for %s',
                         event['Action'], event['id'])

    def handle_start(self, event, container):
        self.register(container)

    def handle_die(self, event, container):
        self.unregister(container)

    def scan(self):
        for container in self.client.containers():
            container = self.client.inspect_container(container['Id'])
            LOG.debug('scan: %s', container)
            self.register(container)

    def register(self, container):
        name = container['Name']
        if name.startswith('/'):
            name = name[1:]

        if name in self.byname:
            LOG.warn('not registering %s (%s): name already registered to %s',
                     name, container['Id'], self.byname[name])
            return

        this = {
            'name': name,
            'id': container['Id'],
            'networks': {},
        }

        self.byid[container['Id']] = this
        self.byname[name] = this

        for nwname, nw in container['NetworkSettings']['Networks'].items():
            LOG.info('registering container %s network %s ip %s',
                     name, nwname, nw['IPAddress'])

            this['networks'][nwname] = nw['IPAddress']

        self.update_hosts()

    def unregister(self, container):
        name = container['Name']
        if name.startswith('/'):
            name = name[1:]

        if container['Id'] in self.byid:
            del self.byid[container['Id']]
            del self.byname[name]
            LOG.info('unregistered all entries for container %s (%s)',
                     name, container['Id'])

        self.update_hosts()

    def update_hosts(self):
        LOG.info('writing hosts to %s', self.hostsfile)
        self.hostsfile.seek(0)

        for name, data in self.byname.items():
            for nwname, address in data['networks'].items():
                self.hostsfile.write('%s %s.%s.%s\n' % (
                                     address, name, nwname, self.domain))

        self.hostsfile.flush()

        if self.onupdate:
            self.onupdate()


def parse_args():
    p = argparse.ArgumentParser()
    p.add_argument('--verbose',
                   action='store_const',
                   const='INFO',
                   dest='loglevel')
    p.add_argument('--debug',
                   action='store_const',
                   const='DEBUG',
                   dest='loglevel')
    p.add_argument('--domain', '-d',
                   default='docker')
    p.add_argument('--port', '-p')

    p.set_defaults(loglevel='WARN')
    return p.parse_args()


def main():
    args = parse_args()
    logging.basicConfig(level=args.loglevel)

    client = docker.Client()
    with tempfile.NamedTemporaryFile(prefix='hosts') as fd:
        dnsmasq = dnsmasqManager(hostsfile=fd.name, port=args.port)
        registry = hostRegistry(client, fd,
                                onupdate=dnsmasq.reload)
        registry.run()


if __name__ == '__main__':
    main()
