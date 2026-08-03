package httpapi

import (
	"encoding/xml"
	"errors"
	"io"
	"net"
	"strings"
)

const (
	// maxIngestionBytes and maxIngestionHosts bound one tool artifact. These are
	// inventory limits rather than evidence limits, and the supported
	// resource-limit matrix is still an open product decision, so the artifact is
	// refused whole before anything is recorded.
	maxIngestionBytes = 8 << 20
	maxIngestionHosts = 4096
	// maxAssetNameBytes bounds one derived asset name. A scan reports names the
	// scanned network controls, so the name an operator later reads is bounded
	// and restricted here rather than wherever it is displayed.
	maxAssetNameBytes = 253
)

var errInvalidNmapReport = errors.New("artifact is not an accepted Nmap XML report")

// nmapReport is the closed profile of `nmaprun` FrameOPS reads: which hosts a
// scan saw, how they were addressed, and what the scanner called itself. Ports,
// scripts, timing and every other element are ignored, because this ingestion
// only builds the engagement asset inventory.
type nmapReport struct {
	XMLName          xml.Name   `xml:"nmaprun"`
	Scanner          string     `xml:"scanner,attr"`
	Version          string     `xml:"version,attr"`
	XMLOutputVersion string     `xml:"xmloutputversion,attr"`
	Hosts            []nmapHost `xml:"host"`
}

type nmapHost struct {
	Status struct {
		State string `xml:"state,attr"`
	} `xml:"status"`
	Addresses []struct {
		Address string `xml:"addr,attr"`
		Type    string `xml:"addrtype,attr"`
	} `xml:"address"`
	Hostnames []struct {
		Name string `xml:"name,attr"`
	} `xml:"hostnames>hostname"`
}

// nmapImport is what one accepted artifact contributes: the distinct asset names
// it identified, in the order the scan reported them, and the count of host
// entries the import could not turn into an asset. `Read` always equals the
// distinct names plus `Ignored` plus `Rejected`, which is the arithmetic the
// database enforces on the recorded summary.
type nmapImport struct {
	FormatVersion string
	Names         []string
	Read          int
	Ignored       int
	Rejected      int
}

// readNmapReport parses one artifact into the assets it identifies. A host that
// the scan did not see as up, or that repeats an identity already read from the
// same artifact, is ignored; a host reported up with no usable address or
// hostname is rejected. Neither aborts the import, because a partially useful
// scan is still an inventory. A structurally unacceptable artifact is refused
// whole so no half-read scan is ever recorded as an ingestion.
func readNmapReport(reader io.Reader) (nmapImport, error) {
	var report nmapReport
	decoder := xml.NewDecoder(io.LimitReader(reader, maxIngestionBytes+1))
	decoder.Strict = true
	if err := decoder.Decode(&report); err != nil {
		return nmapImport{}, errInvalidNmapReport
	}
	if !strings.EqualFold(strings.TrimSpace(report.Scanner), "nmap") {
		return nmapImport{}, errInvalidNmapReport
	}
	if len(report.Hosts) > maxIngestionHosts {
		return nmapImport{}, errInvalidNmapReport
	}

	result := nmapImport{
		FormatVersion: nmapFormatVersion(report),
		Read:          len(report.Hosts),
	}
	seen := make(map[string]bool, len(report.Hosts))
	for _, host := range report.Hosts {
		if !strings.EqualFold(strings.TrimSpace(host.Status.State), "up") {
			result.Ignored++
			continue
		}
		name, ok := hostAssetName(host)
		if !ok {
			result.Rejected++
			continue
		}
		if seen[name] {
			result.Ignored++
			continue
		}
		seen[name] = true
		result.Names = append(result.Names, name)
	}
	return result, nil
}

// hostAssetName derives the single name one host is known by. A reverse-DNS
// hostname is the most useful identifier to an operator, so it is preferred, but
// it is controlled by the scanned network: a hostname that is not a plain
// domain name is discarded in favour of the address rather than trusted.
func hostAssetName(host nmapHost) (string, bool) {
	for _, hostname := range host.Hostnames {
		name := strings.ToLower(strings.TrimSpace(hostname.Name))
		if domainName(name) {
			return name, true
		}
	}
	for _, addressType := range []string{"ipv4", "ipv6"} {
		for _, address := range host.Addresses {
			if !strings.EqualFold(strings.TrimSpace(address.Type), addressType) {
				continue
			}
			parsed := net.ParseIP(strings.TrimSpace(address.Address))
			if parsed == nil {
				continue
			}
			return parsed.String(), true
		}
	}
	return "", false
}

// domainName accepts the conservative host-name shape an asset may be named
// after: dot-separated labels of letters, digits and hyphens.
func domainName(name string) bool {
	if name == "" || len(name) > maxAssetNameBytes || strings.HasPrefix(name, "-") {
		return false
	}
	labels := strings.Split(strings.TrimSuffix(name, "."), ".")
	for _, label := range labels {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, character := range label {
			letter := character >= 'a' && character <= 'z'
			digit := character >= '0' && character <= '9'
			if !letter && !digit && character != '-' {
				return false
			}
		}
	}
	return true
}

// nmapFormatVersion records which producer and output contract the artifact
// declared, so a later change of Nmap output can be told apart in the history
// instead of being reconstructed. Attribute values the scan controls are only
// recorded when they are plain version tokens.
func nmapFormatVersion(report nmapReport) string {
	return "nmap " + versionToken(report.Version) + " xmloutputversion " + versionToken(report.XMLOutputVersion)
}

func versionToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 32 {
		return "unknown"
	}
	for _, character := range value {
		letter := (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z')
		digit := character >= '0' && character <= '9'
		if !letter && !digit && character != '.' && character != '-' && character != '_' {
			return "unknown"
		}
	}
	return value
}
