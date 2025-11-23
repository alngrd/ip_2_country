package database

import (
	"encoding/csv"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
)

// Format: ip,city,country
// Supports both single IPs and CIDR ranges like 192.168.1.0/24
type CSVDatabase struct {
	file        *os.File
	reader      *csv.Reader
	exactIPs    map[string]*Location
	cidrByNet   map[string][]cidrEntry
}

type cidrEntry struct {
	network *net.IPNet
	location *Location
}

func NewCSVDatabase(filePath string) (*CSVDatabase, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database file: %w", err)
	}

	db := &CSVDatabase{
		file:      file,
		reader:    csv.NewReader(file),
		exactIPs:  make(map[string]*Location, 1000),
		cidrByNet: make(map[string][]cidrEntry, 1000),
	}

	if err := db.loadEntries(); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to load database entries: %w", err)
	}

	return db, nil
}

func (db *CSVDatabase) loadEntries() error {
	records, err := db.reader.ReadAll()
	if err != nil {
		return err
	}

	for _, record := range records {
		if len(record) < 3 {
			continue
		}

		ipStr := strings.TrimSpace(record[0])
		city := strings.TrimSpace(record[1])
		country := strings.TrimSpace(record[2])

		location := &Location{
			Country: country,
			City:    city,
		}

		// try CIDR first
		_, network, err := net.ParseCIDR(ipStr)
		if err == nil {
			netKey := network.IP.String()
			db.cidrByNet[netKey] = append(db.cidrByNet[netKey], cidrEntry{
				network:  network,
				location: location,
			})
			continue
		}

		// then try single IP
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}

		// Normalize IP to string for map key (handles IPv4-mapped IPv6 addresses)
		ipKey := ip.String()
		db.exactIPs[ipKey] = location
	}

	if len(db.exactIPs) == 0 && len(db.cidrByNet) == 0 {
		return fmt.Errorf("no valid entries found in database file")
	}

	// Sort CIDR ranges within each network by prefix length (longest first)
	for netKey := range db.cidrByNet {
		sort.Slice(db.cidrByNet[netKey], func(i, j int) bool {
			iPrefix, _ := db.cidrByNet[netKey][i].network.Mask.Size()
			jPrefix, _ := db.cidrByNet[netKey][j].network.Mask.Size()
			return iPrefix > jPrefix
		})
	}

	return nil
}

func (db *CSVDatabase) findInCIDR(ip net.IP, ipBytes net.IP, prefixLengths []int) (*Location, error) {
	for _, prefixLen := range prefixLengths {
		mask := net.CIDRMask(prefixLen, len(ipBytes)*8)
		if mask == nil {
			continue
		}

		// Apply mask to get the network address
		networkAddr := make(net.IP, len(ipBytes))
		copy(networkAddr, ipBytes)
		for i := range networkAddr {
			networkAddr[i] &= mask[i]
		}

		// Check if we have any CIDR ranges for this network address
		netKey := networkAddr.String()
		if entries, exists := db.cidrByNet[netKey]; exists {
			// Check all entries for this network (already sorted by prefix length)
			for _, entry := range entries {
				if entry.network.Contains(ip) {
					return entry.location, nil
				}
			}
		}
	}

	return nil, &NotFoundError{IP: ip.String()}
}

func (db *CSVDatabase) FindLocation(ip net.IP) (*Location, error) {
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address")
	}

	ipKey := ip.String()
	if location, exists := db.exactIPs[ipKey]; exists {
		return location, nil
	}


	ip4 := ip.To4()
	if ip4 != nil {
		// IPv4: check all prefix lengths from /32 down to /0
		prefixLengths := make([]int, 33)
		for i := 0; i <= 32; i++ {
			prefixLengths[i] = 32 - i
		}
		return db.findInCIDR(ip, ip4, prefixLengths)
	} else {
		// IPv6: check all prefix lengths from /128 down to /0
		prefixLengths := make([]int, 129)
		for i := 0; i <= 128; i++ {
			prefixLengths[i] = 128 - i
		}
		return db.findInCIDR(ip, ip, prefixLengths)
	}
}

func (db *CSVDatabase) Close() error {
	if db.file != nil {
		return db.file.Close()
	}
	return nil
}

