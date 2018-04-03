#!/usr/bin/python

import argparse
import docker
import logging
import subprocess


LOG = logging.getLogger(__name__)


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
        # Register any existing containers first
        self.scan()

        # Watch for docker events and register/unregister
        # addresses as containers are started and stopped.
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
                LOG.debug('not handling %s event for %s',
                          event['Action'], event['id'])

    def handle_start(self, event, container):
        self.register(container)

    def handle_die(self, event, container):
        self.unregister(container)

    def scan(self):
        '''Register any existing containers'''

        for container in self.client.containers():
            container = self.client.inspect_container(container['Id'])
            LOG.debug('scan: %s', container)
            self.register(container)

    def register(self, container):
        '''Register a container.  Iterate over all of the networks to
        which this container is attached, and for each network add the
        name <container_name>.<network_name>.<domnain>.'''

        name = container['Name']
        if name.startswith('/'):
            name = name[1:]

        if name in self.byname:
            LOG.warn('not registering %s (%s): name already registered to %s',
                     name, container['Id'], self.byname[name])
            return

        if container['NetworkSettings'].get('Networks') is None:
            LOG.warn('container %s (%s) has no network information',
                     name, container['Id'])
            return

        this = {
            'name': name,
            'id': container['Id'],
            'networks': {},
            'hostname': container['Config']['Hostname']
        }

        self.byid[container['Id']] = this
        self.byname[name] = this

        for nwname, nw in container['NetworkSettings']['Networks'].items():
            LOG.info('registering container %s network %s ip %s',
                     name, nwname, nw['IPAddress'])

            this['networks'][nwname] = nw['IPAddress']

        self.update_hosts()

    def unregister(self, container):
        '''Remove all entries associated with a given container.'''

        name = container['Name']
        if name.startswith('/'):
            name = name[1:]

        hostname = container['Config']['Hostname']

        if container['Id'] in self.byid:
            del self.byid[container['Id']]
            del self.byname[name]
            del self.byname[hostname]
            LOG.info('unregistered all entries for container %s (%s)',
                     name, container['Id'])

        self.update_hosts()

    def update_hosts(self):
        '''Write out the hosts file and (optionally) trigger the
        onupdate callback.'''

        LOG.info('writing hosts to %s', self.hostsfile)

        with open(self.hostsfile, 'w') as fd:
            for name, data in self.byname.items():
                byhost = False
                for nwname, address in data['networks'].items():
                    if byhost == False:
                        fd.write('%s %s.%s\n' % (address, data['hostname'], self.domain))
                        byhost = True
                    fd.write('%s %s.%s.%s\n' % (
                        address, name, nwname, self.domain))

        if self.onupdate:
            self.onupdate()


def parse_args():
    p = argparse.ArgumentParser()
    p.add_argument('--verbose', '-v',
                   action='store_const',
                   const='INFO',
                   dest='loglevel')
    p.add_argument('--debug',
                   action='store_const',
                   const='DEBUG',
                   dest='loglevel')
    p.add_argument('--domain', '-d',
                   default='docker')
    p.add_argument('--hostsfile', '-H',
                   default='./hosts')
    p.add_argument('--update-command', '-c')

    p.set_defaults(loglevel='WARN')
    return p.parse_args()


def run_external_command(cmd):
    def runner():
        LOG.info('running external command: %s', cmd)
        subprocess.call(cmd, shell=True)

    return runner


def main():
    args = parse_args()
    logging.basicConfig(level=args.loglevel)
    registry_args = {}

    if args.update_command:
        run_update_command = run_external_command(args.update_command)
        registry_args['onupdate'] = run_update_command

    try:
        client = docker.client.AutoVersionClient()
    except AttributeError:
        client = docker.client.APIClient()

    registry = hostRegistry(client,
                            args.hostsfile,
                            **registry_args)

    registry.run()


if __name__ == '__main__':
    main()
