package database

import (
	"encoding/csv"
	"net"
	"os"
	"strings"
	"testing"
)

func newTestDB(csvContent string) (*CSVDatabase, error) {
	db := &CSVDatabase{
		exactIPs:  make(map[string]*Location),
		cidrByNet: make(map[string][]cidrEntry),
	}
	reader := csv.NewReader(strings.NewReader(csvContent))
	return db, db.loadEntries(reader)
}

func TestFindLocation_ExactIPHit(t *testing.T) {
	db, err := newTestDB("1.2.3.4,New York,US\n")
	if err != nil {
		t.Fatal(err)
	}
	loc, err := db.FindLocation(net.ParseIP("1.2.3.4"))
	if err != nil {
		t.Fatalf("expected hit, got error: %v", err)
	}
	if loc.Country != "US" || loc.City != "New York" {
		t.Errorf("unexpected location: %+v", loc)
	}
}

func TestFindLocation_CIDRMatch(t *testing.T) {
	db, err := newTestDB("192.168.1.0/24,London,GB\n")
	if err != nil {
		t.Fatal(err)
	}
	loc, err := db.FindLocation(net.ParseIP("192.168.1.50"))
	if err != nil {
		t.Fatalf("expected CIDR hit, got error: %v", err)
	}
	if loc.Country != "GB" {
		t.Errorf("expected GB, got %s", loc.Country)
	}
}

func TestFindLocation_CIDRBoundaryIPs(t *testing.T) {
	db, err := newTestDB("10.0.0.0/8,Berlin,DE\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, ip := range []string{"10.0.0.0", "10.255.255.255", "10.128.0.1"} {
		loc, err := db.FindLocation(net.ParseIP(ip))
		if err != nil {
			t.Errorf("expected hit for %s, got error: %v", ip, err)
			continue
		}
		if loc.Country != "DE" {
			t.Errorf("expected DE for %s, got %s", ip, loc.Country)
		}
	}
	// Outside the range
	if _, err := db.FindLocation(net.ParseIP("11.0.0.0")); err == nil {
		t.Error("expected NotFoundError for 11.0.0.0")
	}
}

func TestFindLocation_LongestPrefixWins(t *testing.T) {
	db, err := newTestDB("192.168.0.0/16,General,GEN\n192.168.1.0/24,Specific,SPEC\n")
	if err != nil {
		t.Fatal(err)
	}
	loc, err := db.FindLocation(net.ParseIP("192.168.1.5"))
	if err != nil {
		t.Fatal(err)
	}
	if loc.Country != "SPEC" {
		t.Errorf("expected longest prefix match SPEC, got %s", loc.Country)
	}
	// IP in the /16 but not /24 should get the wider range
	loc, err = db.FindLocation(net.ParseIP("192.168.2.5"))
	if err != nil {
		t.Fatal(err)
	}
	if loc.Country != "GEN" {
		t.Errorf("expected GEN for IP outside /24, got %s", loc.Country)
	}
}

func TestFindLocation_NotFound(t *testing.T) {
	db, err := newTestDB("1.2.3.4,City,CC\n")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.FindLocation(net.ParseIP("9.9.9.9"))
	if err == nil {
		t.Fatal("expected NotFoundError")
	}
	if _, ok := err.(*NotFoundError); !ok {
		t.Errorf("expected *NotFoundError, got %T: %v", err, err)
	}
}

func TestFindLocation_NilIP(t *testing.T) {
	db, err := newTestDB("1.2.3.4,City,CC\n")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.FindLocation(nil)
	if err == nil {
		t.Fatal("expected error for nil IP")
	}
}

func TestFindLocation_IPv6Exact(t *testing.T) {
	db, err := newTestDB("2001:db8::1,Tokyo,JP\n")
	if err != nil {
		t.Fatal(err)
	}
	loc, err := db.FindLocation(net.ParseIP("2001:db8::1"))
	if err != nil {
		t.Fatalf("expected IPv6 hit, got error: %v", err)
	}
	if loc.Country != "JP" {
		t.Errorf("expected JP, got %s", loc.Country)
	}
}

func TestFindLocation_IPv6CIDR(t *testing.T) {
	db, err := newTestDB("2001:db8::/32,Toronto,CA\n")
	if err != nil {
		t.Fatal(err)
	}
	loc, err := db.FindLocation(net.ParseIP("2001:db8::cafe"))
	if err != nil {
		t.Fatalf("expected IPv6 CIDR hit, got error: %v", err)
	}
	if loc.Country != "CA" {
		t.Errorf("expected CA, got %s", loc.Country)
	}
}

func TestLoadEntries_HeaderSkipped(t *testing.T) {
	// "ip" is not a valid IP or CIDR, so it should be treated as a header
	db, err := newTestDB("ip,city,country\n1.2.3.4,NYC,US\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(db.exactIPs) != 1 {
		t.Errorf("expected 1 entry after skipping header, got %d", len(db.exactIPs))
	}
}

func TestLoadEntries_ShortRowSkipped(t *testing.T) {
	// All rows have 2 fields (consistent count) — all are skipped, no valid entries
	_, err := newTestDB("1.2.3.4,OnlyTwoFields\n5.6.7.8,AlsoTwoFields\n")
	if err == nil {
		t.Fatal("expected error when all rows have fewer than 3 fields")
	}
}

func TestLoadEntries_InvalidIPSkipped(t *testing.T) {
	db, err := newTestDB("not-an-ip,City,CC\n1.2.3.4,City,CC\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(db.exactIPs) != 1 {
		t.Errorf("expected 1 valid entry, got %d", len(db.exactIPs))
	}
}

func TestLoadEntries_EmptyFile(t *testing.T) {
	_, err := newTestDB("")
	if err == nil {
		t.Fatal("expected error for empty file")
	}
}

func TestLoadEntries_AllInvalidEntries(t *testing.T) {
	_, err := newTestDB("bad,row\nanother,bad,row\n")
	if err == nil {
		t.Fatal("expected error when no valid entries exist")
	}
}

func TestNewCSVDatabase_FileNotFound(t *testing.T) {
	_, err := NewCSVDatabase("/nonexistent/path/db.csv")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestNewCSVDatabase_ValidFile(t *testing.T) {
	f, err := os.CreateTemp("", "ip2country_test_*.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString("1.2.3.4,Paris,FR\n")
	f.Close()

	db, err := NewCSVDatabase(f.Name())
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	defer db.Close()

	loc, err := db.FindLocation(net.ParseIP("1.2.3.4"))
	if err != nil {
		t.Fatal(err)
	}
	if loc.Country != "FR" {
		t.Errorf("expected FR, got %s", loc.Country)
	}
}

func TestCSVDatabase_Close(t *testing.T) {
	db, err := newTestDB("1.2.3.4,City,CC\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Errorf("Close() should not error, got: %v", err)
	}
}

func TestFindLocation_ExactIPTakesPrecedenceOverCIDR(t *testing.T) {
	// Both an exact match and a CIDR covering it exist — exact wins
	db, err := newTestDB("192.168.1.5,ExactCity,EXACT\n192.168.1.0/24,CIDRCity,CIDR\n")
	if err != nil {
		t.Fatal(err)
	}
	loc, err := db.FindLocation(net.ParseIP("192.168.1.5"))
	if err != nil {
		t.Fatal(err)
	}
	if loc.Country != "EXACT" {
		t.Errorf("expected exact match to win, got %s", loc.Country)
	}
}

func TestFindLocation_WhitespaceInFields(t *testing.T) {
	db, err := newTestDB("  1.2.3.4  ,  San Francisco  ,  US  \n")
	if err != nil {
		t.Fatal(err)
	}
	loc, err := db.FindLocation(net.ParseIP("1.2.3.4"))
	if err != nil {
		t.Fatal(err)
	}
	if loc.City != "San Francisco" || loc.Country != "US" {
		t.Errorf("expected trimmed fields, got city=%q country=%q", loc.City, loc.Country)
	}
}

func TestFindLocation_IPv4MappedIPv6(t *testing.T) {
	// net.ParseIP normalizes both "1.2.3.4" and "::ffff:1.2.3.4" to the same
	// 16-byte IPv4-in-IPv6 representation, so a lookup via the mapped form must
	// resolve to the same entry as the plain IPv4 form.
	db, err := newTestDB("1.2.3.4,New York,US\n")
	if err != nil {
		t.Fatal(err)
	}
	loc, err := db.FindLocation(net.ParseIP("::ffff:1.2.3.4"))
	if err != nil {
		t.Fatalf("IPv4-mapped IPv6 lookup failed: %v", err)
	}
	if loc.Country != "US" {
		t.Errorf("expected US, got %s", loc.Country)
	}
}

func TestLoadEntries_DuplicateIPLastWins(t *testing.T) {
	// When the same IP appears twice, the second entry overwrites the first.
	db, err := newTestDB("1.2.3.4,First,AA\n1.2.3.4,Second,BB\n")
	if err != nil {
		t.Fatal(err)
	}
	loc, err := db.FindLocation(net.ParseIP("1.2.3.4"))
	if err != nil {
		t.Fatal(err)
	}
	if loc.Country != "BB" {
		t.Errorf("expected last entry (BB) to win, got %s", loc.Country)
	}
}
