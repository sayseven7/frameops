package domain

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

var ErrInvalidCVSS31Vector = errors.New("invalid CVSS v3.1 vector")

type CVSS31 struct {
	Vector   string
	Score    float64
	Severity string
}

var cvss31Weights = map[string]map[string]float64{
	"AV": {"N": .85, "A": .62, "L": .55, "P": .2},
	"AC": {"L": .77, "H": .44},
	"UI": {"N": .85, "R": .62},
	"C":  {"H": .56, "L": .22, "N": 0},
	"I":  {"H": .56, "L": .22, "N": 0},
	"A":  {"H": .56, "L": .22, "N": 0},
}

var cvss31Order = []string{"AV", "AC", "PR", "UI", "S", "C", "I", "A"}

// ParseCVSS31 validates and calculates a CVSS v3.1 base vector.
func ParseCVSS31(vector string) (CVSS31, error) {
	parts := strings.Split(vector, "/")
	if len(parts) != 9 || parts[0] != "CVSS:3.1" {
		return CVSS31{}, ErrInvalidCVSS31Vector
	}
	metrics := make(map[string]string, 8)
	for _, part := range parts[1:] {
		pair := strings.Split(part, ":")
		if len(pair) != 2 || metrics[pair[0]] != "" {
			return CVSS31{}, ErrInvalidCVSS31Vector
		}
		metrics[pair[0]] = pair[1]
	}
	for _, name := range cvss31Order {
		value := metrics[name]
		if value == "" || !validCVSS31Metric(name, value) {
			return CVSS31{}, ErrInvalidCVSS31Vector
		}
	}

	iss := 1 - (1-weight(metrics, "C"))*(1-weight(metrics, "I"))*(1-weight(metrics, "A"))
	changed := metrics["S"] == "C"
	impact := 6.42 * iss
	if changed {
		impact = 7.52*(iss-.029) - 3.25*math.Pow(iss-.02, 15)
	}
	score := 0.0
	if impact > 0 {
		exploitability := 8.22 * weight(metrics, "AV") * weight(metrics, "AC") * privilegeWeight(metrics["PR"], changed) * weight(metrics, "UI")
		score = impact + exploitability
		if changed {
			score *= 1.08
		}
		score = roundUp(math.Min(score, 10))
	}
	normalized := "CVSS:3.1"
	for _, name := range cvss31Order {
		normalized += "/" + name + ":" + metrics[name]
	}
	return CVSS31{Vector: normalized, Score: score, Severity: cvssSeverity(score)}, nil
}

func validCVSS31Metric(name, value string) bool {
	if name == "S" {
		return value == "U" || value == "C"
	}
	if name == "PR" {
		return value == "N" || value == "L" || value == "H"
	}
	_, ok := cvss31Weights[name][value]
	return ok
}

func weight(metrics map[string]string, name string) float64 {
	return cvss31Weights[name][metrics[name]]
}

func privilegeWeight(value string, changed bool) float64 {
	if value == "N" {
		return .85
	}
	if value == "L" {
		if changed {
			return .68
		}
		return .62
	}
	if changed {
		return .5
	}
	return .27
}

func roundUp(value float64) float64 {
	return math.Ceil(value*10-1e-9) / 10
}

func cvssSeverity(score float64) string {
	switch {
	case score == 0:
		return "none"
	case score < 4:
		return "low"
	case score < 7:
		return "medium"
	case score < 9:
		return "high"
	case score <= 10:
		return "critical"
	default:
		panic(fmt.Sprintf("CVSS score outside range: %v", score))
	}
}
