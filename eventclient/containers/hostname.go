package containers

import "strings"

// buildHostname constructs a hostname based on container name, network, domain, and mode.
// This eliminates duplication that occurred in 6 different locations.
func buildHostname(containerName, networkName, domain string, includeNetwork bool) string {
	var parts []string
	parts = append(parts, containerName)

	if includeNetwork && networkName != "" {
		parts = append(parts, networkName)
	}

	if domain != "" {
		parts = append(parts, domain)
	}

	return strings.Join(parts, ".")
}

// BuildSingleNetworkHostname creates hostname for single-network mode.
// Format: containerName[.domain]
func BuildSingleNetworkHostname(containerName, domain string) string {
	return buildHostname(containerName, "", domain, false)
}

// BuildMultiNetworkHostname creates hostname for multi-network mode.
// Format: containerName.networkName[.domain]
func BuildMultiNetworkHostname(containerName, networkName, domain string) string {
	return buildHostname(containerName, networkName, domain, true)
}

// ConstructHostnames creates a map of hostnames to IP addresses based on container name,
// domain, network IPs, and multi-network mode.
func ConstructHostnames(containerName, domain string, ips map[string]string, multiNet bool) map[string]string {
	hostnames := make(map[string]string)

	if len(ips) == 0 {
		return hostnames
	}

	if multiNet {
		// Multi-network mode: create entry for each network
		// Format: containerName.networkName[.domain]
		for networkName, ip := range ips {
			hostname := BuildMultiNetworkHostname(containerName, networkName, domain)
			hostnames[hostname] = ip
		}
	} else {
		// Single network mode: use first available IP
		// Format: containerName[.domain]
		hostname := BuildSingleNetworkHostname(containerName, domain)
		// Get first IP from map
		for _, ip := range ips {
			hostnames[hostname] = ip
			break
		}
	}

	return hostnames
}
