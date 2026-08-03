package httpapi

import (
	"strings"
	"testing"
)

// scanReport builds one synthetic Nmap artifact. Addresses come from the
// documentation ranges and names from `.test`, so no fixture describes a real
// network.
const scanReport = `<?xml version="1.0" encoding="UTF-8"?>
<nmaprun scanner="nmap" args="-sV 198.51.100.0/24" version="7.94" xmloutputversion="1.05">
  <host>
    <status state="up" reason="syn-ack"/>
    <address addr="198.51.100.10" addrtype="ipv4"/>
    <hostnames><hostname name="API.Example.test" type="PTR"/></hostnames>
  </host>
  <host>
    <status state="up" reason="echo-reply"/>
    <address addr="198.51.100.11" addrtype="ipv4"/>
    <hostnames></hostnames>
  </host>
  <host>
    <status state="down" reason="no-response"/>
    <address addr="198.51.100.12" addrtype="ipv4"/>
  </host>
  <host>
    <status state="up" reason="syn-ack"/>
    <address addr="198.51.100.10" addrtype="ipv4"/>
    <hostnames><hostname name="api.example.test" type="user"/></hostnames>
  </host>
  <host>
    <status state="up" reason="syn-ack"/>
    <address addr="00:11:22:33:44:55" addrtype="mac"/>
  </host>
</nmaprun>`

func TestReadNmapReportIdentifiesHosts(t *testing.T) {
	report, err := readNmapReport(strings.NewReader(scanReport))
	if err != nil {
		t.Fatalf("read scan report: %v", err)
	}
	if report.FormatVersion != "nmap 7.94 xmloutputversion 1.05" {
		t.Fatalf("format version = %q, want the scanner and output versions", report.FormatVersion)
	}
	want := []string{"api.example.test", "198.51.100.11"}
	if len(report.Names) != len(want) {
		t.Fatalf("names = %#v, want %#v", report.Names, want)
	}
	for index, name := range want {
		if report.Names[index] != name {
			t.Fatalf("names = %#v, want %#v", report.Names, want)
		}
	}
	// A host that is down and a repeated identity are ignored; a host reported up
	// with only a hardware address cannot become an asset and is rejected.
	if report.Read != 5 || report.Ignored != 2 || report.Rejected != 1 {
		t.Fatalf("summary = read %d ignored %d rejected %d, want 5/2/1", report.Read, report.Ignored, report.Rejected)
	}
	if report.Read != len(report.Names)+report.Ignored+report.Rejected {
		t.Fatalf("summary does not account for every host read: %#v", report)
	}
}

// TestReadNmapReportDistrustsScannedNames checks the identity a scanned network
// can influence: a hostname that is not a plain domain name is discarded in
// favour of the address instead of becoming an asset name.
func TestReadNmapReportDistrustsScannedNames(t *testing.T) {
	hostile := `<nmaprun scanner="nmap" version="7.94; rm -rf /" xmloutputversion="1.05">
  <host>
    <status state="up"/>
    <address addr="198.51.100.20" addrtype="ipv4"/>
    <hostnames><hostname name="api.example.test/../../etc/passwd" type="PTR"/></hostnames>
  </host>
  <host>
    <status state="up"/>
    <address addr="2001:db8::5" addrtype="ipv6"/>
    <hostnames><hostname name="tab&#x9;bed.example.test" type="PTR"/></hostnames>
  </host>
</nmaprun>`
	report, err := readNmapReport(strings.NewReader(hostile))
	if err != nil {
		t.Fatalf("read hostile report: %v", err)
	}
	want := []string{"198.51.100.20", "2001:db8::5"}
	for index, name := range want {
		if len(report.Names) != len(want) || report.Names[index] != name {
			t.Fatalf("names = %#v, want %#v", report.Names, want)
		}
	}
	if report.FormatVersion != "nmap unknown xmloutputversion 1.05" {
		t.Fatalf("format version = %q, want an unreadable version recorded as unknown", report.FormatVersion)
	}
}

func TestReadNmapReportRefusesUnacceptedArtifacts(t *testing.T) {
	for name, artifact := range map[string]string{
		"empty":            "",
		"not xml":          "{\"hosts\":[]}",
		"truncated":        `<nmaprun scanner="nmap"><host><status state="up"/>`,
		"another scanner":  `<nmaprun scanner="masscan" version="1.3"><host><status state="up"/><address addr="198.51.100.30" addrtype="ipv4"/></host></nmaprun>`,
		"another document": `<scanreport><host addr="198.51.100.31"/></scanreport>`,
		"unknown entity":   `<!DOCTYPE nmaprun [<!ENTITY payload SYSTEM "file:///etc/passwd">]><nmaprun scanner="nmap" version="7.94">&payload;</nmaprun>`,
		"too many hosts": `<nmaprun scanner="nmap" version="7.94">` +
			strings.Repeat(`<host><status state="up"/><address addr="198.51.100.40" addrtype="ipv4"/></host>`, maxIngestionHosts+1) +
			`</nmaprun>`,
	} {
		if _, err := readNmapReport(strings.NewReader(artifact)); err == nil {
			t.Fatalf("%s artifact was accepted, want a refusal", name)
		}
	}
}
