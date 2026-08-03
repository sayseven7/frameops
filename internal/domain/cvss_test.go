package domain

import (
	"errors"
	"testing"
)

func TestCVSS31FIRSTBaseVectors(t *testing.T) {
	tests := []struct {
		vector   string
		score    float64
		severity string
	}{
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", 9.8, "critical"},
		{"CVSS:3.1/AV:N/AC:H/PR:N/UI:N/S:U/C:H/I:N/A:N", 5.9, "medium"},
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:R/S:C/C:L/I:L/A:N", 6.1, "medium"},
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:C/C:H/I:H/A:H", 10, "critical"},
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:N", 0, "none"},
	}
	for _, test := range tests {
		t.Run(test.vector, func(t *testing.T) {
			result, err := ParseCVSS31(test.vector)
			if err != nil || result.Score != test.score || result.Severity != test.severity {
				t.Fatalf("ParseCVSS31() = %#v, %v; want score %.1f, severity %s", result, err, test.score, test.severity)
			}
		})
	}
}

func TestCVSS31NormalizesMetricOrder(t *testing.T) {
	result, err := ParseCVSS31("CVSS:3.1/A:H/I:H/C:H/S:U/UI:N/PR:N/AC:L/AV:N")
	if err != nil {
		t.Fatal(err)
	}
	if result.Vector != "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H" {
		t.Fatalf("normalized vector = %q", result.Vector)
	}
}

func TestCVSS31RejectsInvalidVectors(t *testing.T) {
	for _, vector := range []string{
		"CVSS:3.0/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
		"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H",
		"CVSS:3.1/AV:N/AV:A/PR:N/UI:N/S:U/C:H/I:H/A:H",
		"CVSS:3.1/AV:X/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
		"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/E:F",
	} {
		if _, err := ParseCVSS31(vector); !errors.Is(err, ErrInvalidCVSS31Vector) {
			t.Errorf("ParseCVSS31(%q) error = %v", vector, err)
		}
	}
}
