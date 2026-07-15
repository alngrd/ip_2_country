package database

import (
	"fmt"
	"net"
	"net/url"
)

type Location struct {
	Country string
	City    string
}

type Database interface {
	FindLocation(ip net.IP) (*Location, error)
	Close() error
}

type Factory struct{}

func (f *Factory) NewDatabase(dbURL string) (Database, error) {
	u, err := url.Parse(dbURL)
	if err != nil {
		return nil, fmt.Errorf("invalid database URL: %w", err)
	}
	switch u.Scheme {
	case "csv":
		// Opaque form ("csv:relative/path") puts the path in u.Opaque;
		// hierarchical form ("csv:///absolute/path") puts it in u.Path.
		path := u.Opaque
		if path == "" {
			path = u.Path
		}
		return NewCSVDatabase(path)
	default:
		return nil, &UnsupportedDatabaseError{Type: u.Scheme}
	}
}

type UnsupportedDatabaseError struct {
	Type string
}
func (e *UnsupportedDatabaseError) Error() string {
	return fmt.Sprintf("unsupported database type: %s", e.Type)
}

type NotFoundError struct {
	IP string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("IP address %s not found in database", e.IP)
}

