package database

import (
	"fmt"
	"net"
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

func (f *Factory) NewDatabase(dbType, dbPath string) (Database, error) {
	switch dbType {
	case "csv":
		return NewCSVDatabase(dbPath)
	default:
		return nil, &UnsupportedDatabaseError{Type: dbType}
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

