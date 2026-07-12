package database

import (
	"strings"
	"testing"
)

func TestNotFoundError_Message(t *testing.T) {
	err := &NotFoundError{IP: "1.2.3.4"}
	if !strings.Contains(err.Error(), "1.2.3.4") {
		t.Errorf("expected IP in error message, got: %s", err.Error())
	}
}

func TestUnsupportedDatabaseError_Message(t *testing.T) {
	err := &UnsupportedDatabaseError{Type: "mongo"}
	if !strings.Contains(err.Error(), "mongo") {
		t.Errorf("expected type in error message, got: %s", err.Error())
	}
}

func TestFactory_UnsupportedType(t *testing.T) {
	f := &Factory{}
	_, err := f.NewDatabase("redis", "/some/path")
	if err == nil {
		t.Fatal("expected error for unsupported db type")
	}
	if _, ok := err.(*UnsupportedDatabaseError); !ok {
		t.Errorf("expected *UnsupportedDatabaseError, got %T", err)
	}
}
